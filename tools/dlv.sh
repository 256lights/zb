#!/bin/sh
# Copyright 2024 The zb Authors
# SPDX-License-Identifier: MIT

# Use the following VSCode setting:
# "go.alternateTools": {
#   "dlv": "${workspaceFolder}/tools/dlv.sh"
# },

DIRENV_LOG_FORMAT='' exec direnv exec "$(dirname "$0")/.." dlv "$@"
