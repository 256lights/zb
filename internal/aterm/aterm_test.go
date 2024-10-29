// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package aterm

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var stringTests = []struct {
	s     string
	aterm string
}{
	{"", `""`},
	{"x", `"x"`},
	{"\n", `"\n"`},
	{"\r", `"\r"`},
	{"\t", `"\t"`},
	{"\\", `"\\"`},
	{"\"", `"\""`},
}

func TestScanner(t *testing.T) {
	type scannerTest struct {
		aterm string
		want  []Token
		err   bool
		tail  string
	}

	tests := []scannerTest{
		{
			aterm: `()`,
			want: []Token{
				{Kind: LParen},
				{Kind: RParen},
			},
		},
		{
			aterm: `[]`,
			want: []Token{
				{Kind: LBracket},
				{Kind: RBracket},
			},
		},
		{
			aterm: `("x")`,
			want: []Token{
				{Kind: LParen},
				{Kind: String, Value: "x"},
				{Kind: RParen},
			},
		},
		{
			aterm: `("x","y","z")`,
			want: []Token{
				{Kind: LParen},
				{Kind: String, Value: "x"},
				{Kind: String, Value: "y"},
				{Kind: String, Value: "z"},
				{Kind: RParen},
			},
		},
		{
			aterm: `("x",)`,
			want: []Token{
				{Kind: LParen},
				{Kind: String, Value: "x"},
			},
			err: true,
		},
		{
			aterm: `("x",,"y")`,
			want: []Token{
				{Kind: LParen},
				{Kind: String, Value: "x"},
			},
			err:  true,
			tail: `"y")`,
		},
		{
			aterm: `("x"]`,
			want: []Token{
				{Kind: LParen},
				{Kind: String, Value: "x"},
			},
			err: true,
		},
		{
			aterm: `[)`,
			want: []Token{
				{Kind: LBracket},
			},
			err: true,
		},
		{
			aterm: `)`,
			want:  []Token{},
			err:   true,
		},
		{
			aterm: `[`,
			want: []Token{
				{Kind: LBracket},
			},
			err: true,
		},
	}
	for _, test := range stringTests {
		tests = append(tests, scannerTest{
			aterm: test.aterm,
			want: []Token{
				{Kind: String, Value: test.s},
			},
		})
	}

	for _, test := range tests {
		r := strings.NewReader(test.aterm)
		s := NewScanner(r)
		var got []Token
		for {
			tok, err := s.ReadToken()
			if err != nil {
				if !test.err && err != io.EOF {
					t.Errorf("While scanning %s: %v", test.aterm, err)
				}
				if test.err && err == io.EOF {
					t.Errorf("Scanning %s did not result in an error", test.aterm)
				}
				break
			}
			got = append(got, tok)
		}
		if diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("tokens for %s (-want +got):\n%s", test.aterm, diff)
		}
		if got := test.aterm[len(test.aterm)-r.Len():]; got != test.tail {
			t.Errorf("after scanning %s, remaining data = %q; want %q", test.aterm, got, test.tail)
		}
	}
}

func TestAppendString(t *testing.T) {
	for _, test := range stringTests {
		got := string(AppendString(nil, test.s))
		if got != test.aterm {
			t.Errorf("AppendString(nil, %q) = %q; want %q", test.s, got, test.aterm)
		}
	}
}

func FuzzString(f *testing.F) {
	for _, test := range stringTests {
		f.Add(test.s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > maxStringLength {
			// We know that large strings return errors.
			return
		}

		aterm := AppendString(nil, s)
		r := bytes.NewReader(aterm)
		scanner := NewScanner(r)
		got, err := scanner.ReadToken()
		if err != nil {
			t.Fatal(err)
		}
		want := Token{Kind: String, Value: s}
		if got != want {
			t.Errorf("got %v; want %v", got, want)
		}
		if r.Len() > 0 {
			t.Errorf("trailing data %q", s[len(s)-r.Len():])
		}
		if got, err := scanner.ReadToken(); err != io.EOF {
			t.Errorf("ReadToken() #2 = %v, %v; want _, %v", got, err, io.EOF)
		}
	})
}
