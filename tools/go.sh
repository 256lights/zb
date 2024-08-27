#!/bin/sh
# Copyright 2024 Roxy Light
# SPDX-License-Identifier: MIT

# Use the following VSCode setting:
# "go.alternateTools": {
#   "go": "${workspaceFolder}/tools/go.sh"
# },

export DIRENV_LOG_FORMAT=''
if [ $# -gt 1 ] && [ "$1" = env ]; then
  # VSCode does not like having stderr output on go env.
  exec direnv exec "$(dirname "$0")/.." go "$@" 2> /dev/null
fi
exec direnv exec "$(dirname "$0")/.." go "$@"
