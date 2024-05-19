#!/bin/sh
# Copyright 2024 Ross Light
# SPDX-License-Identifier: MIT

# Use the following VSCode setting:
# "go.alternateTools": {
#   "dlv": "${workspaceFolder}/tools/dlv.sh"
# },

exec direnv exec "$(dirname "$0")/.." dlv "$@"
