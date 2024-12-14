// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"os"
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
