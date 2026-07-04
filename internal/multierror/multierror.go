// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

// Package multierror provides a type for collecting errors.
package multierror

import (
	"iter"
	"slices"
	"strings"
)

// A Collector is a list of errors.
// The zero value is an empty list.
type Collector struct {
	errs []error
}

// Error returns the list of errors as a single [error] value.
// If the list is empty, then Error returns nil.
// If the list has a single error, then Error returns it.
// Otherwise, Error returns an error with an Unwrap() []error method
// that returns all the errors collected so far.
func (c *Collector) Error() error {
	switch {
	case c == nil || len(c.errs) == 0:
		return nil
	case len(c.errs) == 1:
		return c.errs[0]
	default:
		return multiError(c.errs)
	}
}

// Add appends an [error] to the list,
// or does nothing if err is nil.
// If the error was created by [*Collector.Error],
// then all the errors are appended to the list.
func (c *Collector) Add(err error) {
	if e, ok := err.(multiError); ok {
		c.errs = append(c.errs, e...)
	} else if err != nil {
		c.errs = append(c.errs, err)
	}
}

// All returns an iterator over the [error].
// If the error is nil, then the iterator does not yield anything.
// If the error was created by [*Collector.Error],
// then the iterator yields the errors in the order they were added to the [*Collector].
// Otherwise, the iterator yields the error itself.
func All(err error) iter.Seq[error] {
	switch e := err.(type) {
	case nil:
		return func(yield func(error) bool) {}
	case multiError:
		return slices.Values(e)
	default:
		return func(yield func(error) bool) { yield(err) }
	}
}

type multiError []error

func (e multiError) Error() string {
	if len(e) == 1 {
		return e[0].Error()
	}
	sb := new(strings.Builder)
	sb.WriteString(e[0].Error())
	for _, err := range e[1:] {
		sb.WriteByte('\n')
		sb.WriteString(err.Error())
	}
	return sb.String()
}

func (e multiError) Unwrap() []error {
	return e
}
