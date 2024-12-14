// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"bufio"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	for test := range readTestData(t) {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.source, bufio.NewReader(test.input))
			if err != nil {
				t.Fatal("Parse:", err)
			}

			wantChunk, err := os.ReadFile(test.luacOutputPath)
			if err != nil {
				t.Fatal(err)
			}
			want := new(Prototype)
			if err := want.UnmarshalBinary(wantChunk); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(want, got, prototypeDiffOptions); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})

		test.input.Close()
	}
}

type parseTest struct {
	name           string
	source         Source
	input          *os.File
	luacOutputPath string
}

func readTestData(tb testing.TB) iter.Seq[parseTest] {
	const inputName = "input.lua"
	const outputName = "luac.out"

	return func(yield func(parseTest) bool) {
		root := "testdata"
		listing, err := os.ReadDir(root)
		if err != nil {
			tb.Error(err)
			return
		}

		for _, ent := range listing {
			name := ent.Name()
			if !ent.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				continue
			}
			f, err := os.Open(filepath.Join(root, name, inputName))
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					tb.Error(err)
				}
				continue
			}
			test := parseTest{
				name:           name,
				source:         Source("@" + filepath.ToSlash(root) + "/" + name + "/" + inputName),
				input:          f,
				luacOutputPath: filepath.Join(root, name, outputName),
			}
			if !yield(test) {
				return
			}
		}
	}
}

func TestMaxVariables(t *testing.T) {
	const limit = 250
	if maxVariables >= limit {
		t.Errorf("maxVariables = %d; want <%d due to bytecode format", maxVariables, limit)
	}
}
