// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package frontend

import (
	"bytes"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/internal/luacode"
)

func TestCompilePrelude(t *testing.T) {
	preludeText, err := os.ReadFile("prelude.lua")
	if err != nil {
		t.Fatal(err)
	}
	got, err := luacode.Parse("=(prelude)", bytes.NewReader(preludeText))
	if err != nil {
		t.Fatal("parse prelude.lua:", err)
	}
	want := new(luacode.Prototype)
	if err := want.UnmarshalBinary(preludeSource); err != nil {
		t.Fatal("unmarshal prelude.luac:", err)
	}

	if diff := cmp.Diff(want, got, prototypeDiffOptions); diff != "" {
		t.Errorf("prelude.luac out of date. Try running `go generate`. (-want +got):\n%s", diff)
	}
}

var prototypeDiffOptions = cmp.Options{
	cmp.Comparer(luacode.Value.IdenticalTo),
	lineInfoCompareOption,
	cmpopts.EquateEmpty(),
}

var lineInfoCompareOption = cmp.Transformer("lineInfoToSlice", lineInfoToSlice)

func lineInfoToSlice(info luacode.LineInfo) []int {
	s := make([]int, 0, info.Len())
	for _, line := range info.All() {
		s = append(s, line)
	}
	return s
}
