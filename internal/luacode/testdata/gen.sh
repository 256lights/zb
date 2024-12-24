#!/usr/bin/env bash
# Copyright 2024 The zb Authors
# SPDX-License-Identifier: MIT

set -euo pipefail

# Check luac
if ! command -v luac >& /dev/null; then
  echo "luac not found" >&2
  exit 1
fi
if [[ "$(luac -v)" != 'Lua 5.4.7  Copyright (C) 1994-2024 Lua.org, PUC-Rio' ]]; then
  echo "luac must be 5.4.7" >&2
  exit 1
fi

# Change into package directory.
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."

# Generate luac.out
for i in testdata/*/input.lua; do
  luac -o "$(dirname "$i")/luac.out" "$i"
done
