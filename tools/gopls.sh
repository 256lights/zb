#!/bin/sh
# Copyright 2024 Roxy Light
# SPDX-License-Identifier: MIT

# Use the following VSCode setting:
# "go.alternateTools": {
#   "gopls": "${workspaceFolder}/tools/gopls.sh"
# },

DIRENV_LOG_FORMAT='' exec direnv exec "$(dirname "$0")/.." gopls "$@"
