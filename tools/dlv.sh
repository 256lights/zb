#!/bin/sh
# Use the following VSCode setting:
# "go.alternateTools": {
#   "dlv": "${workspaceFolder}/tools/dlv.sh"
# },
exec direnv exec "$(dirname "$0")/.." dlv "$@"
