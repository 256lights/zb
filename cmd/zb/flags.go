// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/csv"
	"slices"
	"strconv"
	"strings"

	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
)

// stringAllowList is an allow list of a set of strings.
// If all is true, then it is the set of all strings.
type stringAllowList struct {
	set sets.Set[string]
	all bool
}

func (list *stringAllowList) Has(s string) bool {
	if list == nil {
		return false
	}
	return list.all || list.set.Has(s)
}

func (list *stringAllowList) argFlag(csv bool) *stringAllowListFlag {
	if list.set == nil {
		list.set = make(sets.Set[string])
	}
	return &stringAllowListFlag{
		stringSetFlag: stringSetFlag{
			set: list.set,
			csv: csv,
		},
		all: &list.all,
	}
}

func (list *stringAllowList) allFlag() *stringAllowListAllFlag {
	return &stringAllowListAllFlag{list: list}
}

// stringAllowListFlag is the implementation of [github.com/spf13/pflag.Value]
// and [github.com/spf13/pflag.SliceValue]
// for [*stringAllowList.argFlag].
// If a value is specified, then all will be set to false.
type stringAllowListFlag struct {
	stringSetFlag
	all *bool
}

func (f *stringAllowListFlag) Set(s string) error {
	*f.all = false
	return f.stringSetFlag.Set(s)
}

func (f *stringAllowListFlag) Append(s string) error {
	*f.all = false
	return f.stringSetFlag.Append(s)
}

func (f *stringAllowListFlag) Replace(val []string) error {
	*f.all = false
	return f.stringSetFlag.Replace(val)
}

// stringSetFlag is similar to [github.com/spf13/pflag.StringArray],
// but prevents duplicate entries.
// If csv is true, then stringSetFlag acts like [github.com/spf13/pflag.StringSlice].
type stringSetFlag struct {
	set     sets.Set[string]
	changed bool
	csv     bool
}

func (f *stringSetFlag) Get() any { return f.set }

func (f *stringSetFlag) Type() string {
	if f.csv {
		return "stringSlice"
	} else {
		return "stringArray"
	}
}

func (f *stringSetFlag) GetSlice() []string {
	s := slices.Collect(f.set.All())
	slices.Sort(s)
	return s
}

func (f *stringSetFlag) String() string {
	buf := new(bytes.Buffer)
	buf.WriteString("[")
	w := csv.NewWriter(buf)
	_ = w.Write(f.GetSlice())
	w.Flush()
	b := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	b = append(b, "]"...)
	return string(b)
}

func (f *stringSetFlag) Set(s string) error {
	if f.set == nil {
		f.set = make(sets.Set[string])
	}
	if !f.changed {
		f.set.Clear()
		f.changed = true
	}
	if f.csv {
		r := csv.NewReader(strings.NewReader(s))
		vals, err := r.Read()
		if err != nil {
			return err
		}
		f.set.AddSeq(slices.Values(vals))
	} else {
		f.set.Add(s)
	}
	return nil
}

func (f *stringSetFlag) Append(val string) error {
	if f.set == nil {
		f.set = make(sets.Set[string])
	}
	f.set.Add(val)
	return nil
}

func (f *stringSetFlag) Replace(val []string) error {
	if f.set == nil {
		f.set = make(sets.Set[string])
	} else {
		f.set.Clear()
	}
	for _, s := range val {
		f.set.Add(s)
	}
	return nil
}

// stringAllowListAllFlag is the implementation of [github.com/spf13/pflag.Value]
// for [*stringAllowList.allFlag].
// If set false, then list.set will be cleared.
type stringAllowListAllFlag struct {
	list *stringAllowList
}

func (f *stringAllowListAllFlag) IsBoolFlag() bool { return true }
func (f *stringAllowListAllFlag) Type() string     { return "bool" }
func (f *stringAllowListAllFlag) String() string   { return strconv.FormatBool(f.list.all) }
func (f *stringAllowListAllFlag) Get() any         { return f.list.all }

func (f *stringAllowListAllFlag) Set(s string) error {
	b, err := strconv.ParseBool(s)
	f.list.all = b
	if !b {
		f.list.set.Clear()
	}
	return err
}

type storeDirectoryFlag zbstore.Directory

func (f *storeDirectoryFlag) Type() string  { return "string" }
func (f storeDirectoryFlag) String() string { return string(f) }
func (f storeDirectoryFlag) Get() any       { return zbstore.Directory(f) }

func (f *storeDirectoryFlag) Set(s string) error {
	dir, err := zbstore.CleanDirectory(s)
	if err != nil {
		return err
	}
	*f = storeDirectoryFlag(dir)
	return nil
}
