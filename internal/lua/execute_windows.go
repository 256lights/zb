// Copyright 2023 Ross Light
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the “Software”), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
//
// SPDX-License-Identifier: MIT

package lua

import (
	"errors"
	"os/exec"
	"syscall"
)

func osCommand(command string) *exec.Cmd {
	return &exec.Cmd{
		SysProcAttr: &syscall.SysProcAttr{
			CmdLine: command,
		},
	}
}

func execError(err error) (result string, status int) {
	var e *exec.ExitError
	if !errors.As(err, &e) {
		return "exit", -1
	}
	if w, ok := e.Sys().(syscall.WaitStatus); ok && w.Signaled() {
		return "signal", int(w.Signal())
	}
	return "exit", e.ExitCode()
}

func hasShell() bool {
	// TODO(someday): I don't know how to implement this.
	return true
}
