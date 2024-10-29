// Copyright 2023 Roxy Light
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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOSLibrary(t *testing.T) {
	local := time.FixedZone("PDT", -7*60*60)
	lib := &OSLibrary{
		Now: func() time.Time {
			return time.Date(2023, time.September, 24, 13, 58, 7, 999999999, local)
		},
		Location: func() *time.Location { return local },
		LookupEnv: func(key string) (string, bool) {
			switch key {
			case "FOO":
				return "BAR", true
			case "EMPTY":
				return "", true
			default:
				return "", false
			}
		},
		Remove: func(filename string) error {
			if filename != "foo.txt" {
				return errors.New("mock")
			}
			return nil
		},
		Rename: func(old, new string) error {
			if old != "old" || new != "new" {
				return errors.New("mock")
			}
			return nil
		},
		Execute: func(command string) (ok bool, result string, status int) {
			if command != "true" {
				return false, "exit", 1
			}
			return true, "exit", 0
		},
		HasShell: func() bool { return true },
	}

	state := new(State)
	defer func() {
		if err := state.Close(); err != nil {
			t.Error("Close:", err)
		}
	}()
	out := new(strings.Builder)
	openBase := NewOpenBase(out, nil)
	if err := Require(state, GName, true, openBase); err != nil {
		t.Error(err)
	}
	if err := Require(state, OSLibraryName, true, lib.OpenLibrary); err != nil {
		t.Error(err)
	}
	f, err := os.Open(filepath.Join("testdata", "oslib.lua"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := state.Load(f, "@testdata/oslib.lua", "t"); err != nil {
		t.Fatal(err)
	}
	err = state.Call(0, 0, 0)
	if out.Len() > 0 {
		t.Log(out.String())
	}
	if err != nil {
		t.Error(err)
	}
}

func TestStrftime(t *testing.T) {
	refTime1 := time.Date(2006, time.January, 2, 15, 4, 5, 999999999, time.FixedZone("MST", -7*60*60))
	refTime2 := time.Date(2023, time.September, 24, 13, 58, 7, 999999999, time.FixedZone("PDT", -7*60*60))
	tests := []struct {
		format string
		want1  string
		want2  string
	}{
		{"%a", "Mon", "Sun"},
		{"%A", "Monday", "Sunday"},
		{"%b", "Jan", "Sep"},
		{"%B", "January", "September"},
		{"%c", "Mon Jan  2 15:04:05 2006", "Sun Sep 24 13:58:07 2023"},
		{"%C", "20", "20"},
		{"%d", "02", "24"},
		{"%D", "01/02/06", "09/24/23"},
		{"%e", " 2", "24"},
		{"%F", "2006-01-02", "2023-09-24"},
		{"%g", "06", "23"},
		{"%G", "2006", "2023"},
		{"%h", "Jan", "Sep"},
		{"%H", "15", "13"},
		{"%I", "03", "01"},
		{"%j", "002", "267"},
		{"%m", "01", "09"},
		{"%M", "04", "58"},
		{"%n", "\n", "\n"},
		{"%p", "PM", "PM"},
		{"%r", "03:04:05 PM", "01:58:07 PM"},
		{"%R", "15:04", "13:58"},
		{"%S", "05", "07"},
		{"%t", "\t", "\t"},
		{"%T", "15:04:05", "13:58:07"},
		{"%u", "1", "7"},
		{"%V", "01", "38"},
		{"%w", "1", "0"},
		{"%x", "01/02/06", "09/24/23"},
		{"%X", "15:04:05", "13:58:07"},
		{"%y", "06", "23"},
		{"%Y", "2006", "2023"},
		{"%z", "-0700", "-0700"},
		{"%Z", "MST", "PDT"},
		{"%%", "%", "%"},
	}
	for _, test := range tests {
		if got, err := strftime(refTime1, test.format); got != test.want1 || err != nil {
			t.Errorf("strftime(%s, %q) = %q, %v; want %q, <nil>",
				refTime1.Format(time.Layout), test.format, got, err, test.want1)
		}
		if got, err := strftime(refTime2, test.format); got != test.want2 || err != nil {
			t.Errorf("strftime(%s, %q) = %q, %v; want %q, <nil>",
				refTime2.Format(time.Layout), test.format, got, err, test.want2)
		}
	}
}
