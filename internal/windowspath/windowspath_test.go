// Copied from src/path/filepath/path_windows_test.go in go1.22.5.

// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
// SPDX-License-Identifier: BSD-3-Clause

package windowspath

import (
	"reflect"
	"runtime"
	"testing"
)

type PathTest struct {
	path, result string
}

var cleantests = []PathTest{
	// Already clean
	{"abc", "abc"},
	{"abc/def", "abc/def"},
	{"a/b/c", "a/b/c"},
	{".", "."},
	{"..", ".."},
	{"../..", "../.."},
	{"../../abc", "../../abc"},
	{"/abc", "/abc"},
	{"/", "/"},

	// Empty is current dir
	{"", "."},

	// Remove trailing slash
	{"abc/", "abc"},
	{"abc/def/", "abc/def"},
	{"a/b/c/", "a/b/c"},
	{"./", "."},
	{"../", ".."},
	{"../../", "../.."},
	{"/abc/", "/abc"},

	// Remove doubled slash
	{"abc//def//ghi", "abc/def/ghi"},
	{"abc//", "abc"},

	// Remove . elements
	{"abc/./def", "abc/def"},
	{"/./abc/def", "/abc/def"},
	{"abc/.", "abc"},

	// Remove .. elements
	{"abc/def/ghi/../jkl", "abc/def/jkl"},
	{"abc/def/../ghi/../jkl", "abc/jkl"},
	{"abc/def/..", "abc"},
	{"abc/def/../..", "."},
	{"/abc/def/../..", "/"},
	{"abc/def/../../..", ".."},
	{"/abc/def/../../..", "/"},
	{"abc/def/../../../ghi/jkl/../../../mno", "../../mno"},
	{"/../abc", "/abc"},
	{"a/../b:/../../c", `../c`},

	// Combinations
	{"abc/./../def", "def"},
	{"abc//./../def", "def"},
	{"abc/../../././../def", "../../def"},
}

var nonwincleantests = []PathTest{
	// Remove leading doubled slash
	{"//abc", "/abc"},
	{"///abc", "/abc"},
	{"//abc//", "/abc"},
}

