#!/bin/sh
# Use the following VSCode setting:
# "go.alternateTools": {
#   "go": "${workspaceFolder}/tools/go.sh"
# },
if [ $# -gt 1 ] && [ "$1" = env ]; then
  # VSCode does not like having stderr output on go env.
  exec direnv exec "$(dirname "$0")/.." go "$@" 2> /dev/null
fi
exec direnv exec "$(dirname "$0")/.." go "$@"
