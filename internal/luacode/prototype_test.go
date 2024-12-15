// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var prototypeDiffOptions = cmp.Options{
	cmp.AllowUnexported(LineInfo{}),
	cmp.AllowUnexported(absLineInfo{}),
	cmpopts.EquateEmpty(),
}

func FuzzPrototypeMarshalBinary(f *testing.F) {
	for test := range readTestData(f) {
		test.input.Close()
		chunk, err := os.ReadFile(test.luacOutputPath)
		if err != nil {
			f.Error(err)
			continue
		}
		f.Add(chunk)
	}

	f.Fuzz(func(t *testing.T, chunk []byte) {
		want := new(Prototype)
		if err := want.UnmarshalBinary(chunk); err != nil {
			t.Skip(err)
		}
		data, err := want.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		got := new(Prototype)
		if err := got.UnmarshalBinary(data); err != nil {
			t.Error(err)
		}
		if diff := cmp.Diff(want, got, prototypeDiffOptions); diff != "" {
			t.Errorf("-want +got:\n%s", diff)
		}
	})
}

func TestLocalName(t *testing.T) {
	tests := []struct {
		register uint8
		pc       int
		want     string
	}{
		{pc: 0, register: 0, want: ""},
		{pc: 0, register: 1, want: ""},
		{pc: 0, register: 2, want: ""},

		{pc: 1, register: 0, want: ""},
		{pc: 1, register: 1, want: ""},
		{pc: 1, register: 2, want: ""},

		{pc: 2, register: 0, want: "a"},
		{pc: 2, register: 1, want: ""},
		{pc: 2, register: 2, want: ""},

		{pc: 3, register: 0, want: "a"},
		{pc: 3, register: 1, want: "c"},
		{pc: 3, register: 2, want: ""},

		{pc: 5, register: 0, want: "a"},
		{pc: 5, register: 1, want: "c"},
		{pc: 5, register: 2, want: ""},

		{pc: 6, register: 0, want: "a"},
		{pc: 6, register: 1, want: "c"},
		{pc: 6, register: 2, want: "b"},

		{pc: 7, register: 0, want: "a"},
		{pc: 7, register: 1, want: "c"},
		{pc: 7, register: 2, want: "b"},

		{pc: 8, register: 0, want: "a"},
		{pc: 8, register: 1, want: "c"},
		{pc: 8, register: 2, want: ""},

		{pc: 9, register: 0, want: "a"},
		{pc: 9, register: 1, want: "c"},
		{pc: 9, register: 2, want: ""},

		{pc: 10, register: 0, want: "a"},
		{pc: 10, register: 1, want: "c"},
		{pc: 10, register: 2, want: "d"},
	}

	chunk, err := os.ReadFile(filepath.Join("testdata", "Scoping", "luac.out"))
	if err != nil {
		t.Fatal(err)
	}
	p := new(Prototype)
	if err := p.UnmarshalBinary(chunk); err != nil {
		t.Fatal(err)
	}
	t.Log("Locals:")
	for _, v := range p.LocalVariables {
		t.Logf("%s\tscope:[%d, %d)", v.Name, v.StartPC, v.EndPC)
	}

	for _, test := range tests {
		if got := p.LocalName(test.register, test.pc); got != test.want {
			t.Errorf("p.LocalName(%d, %d) = %q; want %q", test.register, test.pc, got, test.want)
		}
	}
}