var wincleantests = []PathTest{
	{`c:`, `c:.`},
	{`c:\`, `c:\`},
	{`c:\abc`, `c:\abc`},
	{`c:abc\..\..\.\.\..\def`, `c:..\..\def`},
	{`c:\abc\def\..\..`, `c:\`},
	{`c:\..\abc`, `c:\abc`},
	{`c:..\abc`, `c:..\abc`},
	{`c:\b:\..\..\..\d`, `c:\d`},
	{`\`, `\`},
	{`/`, `\`},
	{`\\i\..\c$`, `\\i\..\c$`},
	{`\\i\..\i\c$`, `\\i\..\i\c$`},
	{`\\i\..\I\c$`, `\\i\..\I\c$`},
	{`\\host\share\foo\..\bar`, `\\host\share\bar`},
	{`//host/share/foo/../baz`, `\\host\share\baz`},
	{`\\host\share\foo\..\..\..\..\bar`, `\\host\share\bar`},
	{`\\.\C:\a\..\..\..\..\bar`, `\\.\C:\bar`},
	{`\\.\C:\\\\a`, `\\.\C:\a`},
	{`\\a\b\..\c`, `\\a\b\c`},
	{`\\a\b`, `\\a\b`},
	{`.\c:`, `.\c:`},
	{`.\c:\foo`, `.\c:\foo`},
	{`.\c:foo`, `.\c:foo`},
	{`//abc`, `\\abc`},
	{`///abc`, `\\\abc`},
	{`//abc//`, `\\abc\\`},
	{`\\?\C:\`, `\\?\C:\`},
	{`\\?\C:\a`, `\\?\C:\a`},

	// Don't allow cleaning to move an element with a colon to the start of the path.
	{`a/../c:`, `.\c:`},
	{`a\..\c:`, `.\c:`},
	{`a/../c:/a`, `.\c:\a`},
	{`a/../../c:`, `..\c:`},
	{`foo:bar`, `foo:bar`},

	// Don't allow cleaning to create a Root Local Device path like \??\a.
	{`/a/../??/a`, `\.\??\a`},
}

func TestClean(t *testing.T) {
	tests := cleantests
	for i := range tests {
		tests[i].result = FromSlash(tests[i].result)
	}
	tests = append(tests, wincleantests...)
	for _, test := range tests {
		if s := Clean(test.path); s != test.result {
			t.Errorf("Clean(%q) = %q, want %q", test.path, s, test.result)
		}
		if s := Clean(test.result); s != test.result {
			t.Errorf("Clean(%q) = %q, want %q", test.result, s, test.result)
		}
	}

	if testing.Short() {
		t.Skip("skipping malloc count in short mode")
	}
	if runtime.GOMAXPROCS(0) > 1 {
		t.Log("skipping AllocsPerRun checks; GOMAXPROCS>1")
		return
	}

	for _, test := range tests {
		allocs := testing.AllocsPerRun(100, func() { Clean(test.result) })
		if allocs > 0 {
			t.Errorf("Clean(%q): %v allocs, want zero", test.result, allocs)
		}
	}
}

type IsLocalTest struct {
	path    string
	isLocal bool
}

var islocaltests = []IsLocalTest{
	{"", false},
	{".", true},
	{"..", false},
	{"../a", false},
	{"/", false},
	{"/a", false},
	{"/a/../..", false},
	{"a", true},
	{"a/../a", true},
	{"a/", true},
	{"a/.", true},
	{"a/./b/./c", true},
	{`a/../b:/../../c`, false},
}

var winislocaltests = []IsLocalTest{
	{"NUL", false},
	{"nul", false},
	{"nul ", false},
	{"nul.", false},
	{"a/nul:", false},
	{"a/nul : a", false},
	{"com0", true},
	{"com1", false},
	{"com2", false},
	{"com3", false},
	{"com4", false},
	{"com5", false},
	{"com6", false},
	{"com7", false},
	{"com8", false},
	{"com9", false},
	{"com¹", false},
	{"com²", false},
	{"com³", false},
	{"com¹ : a", false},
	{"cOm1", false},
	{"lpt1", false},
	{"LPT1", false},
	{"lpt³", false},
	{"./nul", false},
	{`\`, false},
	{`\a`, false},
	{`C:`, false},
	{`C:\a`, false},
	{`..\a`, false},
	{`a/../c:`, false},
	{`CONIN$`, false},
	{`conin$`, false},
	{`CONOUT$`, false},
	{`conout$`, false},
	{`dollar$`, true}, // not a special file name
}

var plan9islocaltests = []IsLocalTest{
	{"#a", false},
}

const sep = Separator

var slashtests = []PathTest{
	{"", ""},
	{"/", string(sep)},
	{"/a/b", string([]byte{sep, 'a', sep, 'b'})},
	{"a//b", string([]byte{'a', sep, sep, 'b'})},
}

func TestFromAndToSlash(t *testing.T) {
	for _, test := range slashtests {
		if s := FromSlash(test.path); s != test.result {
			t.Errorf("FromSlash(%q) = %q, want %q", test.path, s, test.result)
		}
		if s := ToSlash(test.result); s != test.path {
			t.Errorf("ToSlash(%q) = %q, want %q", test.result, s, test.path)
		}
	}
}

type SplitListTest struct {
	list   string
	result []string
}

const lsep = ListSeparator

var splitlisttests = []SplitListTest{
	{"", []string{}},
	{string([]byte{'a', lsep, 'b'}), []string{"a", "b"}},
	{string([]byte{lsep, 'a', lsep, 'b'}), []string{"", "a", "b"}},
}

var winsplitlisttests = []SplitListTest{
	// quoted
	{`"a"`, []string{`a`}},

	// semicolon
	{`";"`, []string{`;`}},
	{`"a;b"`, []string{`a;b`}},
	{`";";`, []string{`;`, ``}},
	{`;";"`, []string{``, `;`}},

	// partially quoted
	{`a";"b`, []string{`a;b`}},
	{`a; ""b`, []string{`a`, ` b`}},
	{`"a;b`, []string{`a;b`}},
	{`""a;b`, []string{`a`, `b`}},
	{`"""a;b`, []string{`a;b`}},
	{`""""a;b`, []string{`a`, `b`}},
	{`a";b`, []string{`a;b`}},
	{`a;b";c`, []string{`a`, `b;c`}},
	{`"a";b";c`, []string{`a`, `b;c`}},
}

func TestSplitList(t *testing.T) {
	tests := splitlisttests
	tests = append(tests, winsplitlisttests...)
	for _, test := range tests {
		if l := SplitList(test.list); !reflect.DeepEqual(l, test.result) {
			t.Errorf("SplitList(%#q) = %#q, want %#q", test.list, l, test.result)
		}
	}
}

type SplitTest struct {
	path, dir, file string
}

var unixsplittests = []SplitTest{
	{"a/b", "a/", "b"},
	{"a/b/", "a/b/", ""},
	{"a/", "a/", ""},
	{"a", "", "a"},
	{"/", "/", ""},
}

var winsplittests = []SplitTest{
	{`c:`, `c:`, ``},
	{`c:/`, `c:/`, ``},
	{`c:/foo`, `c:/`, `foo`},
	{`c:/foo/bar`, `c:/foo/`, `bar`},
	{`//host/share`, `//host/share`, ``},
	{`//host/share/`, `//host/share/`, ``},
	{`//host/share/foo`, `//host/share/`, `foo`},
	{`\\host\share`, `\\host\share`, ``},
	{`\\host\share\`, `\\host\share\`, ``},
	{`\\host\share\foo`, `\\host\share\`, `foo`},
}

func TestSplit(t *testing.T) {
	var splittests []SplitTest
	splittests = unixsplittests
	splittests = append(splittests, winsplittests...)
	for _, test := range splittests {
		if d, f := Split(test.path); d != test.dir || f != test.file {
			t.Errorf("Split(%q) = %q, %q, want %q, %q", test.path, d, f, test.dir, test.file)
		}
	}
}

type JoinTest struct {
	elem []string
	path string
}

var jointests = []JoinTest{
	// zero parameters
	{[]string{}, ""},

	// one parameter
	{[]string{""}, ""},
	{[]string{"/"}, "/"},
	{[]string{"a"}, "a"},

	// two parameters
	{[]string{"a", "b"}, "a/b"},
	{[]string{"a", ""}, "a"},
	{[]string{"", "b"}, "b"},
	{[]string{"/", "a"}, "/a"},
	{[]string{"/", "a/b"}, "/a/b"},
	{[]string{"/", ""}, "/"},
	{[]string{"/a", "b"}, "/a/b"},
	{[]string{"a", "/b"}, "a/b"},
	{[]string{"/a", "/b"}, "/a/b"},
	{[]string{"a/", "b"}, "a/b"},
	{[]string{"a/", ""}, "a"},
	{[]string{"", ""}, ""},

	// three parameters
	{[]string{"/", "a", "b"}, "/a/b"},
}

var nonwinjointests = []JoinTest{
	{[]string{"//", "a"}, "/a"},
}

var winjointests = []JoinTest{
	{[]string{`directory`, `file`}, `directory\file`},
	{[]string{`C:\Windows\`, `System32`}, `C:\Windows\System32`},
	{[]string{`C:\Windows\`, ``}, `C:\Windows`},
	{[]string{`C:\`, `Windows`}, `C:\Windows`},
	{[]string{`C:`, `a`}, `C:a`},
	{[]string{`C:`, `a\b`}, `C:a\b`},
	{[]string{`C:`, `a`, `b`}, `C:a\b`},
	{[]string{`C:`, ``, `b`}, `C:b`},
	{[]string{`C:`, ``, ``, `b`}, `C:b`},
	{[]string{`C:`, ``}, `C:.`},
	{[]string{`C:`, ``, ``}, `C:.`},
	{[]string{`C:`, `\a`}, `C:\a`},
	{[]string{`C:`, ``, `\a`}, `C:\a`},
	{[]string{`C:.`, `a`}, `C:a`},
	{[]string{`C:a`, `b`}, `C:a\b`},
	{[]string{`C:a`, `b`, `d`}, `C:a\b\d`},
	{[]string{`\\host\share`, `foo`}, `\\host\share\foo`},
	{[]string{`\\host\share\foo`}, `\\host\share\foo`},
	{[]string{`//host/share`, `foo/bar`}, `\\host\share\foo\bar`},
	{[]string{`\`}, `\`},
	{[]string{`\`, ``}, `\`},
	{[]string{`\`, `a`}, `\a`},
	{[]string{`\\`, `a`}, `\\a`},
	{[]string{`\`, `a`, `b`}, `\a\b`},
	{[]string{`\\`, `a`, `b`}, `\\a\b`},
	{[]string{`\`, `\\a\b`, `c`}, `\a\b\c`},
	{[]string{`\\a`, `b`, `c`}, `\\a\b\c`},
	{[]string{`\\a\`, `b`, `c`}, `\\a\b\c`},
	{[]string{`//`, `a`}, `\\a`},
	{[]string{`a:\b\c`, `x\..\y:\..\..\z`}, `a:\b\z`},
	{[]string{`\`, `??\a`}, `\.\??\a`},
}

func TestJoin(t *testing.T) {
	jointests = append(jointests, winjointests...)
	for _, test := range jointests {
		expected := FromSlash(test.path)
		if p := Join(test.elem...); p != expected {
			t.Errorf("join(%q) = %q, want %q", test.elem, p, expected)
		}
	}
}

type ExtTest struct {
	path, ext string
}

var exttests = []ExtTest{
	{"path.go", ".go"},
	{"path.pb.go", ".go"},
	{"a.dir/b", ""},
	{"a.dir/b.go", ".go"},
	{"a.dir/", ""},
}

func TestExt(t *testing.T) {
	for _, test := range exttests {
		if x := Ext(test.path); x != test.ext {
			t.Errorf("Ext(%q) = %q, want %q", test.path, x, test.ext)
		}
	}
}

type Node struct {
	name    string
	entries []*Node // nil if the entry is a file
	mark    int
}

var tree = &Node{
	"testdata",
	[]*Node{
		{"a", nil, 0},
		{"b", []*Node{}, 0},
		{"c", nil, 0},
		{
			"d",
			[]*Node{
				{"x", nil, 0},
				{"y", []*Node{}, 0},
				{
					"z",
					[]*Node{
						{"u", nil, 0},
						{"v", nil, 0},
					},
					0,
				},
			},
			0,
		},
	},
	0,
}

var basetests = []PathTest{
	{"", "."},
	{".", "."},
	{"/.", "."},
	{"/", "/"},
	{"////", "/"},
	{"x/", "x"},
	{"abc", "abc"},
	{"abc/def", "def"},
	{"a/b/.x", ".x"},
	{"a/b/c.", "c."},
	{"a/b/c.x", "c.x"},
}

var winbasetests = []PathTest{
	{`c:\`, `\`},
	{`c:.`, `.`},
	{`c:\a\b`, `b`},
	{`c:a\b`, `b`},
	{`c:a\b\c`, `c`},
	{`\\host\share\`, `\`},
	{`\\host\share\a`, `a`},
	{`\\host\share\a\b`, `b`},
}

func TestBase(t *testing.T) {
	tests := basetests
	// make unix tests work on windows
	for i := range tests {
		tests[i].result = Clean(tests[i].result)
	}
	// add windows specific tests
	tests = append(tests, winbasetests...)
	for _, test := range tests {
		if s := Base(test.path); s != test.result {
			t.Errorf("Base(%q) = %q, want %q", test.path, s, test.result)
		}
	}
}

var dirtests = []PathTest{
	{"", "."},
	{".", "."},
	{"/.", "/"},
	{"/", "/"},
	{"/foo", "/"},
	{"x/", "x"},
	{"abc", "."},
	{"abc/def", "abc"},
	{"a/b/.x", "a/b"},
	{"a/b/c.", "a/b"},
	{"a/b/c.x", "a/b"},
}

var nonwindirtests = []PathTest{
	{"////", "/"},
}

var windirtests = []PathTest{
	{`c:\`, `c:\`},
	{`c:.`, `c:.`},
	{`c:\a\b`, `c:\a`},
	{`c:a\b`, `c:a`},
	{`c:a\b\c`, `c:a\b`},
	{`\\host\share`, `\\host\share`},
	{`\\host\share\`, `\\host\share\`},
	{`\\host\share\a`, `\\host\share\`},
	{`\\host\share\a\b`, `\\host\share\a`},
	{`\\\\`, `\\\\`},
}

func TestDir(t *testing.T) {
	tests := dirtests
	// make unix tests work on windows
	for i := range tests {
		tests[i].result = Clean(tests[i].result)
	}
	// add windows specific tests
	tests = append(tests, windirtests...)
	for _, test := range tests {
		if s := Dir(test.path); s != test.result {
			t.Errorf("Dir(%q) = %q, want %q", test.path, s, test.result)
		}
	}
}

type IsAbsTest struct {
	path  string
	isAbs bool
}

var isabstests = []IsAbsTest{
	{"", false},
	{"/", true},
	{"/usr/bin/gcc", true},
	{"..", false},
	{"/a/../bb", true},
	{".", false},
	{"./", false},
	{"lala", false},
}

var winisabstests = []IsAbsTest{
	{`C:\`, true},
	{`c\`, false},
	{`c::`, false},
	{`c:`, false},
	{`/`, false},
	{`\`, false},
	{`\Windows`, false},
	{`c:a\b`, false},
	{`c:\a\b`, true},
	{`c:/a/b`, true},
	{`\\host\share`, true},
	{`\\host\share\`, true},
	{`\\host\share\foo`, true},
	{`//host/share/foo/bar`, true},
	{`\\?\a\b\c`, true},
	{`\??\a\b\c`, true},
}

func TestIsAbs(t *testing.T) {
	var tests []IsAbsTest
	tests = append(tests, winisabstests...)
	// All non-windows tests should fail, because they have no volume letter.
	for _, test := range isabstests {
		tests = append(tests, IsAbsTest{test.path, false})
	}
	// All non-windows test should work as intended if prefixed with volume letter.
	for _, test := range isabstests {
		tests = append(tests, IsAbsTest{"c:" + test.path, test.isAbs})
	}

	for _, test := range tests {
		if r := IsAbs(test.path); r != test.isAbs {
			t.Errorf("IsAbs(%q) = %v, want %v", test.path, r, test.isAbs)
		}
	}
}

type EvalSymlinksTest struct {
	// If dest is empty, the path is created; otherwise the dest is symlinked to the path.
	path, dest string
}

var EvalSymlinksTestDirs = []EvalSymlinksTest{
	{"test", ""},
	{"test/dir", ""},
	{"test/dir/link3", "../../"},
	{"test/link1", "../test"},
	{"test/link2", "dir"},
	{"test/linkabs", "/"},
	{"test/link4", "../test2"},
	{"test2", "test/dir"},
	// Issue 23444.
	{"src", ""},
	{"src/pool", ""},
	{"src/pool/test", ""},
	{"src/versions", ""},
	{"src/versions/current", "../../version"},
	{"src/versions/v1", ""},
	{"src/versions/v1/modules", ""},
	{"src/versions/v1/modules/test", "../../../pool/test"},
	{"version", "src/versions/v1"},
}

var EvalSymlinksTests = []EvalSymlinksTest{
	{"test", "test"},
	{"test/dir", "test/dir"},
	{"test/dir/../..", "."},
	{"test/link1", "test"},
	{"test/link2", "test/dir"},
	{"test/link1/dir", "test/dir"},
	{"test/link2/..", "test"},
	{"test/dir/link3", "."},
	{"test/link2/link3/test", "test"},
	{"test/linkabs", "/"},
	{"test/link4/..", "test"},
	{"src/versions/current/modules/test", "src/pool/test"},
}

// simpleJoin builds a file name from the directory and path.
// It does not use Join because we don't want ".." to be evaluated.
func simpleJoin(dir, path string) string {
	return dir + string(Separator) + path
}

// Test directories relative to temporary directory.
// The tests are run in absTestDirs[0].
var absTestDirs = []string{
	"a",
	"a/b",
	"a/b/c",
}

// Test paths relative to temporary directory. $ expands to the directory.
// The tests are run in absTestDirs[0].
// We create absTestDirs first.
var absTests = []string{
	".",
	"b",
	"b/",
	"../a",
	"../a/b",
	"../a/b/./c/../../.././a",
	"../a/b/./c/../../.././a/",
	"$",
	"$/.",
	"$/a/../a/b",
	"$/a/b/c/../../.././a",
	"$/a/b/c/../../.././a/",
}

type VolumeNameTest struct {
	path string
	vol  string
}

var volumenametests = []VolumeNameTest{
	{`c:/foo/bar`, `c:`},
	{`c:`, `c:`},
	{`c:\`, `c:`},
	{`2:`, `2:`},
	{``, ``},
	{`\\\host`, `\\\host`},
	{`\\\host\`, `\\\host`},
	{`\\\host\share`, `\\\host`},
	{`\\\host\\share`, `\\\host`},
	{`\\host`, `\\host`},
	{`//host`, `\\host`},
	{`\\host\`, `\\host\`},
	{`//host/`, `\\host\`},
	{`\\host\share`, `\\host\share`},
	{`//host/share`, `\\host\share`},
	{`\\host\share\`, `\\host\share`},
	{`//host/share/`, `\\host\share`},
	{`\\host\share\foo`, `\\host\share`},
	{`//host/share/foo`, `\\host\share`},
	{`\\host\share\\foo\\\bar\\\\baz`, `\\host\share`},
	{`//host/share//foo///bar////baz`, `\\host\share`},
	{`\\host\share\foo\..\bar`, `\\host\share`},
	{`//host/share/foo/../bar`, `\\host\share`},
	{`//.`, `\\.`},
	{`//./`, `\\.\`},
	{`//./NUL`, `\\.\NUL`},
	{`//?`, `\\?`},
	{`//?/`, `\\?\`},
	{`//?/NUL`, `\\?\NUL`},
	{`/??`, `\??`},
	{`/??/`, `\??\`},
	{`/??/NUL`, `\??\NUL`},
	{`//./a/b`, `\\.\a`},
	{`//./C:`, `\\.\C:`},
	{`//./C:/`, `\\.\C:`},
	{`//./C:/a/b/c`, `\\.\C:`},
	{`//./UNC/host/share/a/b/c`, `\\.\UNC\host\share`},
	{`//./UNC/host`, `\\.\UNC\host`},
	{`//./UNC/host\`, `\\.\UNC\host\`},
	{`//./UNC`, `\\.\UNC`},
	{`//./UNC/`, `\\.\UNC\`},
	{`\\?\x`, `\\?\x`},
	{`\??\x`, `\??\x`},
}

func TestVolumeName(t *testing.T) {
	for _, v := range volumenametests {
		if vol := VolumeName(v.path); vol != v.vol {
			t.Errorf("VolumeName(%q)=%q, want %q", v.path, vol, v.vol)
		}
	}
}

func TestIssue52476(t *testing.T) {
	tests := []struct {
		lhs, rhs string
		want     string
	}{
		{`..\.`, `C:`, `..\C:`},
		{`..`, `C:`, `..\C:`},
		{`.`, `:`, `.\:`},
		{`.`, `C:`, `.\C:`},
		{`.`, `C:/a/b/../c`, `.\C:\a\c`},
		{`.`, `\C:`, `.\C:`},
		{`C:\`, `.`, `C:\`},
		{`C:\`, `C:\`, `C:\C:`},
		{`C`, `:`, `C\:`},
		{`\.`, `C:`, `\C:`},
		{`\`, `C:`, `\C:`},
	}

	for _, test := range tests {
		got := Join(test.lhs, test.rhs)
		if got != test.want {
			t.Errorf(`Join(%q, %q): got %q, want %q`, test.lhs, test.rhs, got, test.want)
		}
	}
}
