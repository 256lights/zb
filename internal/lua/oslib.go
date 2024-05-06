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
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// OSLibrary is a pure Go implementation of the standard Lua "os" library.
// The zero value of OSLibrary stubs out any functionality not related to time.
type OSLibrary struct {
	// Now returns the current local time.
	// If nil, uses time.Now.
	Now func() time.Time
	// Location returns the local timezone.
	// If nil, uses time.Local.
	Location func() *time.Location
	// LookupEnv returns the value of the given process environment variable.
	// If nil, os.getenv will always return nil.
	LookupEnv func(string) (string, bool)
	// Remove deletes the given file.
	// If nil, os.remove will always return nil and an error message.
	Remove func(string) error
	// Rename renames the given file.
	// If nil, os.rename will always return nil and an error message.
	Rename func(oldname, newname string) error
	// Execute runs a subprocess in the operating system shell.
	// If nil, os.execute with an argument will always return nil.
	Execute func(command string) (ok bool, result string, status int)
	// HasShell reports whether a shell is available.
	// If nil, os.execute without an argument will always return false.
	HasShell func() bool
	// TempName should return a file name that can be used for a temporary file.
	// If nil, os.tmpname will always raise an error.
	TempName func() (string, error)
}

// NewOSLibrary returns an OSLibrary that uses the native operating system.
func NewOSLibrary() *OSLibrary {
	return &OSLibrary{
		LookupEnv: os.LookupEnv,
		Remove:    os.Remove,
		Rename:    os.Rename,
		Execute:   osExecute,
		HasShell:  hasShell,
		TempName:  osTempName,
	}
}

func osExecute(command string) (ok bool, result string, status int) {
	c := osCommand(command)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		result, status = execError(err)
		return false, result, status
	}
	return true, "exit", 0
}

func osTempName() (string, error) {
	f, err := os.CreateTemp("", "lua_")
	if err != nil {
		return "", err
	}
	name := f.Name()
	f.Close()
	return name, nil
}

// OpenLibrary loads the standard os library.
// This method is intended to be used as an argument to [Require].
func (lib *OSLibrary) OpenLibrary(l *State) (int, error) {
	clock := lib.newClock()
	err := NewLib(l, map[string]Function{
		"clock":     clock,
		"date":      lib.date,
		"difftime":  lib.difftime,
		"execute":   lib.execute,
		"getenv":    lib.getenv,
		"remove":    lib.remove,
		"rename":    lib.rename,
		"setlocale": lib.setlocale,
		"time":      lib.time,
		"tmpname":   lib.tmpname,
	})
	if err != nil {
		return 0, err
	}
	return 1, nil
}

// newClock returns a [Function] that reports the wall clock time
// since newClock was called.
//
// The original Lua os.clock function uses the C clock function,
// which reports the CPU time in seconds.
// I am unclear the intent of the os.clock function,
// but at least [on Windows], C clock returns wall clock time.
// Therefore, using wall clock (possibly monotonic) time seems reasonable to me.
//
// [on Windows]: https://learn.microsoft.com/en-us/cpp/c-runtime-library/reference/clock?view=msvc-170
func (lib *OSLibrary) newClock() Function {
	var openTime time.Time
	if lib.Now == nil {
		openTime = time.Now()
	} else {
		openTime = lib.Now()
	}
	return func(l *State) (int, error) {
		var d time.Duration
		if lib.Now == nil {
			d = time.Since(openTime)
		} else {
			d = lib.Now().Sub(openTime)
		}
		l.PushNumber(d.Seconds())
		return 1, nil
	}
}

func (lib *OSLibrary) date(l *State) (int, error) {
	format := "%c"
	if !l.IsNoneOrNil(1) {
		var err error
		format, err = CheckString(l, 1)
		if err != nil {
			return 0, err
		}
	}
	var t time.Time
	if l.IsNoneOrNil(2) {
		if lib.Now == nil {
			t = time.Now()
		} else {
			t = lib.Now()
		}
	} else {
		var err error
		t, err = checkTime(l, 2)
		if err != nil {
			return 0, err
		}
	}
	format, utc := strings.CutPrefix(format, "!")
	if utc {
		t = t.UTC()
	} else if lib.Location != nil {
		t = t.In(lib.Location())
	} else {
		t = t.Local()
	}
	if format == "*t" {
		l.CreateTable(0, 9)
		setTimeFields(l, t)
	} else {
		s, err := strftime(t, format)
		if err != nil {
			return 0, NewArgError(l, 1, err.Error())
		}
		l.PushString(s)
	}
	return 1, nil
}

