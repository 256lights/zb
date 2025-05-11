// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package system

// Environment is the name of an environment for a [System].
// The empty string is treated the same as [Unknown].
type Environment string

// IsUnknown reports whether env is the empty string or [Unknown].
func (env Environment) IsUnknown() bool {
	return env == "" || env == Unknown
}

// String returns string(env) or [Unknown] if env is the empty string.
func (env Environment) String() string {
	if env == "" {
		return Unknown
	}
	return string(env)
}
