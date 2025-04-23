#!@runtimeShell@
# shellcheck shell=sh
# Copyright 2025 The zb Authors
# SPDX-License-Identifier: MIT
export C_INCLUDE_PATH="${C_INCLUDE_PATH:-}"
if [ -n "$C_INCLUDE_PATH" ]; then
  C_INCLUDE_PATH="${C_INCLUDE_PATH}:"
fi
C_INCLUDE_PATH="${C_INCLUDE_PATH}@gcc@/include"
export LIBRARY_PATH="${LIBRARY_PATH:-}"
if [ -n "$LIBRARY_PATH" ]; then
  LIBRARY_PATH="${LIBRARY_PATH}:"
fi
LIBRARY_PATH="${LIBRARY_PATH}@gcc@/lib/gcc/@target@/@version@:@gcc@/@target@/lib"
exec @gcc@/bin/@tool@ "$@"
