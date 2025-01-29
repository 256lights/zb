// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package lualex

import (
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestScanner(t *testing.T) {
	tests := []struct {
		s    string
		want []Token
		bad  bool
	}{
		{s: "", want: []Token{}},
		{
			s: "foo",
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 1), Value: "foo"},
			},
		},
		{
			s: "  foo  ",
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 3), Value: "foo"},
			},
		},
		{
			s: "3",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "3"},
			},
		},
		{
			s: "345",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "345"},
			},
		},
		{
			s: "0xff",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "0xff"},
			},
		},
		{
			s: "0xBEBADA",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "0xBEBADA"},
			},
		},
		{
			s: "3.0",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "3.0"},
			},
		},
		{
			s: "3.1416",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "3.1416"},
			},
		},
		{
			s: "314.16e-2",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "314.16e-2"},
			},
		},
		{
			s: "0.31416E1",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "0.31416E1"},
			},
		},
		{
			s: "34e1",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "34e1"},
			},
		},
		{
			s: "0x0.1E",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "0x0.1E"},
			},
		},
		{
			s: "0xA23p-4",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "0xA23p-4"},
			},
		},
		{
			s: "0X1.921FB54442D18P+1",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "0X1.921FB54442D18P+1"},
			},
		},
		{
			s: "5.",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: "5."},
			},
		},
		{
			s: ".5",
			want: []Token{
				{Kind: NumeralToken, Position: Pos(1, 1), Value: ".5"},
			},
		},
		{
			s: `a = 'alo\n123"'`,
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 1), Value: "a"},
				{Kind: AssignToken, Position: Pos(1, 3)},
				{Kind: StringToken, Position: Pos(1, 5), Value: "alo\n123\""},
			},
		},
		{
			s: `a = "alo\n123\""`,
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 1), Value: "a"},
				{Kind: AssignToken, Position: Pos(1, 3)},
				{Kind: StringToken, Position: Pos(1, 5), Value: "alo\n123\""},
			},
		},
		{
			s: "a = [[alo\n123\"]]",
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 1), Value: "a"},
				{Kind: AssignToken, Position: Pos(1, 3)},
				{Kind: StringToken, Position: Pos(1, 5), Value: "alo\n123\""},
			},
		},
		{
			s: "a = [==[alo\n123\"]==]",
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 1), Value: "a"},
				{Kind: AssignToken, Position: Pos(1, 3)},
				{Kind: StringToken, Position: Pos(1, 5), Value: "alo\n123\""},
			},
		},
		{
			s: `a = "xyz`,
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 1), Value: "a"},
				{Kind: AssignToken, Position: Pos(1, 3)},
				{Kind: ErrorToken, Position: Pos(1, 5)},
			},
			bad: true,
		},
		{
			s: `a = 'xyz`,
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 1), Value: "a"},
				{Kind: AssignToken, Position: Pos(1, 3)},
				{Kind: ErrorToken, Position: Pos(1, 5)},
			},
			bad: true,
		},
		{
			s: "a = 'xyz\nabc'",
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 1), Value: "a"},
				{Kind: AssignToken, Position: Pos(1, 3)},
				{Kind: ErrorToken, Position: Pos(1, 5)},
			},
			bad: true,
		},
		{
			s: `a = [[xyz`,
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 1), Value: "a"},
				{Kind: AssignToken, Position: Pos(1, 3)},
				{Kind: ErrorToken, Position: Pos(1, 5)},
			},
			bad: true,
		},
		{
			s: ` --[[ foo`,
			want: []Token{
				{Kind: ErrorToken, Position: Pos(1, 2)},
			},
			bad: true,
		},
		{
			s: "goto",
			want: []Token{
				{Kind: GotoToken, Position: Pos(1, 1)},
			},
		},
		{
			s: "-- hello comment\ntest\n2 + 2\n",
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(2, 1), Value: "test"},
				{Kind: NumeralToken, Position: Pos(3, 1), Value: "2"},
				{Kind: AddToken, Position: Pos(3, 3)},
				{Kind: NumeralToken, Position: Pos(3, 5), Value: "2"},
			},
		},
		{
			s: "--[=[ hello comment\nfake-out: ]]\n]=]\ntest\n2 + 2\n",
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(4, 1), Value: "test"},
				{Kind: NumeralToken, Position: Pos(5, 1), Value: "2"},
				{Kind: AddToken, Position: Pos(5, 3)},
				{Kind: NumeralToken, Position: Pos(5, 5), Value: "2"},
			},
		},
		{
			s: ".",
			want: []Token{
				{Kind: DotToken, Position: Pos(1, 1)},
			},
		},
		{
			s: "..",
			want: []Token{
				{Kind: ConcatToken, Position: Pos(1, 1)},
			},
		},
		{
			s: "...",
			want: []Token{
				{Kind: VarargToken, Position: Pos(1, 1)},
			},
		},
		{
			s: "....",
			want: []Token{
				{Kind: VarargToken, Position: Pos(1, 1)},
				{Kind: DotToken, Position: Pos(1, 4)},
			},
		},
		{
			s: ".....",
			want: []Token{
				{Kind: VarargToken, Position: Pos(1, 1)},
				{Kind: ConcatToken, Position: Pos(1, 4)},
			},
		},
		{
			s: ":",
			want: []Token{
				{Kind: ColonToken, Position: Pos(1, 1)},
			},
		},
		{
			s: "::",
			want: []Token{
				{Kind: LabelToken, Position: Pos(1, 1)},
			},
		},
		{
			s: "[=",
			want: []Token{
				{Kind: LBracketToken, Position: Pos(1, 1)},
				{Kind: AssignToken, Position: Pos(1, 2)},
			},
		},
		{
			s: "[==",
			want: []Token{
				{Kind: LBracketToken, Position: Pos(1, 1)},
				{Kind: EqualToken, Position: Pos(1, 2)},
			},
		},
		{
			s: "[===",
			want: []Token{
				{Kind: LBracketToken, Position: Pos(1, 1)},
				{Kind: EqualToken, Position: Pos(1, 2)},
				{Kind: AssignToken, Position: Pos(1, 4)},
			},
		},
		{
			s: "[===abc",
			want: []Token{
				{Kind: LBracketToken, Position: Pos(1, 1)},
				{Kind: EqualToken, Position: Pos(1, 2)},
				{Kind: AssignToken, Position: Pos(1, 4)},
				{Kind: IdentifierToken, Position: Pos(1, 5), Value: "abc"},
			},
		},
		{
			s: `res = (h >> (32 - floatbits)) % 2^32`,
			want: []Token{
				{Kind: IdentifierToken, Position: Pos(1, 1), Value: "res"},
				{Kind: AssignToken, Position: Pos(1, 5)},
				{Kind: LParenToken, Position: Pos(1, 7)},
				{Kind: IdentifierToken, Position: Pos(1, 8), Value: "h"},
				{Kind: RShiftToken, Position: Pos(1, 10)},
				{Kind: LParenToken, Position: Pos(1, 13)},
				{Kind: NumeralToken, Position: Pos(1, 14), Value: "32"},
				{Kind: SubToken, Position: Pos(1, 17)},
				{Kind: IdentifierToken, Position: Pos(1, 19), Value: "floatbits"},
				{Kind: RParenToken, Position: Pos(1, 28)},
				{Kind: RParenToken, Position: Pos(1, 29)},
				{Kind: ModToken, Position: Pos(1, 31)},
				{Kind: NumeralToken, Position: Pos(1, 33), Value: "2"},
				{Kind: PowToken, Position: Pos(1, 34)},
				{Kind: NumeralToken, Position: Pos(1, 35), Value: "32"},
			},
		},
	}

	for _, test := range tests {
		s := NewScanner(strings.NewReader(test.s))
		var got []Token
		for {
			tok, err := s.Scan()
			if err != io.EOF {
				got = append(got, tok)
			}
			switch {
			case err == io.EOF && test.bad:
				t.Errorf("scan of %q did not return an error", test.s)
			case err != nil && err != io.EOF && test.bad:
				t.Logf("scan of %q returned (expected) error: %v", test.s, err)
			case err != nil && err != io.EOF && !test.bad:
				t.Errorf("scan of %q error: %v", test.s, err)
			}
			if err != nil {
				break
			}
		}
		if diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("scan of %q (-want +got):\n%s", test.s, diff)
		}
	}
}

