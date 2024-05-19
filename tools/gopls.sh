#!/bin/sh
# Copyright 2024 Ross Light
# SPDX-License-Identifier: MIT

# Use the following VSCode setting:
# "go.alternateTools": {
#   "gopls": "${workspaceFolder}/tools/gopls.sh"
# },

exec direnv exec "$(dirname "$0")/.." gopls "$@"
