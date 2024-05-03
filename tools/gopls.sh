#!/bin/sh
# Use the following VSCode setting:
# "go.alternateTools": {
#   "gopls": "${workspaceFolder}/tools/gopls.sh"
# },
exec direnv exec "$(dirname "$0")/.." gopls "$@"