func (lib *OSLibrary) difftime(l *State) (int, error) {
	t2, err := checkTime(l, 1)
	if err != nil {
		return 0, err
	}
	t1, err := checkTime(l, 2)
	if err != nil {
		return 0, err
	}
	l.PushNumber(t2.Sub(t1).Seconds())
	return 1, nil
}

func (lib *OSLibrary) getenv(l *State) (int, error) {
	k, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	if lib.LookupEnv == nil {
		pushFail(l)
		return 1, nil
	}
	v, ok := lib.LookupEnv(k)
	if !ok {
		pushFail(l)
		return 1, nil
	}
	l.PushString(v)
	return 1, nil
}

func (lib *OSLibrary) remove(l *State) (int, error) {
	filename, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	if lib.Remove == nil {
		err = &os.PathError{
			Op:   "remove",
			Path: filename,
			Err:  errors.ErrUnsupported,
		}
	} else {
		err = lib.Remove(filename)
	}
	return pushFileResult(l, err), nil
}

func (lib *OSLibrary) rename(l *State) (int, error) {
	oldName, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	newName, err := CheckString(l, 2)
	if err != nil {
		return 0, err
	}
	if lib.Rename == nil {
		err = &os.LinkError{
			Op:  "rename",
			Old: oldName,
			New: newName,
			Err: errors.ErrUnsupported,
		}
	} else {
		err = lib.Rename(oldName, newName)
	}
	return pushFileResult(l, err), nil
}

func (lib *OSLibrary) execute(l *State) (int, error) {
	if l.IsNoneOrNil(1) {
		l.PushBoolean(lib.HasShell != nil && lib.HasShell())
		return 1, nil
	}
	command, err := CheckString(l, 1)
	if err != nil {
		return 0, err
	}
	if lib.Execute == nil {
		return pushFileResult(l, errors.ErrUnsupported), nil
	}
	ok, result, status := lib.Execute(command)
	if ok {
		l.PushBoolean(true)
	} else {
		pushFail(l)
	}
	l.PushString(result)
	l.PushInteger(int64(status))
	return 3, nil
}

func (lib *OSLibrary) setlocale(l *State) (int, error) {
	pushFail(l)
	return 1, nil
}

func (lib *OSLibrary) time(l *State) (int, error) {
	var t time.Time
	switch l.Type(1) {
	case TypeNone, TypeNil:
		if lib.Now == nil {
			t = time.Now()
		} else {
			t = lib.Now()
		}
	case TypeTable:
		l.SetTop(1)
		year, err := timeField(l, "year", -1)
		if err != nil {
			return 0, err
		}
		month, err := timeField(l, "month", -1)
		if err != nil {
			return 0, err
		}
		day, err := timeField(l, "day", -1)
		if err != nil {
			return 0, err
		}
		hour, err := timeField(l, "hour", -1)
		if err != nil {
			return 0, err
		}
		min, err := timeField(l, "min", -1)
		if err != nil {
			return 0, err
		}
		sec, err := timeField(l, "sec", -1)
		if err != nil {
			return 0, err
		}
		loc := time.Local
		if lib.Location != nil {
			loc = lib.Location()
		}
		t = time.Date(year, time.Month(month), day, hour, min, sec, 0, loc)
		if err := setTimeFields(l, t); err != nil {
			return 0, err
		}
	default:
		return 0, NewTypeError(l, 1, TypeTable.String())
	}
	l.PushInteger(t.Unix())
	return 1, nil
}

func (lib *OSLibrary) tmpname(l *State) (int, error) {
	if lib.TempName == nil {
		return 0, errors.ErrUnsupported
	}
	filename, err := lib.TempName()
	if err != nil {
		return 0, err
	}
	l.PushString(filename)
	return 1, nil
}

func timeField(l *State, key string, d int) (int, error) {
	tp, err := l.Field(-1, key, 0)
	if err != nil {
		return 0, err
	}
	res, ok := l.ToInteger(-1)
	l.Pop(1)
	if !ok {
		// TODO(soon): Add where information to errors.
		if tp != TypeNil {
			return 0, fmt.Errorf("%sfield '%s' is not an integer", Where(l, 1), key)
		}
		if d < 0 {
			return 0, fmt.Errorf("%sfield '%s' missing in date table", Where(l, 1), key)
		}
		return d, nil
	}

	if !(math.MinInt <= res && res <= math.MaxInt) {
		return 0, fmt.Errorf("%sfield '%s' is out-of-bound", Where(l, 1), key)
	}
	return int(res), nil
}

