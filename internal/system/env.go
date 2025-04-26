// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package system

import "strings"

// Environment is the name of an environment for a [System].
// The empty string is treated the same as [Unknown].
type Environment string

// IsUnknown reports whether env is the empty string or [Unknown].
func (env Environment) IsUnknown() bool {
	return env == "" || env == Unknown
}

func (env Environment) isKnown() bool {
	return strings.HasPrefix(string(env), "musl") ||
		strings.HasPrefix(string(env), "gnu") ||
		strings.HasPrefix(string(env), "msvc") ||
		strings.HasPrefix(string(env), "cygnus") ||
		strings.HasPrefix(string(env), "android")
}

// String returns string(env) or [Unknown] if env is the empty string.
func (env Environment) String() string {
	if env == "" {
		return Unknown
	}
	return string(env)
}
