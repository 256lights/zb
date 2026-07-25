// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"fmt"
	"time"
)

type splitContext struct {
	cancel context.Context
	values context.Context
}

func (c splitContext) Deadline() (deadline time.Time, ok bool) { return c.cancel.Deadline() }
func (c splitContext) Done() <-chan struct{}                   { return c.cancel.Done() }
func (c splitContext) Err() error                              { return c.cancel.Err() }
func (c splitContext) AfterFunc(f func()) func() bool          { return context.AfterFunc(c.cancel, f) }
func (c splitContext) Value(key any) any                       { return c.values.Value(key) }

func (c splitContext) String() string {
	return fmt.Sprintf("splitContext{cancel:%v values:%v}", c.cancel, c.values)
}
