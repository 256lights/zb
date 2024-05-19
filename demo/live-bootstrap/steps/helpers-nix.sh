#!/usr/bin/env bash
# Helper functions copied from nixpkgs.
# SPDX-License-Identifier: MIT

# Return success if the specified file is a script (i.e. starts with
# "#!").
isScript() {
    local fn="$1"
    local magic
    magic="$(head -c 2 "$fn")"
    if [[ "$magic" = '#!' ]]; then return 0; else return 1; fi
}

# Run patch shebangs on a directory or file.
# Can take multiple paths as arguments.
# patchShebangs [--update] [--] PATH...

# Flags:
# --update : Update shebang paths that are in Nix store
patchShebangs() {
    local pathName=PATH
    local update

    while [[ $# -gt 0 ]]; do
        case "$1" in
        --update)
            update=true
            shift
            ;;
        --)
            shift
            break
            ;;
        -*|--*)
            echo "Unknown option $1 supplied to patchShebangs" >&2
            return 1
            ;;
        *)
            break
            ;;
        esac
    done

    echo "patching script interpreter paths in $@"
    local f
    local oldPath
    local newPath
    local arg0
    local args
    local oldInterpreterLine
    local newInterpreterLine

    if [[ $# -eq 0 ]]; then
        echo "No arguments supplied to patchShebangs" >&2
        return 0
    fi

    local f
    for f in "$@"; do
        isScript "$f" || continue

        # read exits unclean if the shebang does not end with a newline, but still assigns the variable.
        # So if read returns errno != 0, we check if the assigned variable is non-empty and continue.
        read -r oldInterpreterLine < "$f" || [ "$oldInterpreterLine" ]

        read -r oldPath arg0 args <<< "${oldInterpreterLine:2}"

        if [[ -z "${pathName:-}" ]]; then
            if [[ -n $strictDeps && $f == "$NIX_STORE"* ]]; then
                pathName=HOST_PATH
            else
                pathName=PATH
            fi
        fi

        if [[ "$oldPath" == *"/bin/env" ]]; then
            if [[ $arg0 == "-S" ]]; then
                arg0=${args%% *}
                args=${args#* }
                newPath="$(PATH="${!pathName}" command -v "env" || true)"
                args="-S $(PATH="${!pathName}" command -v "$arg0" || true) $args"

            # Check for unsupported 'env' functionality:
            # - options: something starting with a '-' besides '-S'
            # - environment variables: foo=bar
            elif [[ $arg0 == "-"* || $arg0 == *"="* ]]; then
                echo "$f: unsupported interpreter directive \"$oldInterpreterLine\" (set dontPatchShebangs=1 and handle shebang patching yourself)" >&2
                exit 1
            else
                newPath="$(PATH="${!pathName}" command -v "$arg0" || true)"
            fi
        else
            if [[ -z $oldPath ]]; then
                # If no interpreter is specified linux will use /bin/sh. Set
                # oldpath="/bin/sh" so that we get /nix/store/.../sh.
                oldPath="/bin/sh"
            fi

            newPath="$(PATH="${!pathName}" command -v "$(basename "$oldPath")" || true)"

            args="$arg0 $args"
        fi

        # Strip trailing whitespace introduced when no arguments are present
        newInterpreterLine="$newPath $args"
        newInterpreterLine=${newInterpreterLine%${newInterpreterLine##*[![:space:]]}}

        if [[ -n "$oldPath" && ( "$update" == true || "${oldPath:0:${#NIX_STORE}}" != "$NIX_STORE" ) ]]; then
            if [[ -n "$newPath" && "$newPath" != "$oldPath" ]]; then
                echo "$f: interpreter directive changed from \"$oldInterpreterLine\" to \"$newInterpreterLine\""
                # escape the escape chars so that sed doesn't interpret them
                escapedInterpreterLine=${newInterpreterLine//\\/\\\\}

                # TODO: Preserve times, see: https://github.com/NixOS/nixpkgs/pull/33281
                # timestamp=$(stat --printf "%y" "$f")
                sed -i -e "1 s|.*|#\!$escapedInterpreterLine|" "$f"
                chmod +x "$f"
                # touch --date "$timestamp" "$f"
            fi
        fi
    done
}
