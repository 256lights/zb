// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package luacode

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestParse(t *testing.T) {
	root := "testdata"
	listing, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	diffOptions := cmp.Options{
		cmp.AllowUnexported(LineInfo{}),
		cmp.AllowUnexported(absLineInfo{}),
		cmpopts.EquateEmpty(),
	}

	for _, ent := range listing {
		name := ent.Name()
		if !ent.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		const inputName = "input.lua"
		const outputName = "luac.out"

		f, err := os.Open(filepath.Join(root, name, inputName))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				t.Error(err)
			}
			continue
		}

		t.Run(name, func(t *testing.T) {
			defer f.Close()

			source := Source("@" + filepath.ToSlash(root) + "/" + name + "/" + inputName)
			got, err := Parse(source, bufio.NewReader(f))
			if err != nil {
				t.Fatal("Parse:", err)
			}

			wantChunk, err := os.ReadFile(filepath.Join(root, name, outputName))
			if err != nil {
				t.Fatal(err)
			}
			want := new(Prototype)
			if err := want.UnmarshalBinary(wantChunk); err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(want, got, diffOptions); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}

func TestMaxVariables(t *testing.T) {
	const limit = 250
	if maxVariables >= limit {
		t.Errorf("maxVariables = %d; want <%d due to bytecode format", maxVariables, limit)
	}
}