func checkTime(l *State, arg int) (time.Time, error) {
	sec, err := CheckInteger(l, arg)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0), nil
}

func setTimeFields(l *State, t time.Time) error {
	if err := setTimeField(l, "year", t.Year()); err != nil {
		return err
	}
	if err := setTimeField(l, "month", int(t.Month())); err != nil {
		return err
	}
	if err := setTimeField(l, "day", t.Day()); err != nil {
		return err
	}
	if err := setTimeField(l, "hour", t.Hour()); err != nil {
		return err
	}
	if err := setTimeField(l, "min", t.Minute()); err != nil {
		return err
	}
	if err := setTimeField(l, "sec", t.Second()); err != nil {
		return err
	}
	if err := setTimeField(l, "yday", t.YearDay()); err != nil {
		return err
	}
	if err := setTimeField(l, "wday", int(t.Weekday())+1); err != nil {
		return err
	}
	return nil
}

func setTimeField(l *State, key string, value int) error {
	l.PushInteger(int64(value))
	return l.SetField(-2, key, 0)
}

func strftime(t time.Time, format string) (string, error) {
	buf := make([]byte, 0, len(format))
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			buf = append(buf, c)
			continue
		}
		i++
		if i >= len(format) {
			return string(buf), fmt.Errorf("invalid conversion specifier '%%'")
		}
		switch format[i] {
		case 'a':
			buf = t.AppendFormat(buf, "Mon")
		case 'A':
			buf = t.AppendFormat(buf, "Monday")
		case 'b', 'h':
			buf = t.AppendFormat(buf, "Jan")
		case 'B':
			buf = t.AppendFormat(buf, "January")
		case 'c':
			buf = t.AppendFormat(buf, time.ANSIC)
		case 'C':
			century := t.Year() / 100
			if century < 10 {
				buf = append(buf, '0')
			}
			buf = strconv.AppendInt(buf, int64(century), 10)
		case 'd':
			buf = t.AppendFormat(buf, "02")
		case 'D':
			buf = t.AppendFormat(buf, "01/02/06")
		case 'e':
			buf = t.AppendFormat(buf, "_2")
		case 'F':
			buf = t.AppendFormat(buf, "2006-01-02")
		case 'g':
			year, _ := t.ISOWeek()
			year = year % 100
			if year < 10 {
				buf = append(buf, '0')
			}
			buf = strconv.AppendInt(buf, int64(year), 10)
		case 'G':
			year, _ := t.ISOWeek()
			buf = strconv.AppendInt(buf, int64(year), 10)
		case 'H':
			buf = t.AppendFormat(buf, "15")
		case 'I':
			buf = t.AppendFormat(buf, "03")
		case 'j':
			buf = t.AppendFormat(buf, "002")
		case 'm':
			buf = t.AppendFormat(buf, "01")
		case 'M':
			buf = t.AppendFormat(buf, "04")
		case 'n':
			buf = append(buf, '\n')
		case 'p':
			buf = t.AppendFormat(buf, "PM")
		case 'r':
			buf = t.AppendFormat(buf, "03:04:05 PM")
		case 'R':
			buf = t.AppendFormat(buf, "15:04")
		case 'S':
			buf = t.AppendFormat(buf, "05")
		case 't':
			buf = append(buf, '\t')
		case 'T':
			buf = t.AppendFormat(buf, "15:04:05")
		case 'u':
			wday := 1 + (int(t.Weekday())+6)%7
			buf = strconv.AppendInt(buf, int64(wday), 10)
		case 'V':
			_, week := t.ISOWeek()
			if week < 10 {
				buf = append(buf, '0')
			}
			buf = strconv.AppendInt(buf, int64(week), 10)
		case 'w':
			buf = strconv.AppendInt(buf, int64(t.Weekday()), 10)
		case 'x':
			buf = t.AppendFormat(buf, "01/02/06")
		case 'X':
			buf = t.AppendFormat(buf, "15:04:05")
		case 'y':
			buf = t.AppendFormat(buf, "06")
		case 'Y':
			buf = t.AppendFormat(buf, "2006")
		case 'z':
			buf = t.AppendFormat(buf, "-0700")
		case 'Z':
			buf = t.AppendFormat(buf, "MST")
		case '%':
			buf = append(buf, '%')
		default:
			return string(buf), fmt.Errorf("invalid conversion specifier '%%%c'", format[i])
		}
	}
	return string(buf), nil
}
