// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lua

import (
	"fmt"

	"zb.256lights.llc/pkg/internal/luacode"
)

// errorToValue converts a Go error to a Lua [value].
// If the error is an [*errorObject]
// and it came from the same generation of *State,
// then errorToValue returns its value.
// errorToValue(nil) returns nil.
func (l *State) errorToValue(err error) value {
	if err == nil {
		return nil
	}
	// We test for identity instead of using errors.As
	// because any wrapping could have a different message,
	// and thus lead to confusing results.
	if obj, ok := err.(*errorObject); ok && obj.state == l && obj.generation == l.generation {
		return obj.value
	}
	// TODO(maybe): Use a userdata instead (so errors can be round-tripped)?
	return stringValue{s: err.Error()}
}

// errorObject wraps a [value] as an [error].
type errorObject struct {
	state      *State
	generation uint64
	value      value
}

func newErrorObject(l *State, value value) *errorObject {
	return &errorObject{
		state:      l,
		generation: l.generation,
		value:      value,
	}
}

// Error performs a reduced version of [ToString]
// that does not call functions.
func (obj *errorObject) Error() string {
	switch v := obj.value.(type) {
	case nil:
		return "nil"
	case booleanValue:
		s, _ := luacode.BoolValue(bool(v)).Unquoted()
		return s
	case valueStringer:
		return v.stringValue().s
	case *table:
		if name, ok := v.meta.get(stringValue{s: typeNameMetafield}).(stringValue); ok {
			return formatObject(name.s, v.id)
		}
		return formatObject(v.valueType().String(), v.id)
	case *userdata:
		if name, ok := v.meta.get(stringValue{s: typeNameMetafield}).(stringValue); ok {
			return formatObject(name.s, v.id)
		}
		return formatObject(v.valueType().String(), v.id)
	case referenceValue:
		return formatObject(v.valueType().String(), v.valueID())
	default:
		// Unhandled type (should not occur in practice).
		return fmt.Sprintf("(error object is a %v value)", v.valueType())
	}
}
