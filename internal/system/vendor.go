// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package system

// Vendor is the name of a machine vendor for a [System].
// The empty string is treated the same as [Unknown].
type Vendor string

// IsUnknown reports whether vendor is the empty string or [Unknown].
func (vendor Vendor) IsUnknown() bool {
	return vendor == "" || vendor == Unknown
}

// String returns string(vendor) or [Unknown] if vendor is the empty string.
func (vendor Vendor) String() string {
	if vendor == "" {
		return Unknown
	}
	return string(vendor)
}