func TestUnquote(t *testing.T) {
	tests := []struct {
		s    string
		want string
		err  bool
	}{
		{
			s:    `""`,
			want: "",
		},
		{
			s:    `''`,
			want: "",
		},
		{
			s:    `"abc"`,
			want: "abc",
		},
		{
			s:    `'abc'`,
			want: "abc",
		},

		// Invalid UTF-8 code points.
		{
			s:    `"\u{110000}"`,
			want: "\xf4\x90\x80\x80",
		},
		{
			s:    `"\u{7FFFFFFF}"`,
			want: "\xfd\xbf\xbf\xbf\xbf\xbf",
		},
		{
			s:   `"\u{80000000}"`,
			err: true,
		},
	}

	for _, test := range tests {
		got, err := Unquote(test.s)
		if got != test.want || (err != nil) != test.err {
			errString := "<nil>"
			if test.err {
				errString = "<error>"
			}
			t.Errorf("Unquote(%q) = %q, %v; want %q, %s", test.s, got, err, test.want, errString)
		}
	}
}

func FuzzQuote(f *testing.F) {
	f.Add("")
	f.Add("abc")
	f.Add("Hello, 世界")
	f.Add("abc\nxyz")
	f.Add("abc\x00xyz")
	f.Add("\x00\x01\x023\x05\x009")
	f.Add("\x00\xe4\x00b8c\x00")
	f.Add("\x7f\x80")

	f.Fuzz(func(t *testing.T, s string) {
		luaString := Quote(s)
		got, err := Unquote(luaString)
		if got != s || err != nil {
			t.Errorf("Unquote(Quote(%q)) = %q, %v; want %q, <nil> (Quote(...) = %q)",
				s, got, err, s, luaString)
		}
	})
}
