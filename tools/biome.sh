#!/bin/sh
# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT

DIRENV_LOG_FORMAT='' \
  exec direnv exec "$(dirname "$0")/.." \
  "$(dirname "$0")/../internal/ui/node_modules/.bin/biome" "$@"
