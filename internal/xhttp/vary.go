// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"iter"
	"net/http"
	"strings"
)

// VaryValue is a list of HTTP Vary field lines.
type VaryValue []string

// VaryHeader returns a [VaryValue] from the header.
func VaryHeader(h http.Header) VaryValue {
	return VaryValue(h["Vary"])
}

// IsZero reports whether vv is empty.
func (vv VaryValue) IsZero() bool {
	return len(vv) == 0 || len(vv) == 1 && strings.Trim(vv[0], " \t") == ""
}

// HasWildcard reports whether vv contains "*".
func (vv VaryValue) HasWildcard() bool {
	for _, varyValue := range vv {
		for varyElem := range SplitList(varyValue) {
			if varyElem == "*" {
				return true
			}
		}
	}
	return false
}

// FieldNames returns an iterator over the field names in vv.
// All field names are transformed using [http.CanonicalHeaderKey].
func (vv VaryValue) FieldNames() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, varyValue := range vv {
			for varyElem := range SplitList(varyValue) {
				if varyElem != "*" {
					varyElem = http.CanonicalHeaderKey(varyElem)
					if !yield(varyElem) {
						return
					}
				}
			}
		}
	}
}
