// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package mylua

import "errors"

// errorToValue converts a Go error to a Lua [value].
// If there is an [errorObject] in the error chain,
// then errorToValue returns its value.
// errorToValue(nil) returns nil.
func errorToValue(err error) value {
	if err == nil {
		return nil
	}
	if obj := (errorObject{}); errors.As(err, &obj) {
		return obj.value
	}
	// TODO(maybe): Use a userdata instead (so errors can be round-tripped)?
	return stringValue{s: err.Error()}
}

// errorObject wraps a [value] as an [error].
type errorObject struct {
	value value
}

func (obj errorObject) Error() string {
	if obj.value == nil {
		return "<lua nil>"
	}
	s, ok := toString(obj.value)
	if !ok {
		return "<" + obj.value.valueType().String() + ">"
	}
	return s.s
}
