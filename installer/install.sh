#!/usr/bin/env bash
# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

set -euo pipefail

log() {
  echo "$@" >&2
}
to_abs() {
  cd -- "$1" &> /dev/null && pwd
}

if [[ "${ZB_STORE_DIR:-/opt/zb/store}" != /opt/zb/store ]]; then
  log "ZB_STORE_DIR must be either unset or set to the default, /opt/zb/store."
  log "The store objects in this release use the default path."
  exit 1
fi
export ZB_STORE_DIR="/opt/zb/store"

installer_dir="$( to_abs "$( dirname -- "${BASH_SOURCE[0]}" )" )"
bin_dir=/usr/local/bin
bin_dir_explicit=0
single_user=0
usage() {
  log "usage: $0 [options]"
  log
  log "    --installer-dir DIR         install resources from the given directory (default $installer_dir)"
  log "    --bin DIR                   create symlinks to binaries in the given directory (default $bin_dir)"
  log "    --single-user               install without root privileges"
}
while [[ $# -gt 0 ]]; do
  case "$1" in
    --installer-dir)
      installer_dir="$( to_abs "$2" )"
      shift 2
      ;;
    --bin-dir)
      bin_dir="$( to_abs "$2" )"
      bin_dir_explicit=1
      shift 2
      ;;
    --single-user)
      single_user=1
      shift
      ;;
    --)
      shift
      break
      ;;
    --*)
      usage
      if [[ "$1" = '--help' ]]; then
        exit 0
      else
        exit 64
      fi
      ;;
    *)
      break
  esac
done

if [[ $# -gt 0 ]]; then
  usage
  exit 64
fi
# Clear bin directory if --single-user given without --bin.
if [[ $bin_dir_explicit -eq 0 && $single_user -eq 1 ]]; then
  bin_dir=''
fi

zb_object="$( cd "$installer_dir/store" > /dev/null && echo *-zb-* )"
if [[ "$zb_object" = '*-zb-*' ]]; then
  log "Error: missing zb object from $installer_dir/store"
  exit 1
fi
registry="$installer_dir/registry.txt"
if [[ ! -e "$registry" ]]; then
  log "Error: missing $registry"
  exit 1
fi

if [[ single_user -eq 1 || "$(id -u)" -eq 0 ]]; then
  run_as_target_user() {
    "$@"
  }
else
  run_as_target_user() {
    sudo --non-interactive -- "$@"
  }
  log "Testing for password-less sudo..."
  if ! run_as_target_user true ; then
    log "Please re-run this installer as root or with --single-user."
    exit 1
  fi
fi

log "Creating ${ZB_STORE_DIR}..."
run_as_target_user mkdir -p "$ZB_STORE_DIR"
run_as_target_user chmod 1775 "$ZB_STORE_DIR"

for i in $( cd "$installer_dir/store" > /dev/null && echo * ); do
  if [[ -e "$ZB_STORE_DIR/$i" ]]; then
    continue
  fi
  dst="$ZB_STORE_DIR/$i"
  log "Copying $dst..."
  temp_dst="${dst}~"
  if [[ -e "$temp_dst" ]]; then
    run_as_target_user rm -rf "$temp_dst"
  fi
  run_as_target_user cp -RPp "$installer_dir/store/$i" "$temp_dst"
  run_as_target_user find "$temp_dst" -exec touch -m -h -d @0 '{}' \;
  run_as_target_user chmod -R a-w "$temp_dst"
  run_as_target_user mv "$temp_dst" "$dst"
done

zb="$ZB_STORE_DIR/$zb_object/bin/zb"
log "Initializing store database..."
"$zb" store object register < "$registry"

# TODO(soon): Link systemd unit or launchd configuration.

if [[ -z "$bin_dir" ]]; then
  log "zb installed at $zb"
  if [[ "$bin_dir_explicit" -eq 0 ]]; then
    log "Add $(dirname "$zb") to your PATH to complete installation."
  fi
else
  log "Adding symlinks to ${bin_dir}..."
  run_as_target_user ln -sf "$zb" "$bin_dir/zb"
fi

log "Installation complete."
