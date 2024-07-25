package zb

import (
	posixpath "path"
	"strings"
	"testing"

	"zombiezen.com/go/zb/internal/windowspath"
)

var storePathTests = []struct {
	path    string
	windows bool
	err     bool

	dir          StoreDirectory
	base         string
	digestPart   string
	namePart     string
	isDerivation bool
}{
	{
		path: "",
		err:  true,
	},
	{
		path: "foo",
		err:  true,
	},
	{
		path: "foo/ffffffffffffffffffffffffffffffff-x",
		err:  true,
	},
	{
		path: "/nix/store",
		err:  true,
	},
	{
		path:    `C:\zb\store`,
		windows: true,
		err:     true,
	},
	{
		path: "/zb/store/ffffffffffffffffffffffffffffffff",
		err:  true,
	},
	{
		path: "/zb/store/ffffffffffffffffffffffffffffffff-",
		err:  true,
	},
	{
		path: "/zb/store/ffffffffffffffffffffffffffffffff_x",
		err:  true,
	},
	{
		path: "/zb/store/ffffffffffffffffffffffffffffffff-" + strings.Repeat("x", 212),
		err:  true,
	},
	{
		path: "/zb/store/ffffffffffffffffffffffffffffffff-foo@bar",
		err:  true,
	},
	{
		path: "/zb/store/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee-x",
		err:  true,
	},
	{
		path: "/zb/store/00bgd045z0d4icpbc2yy-net-tools-1.60_p20170221182432",
		err:  true,
	},
	{
		path: "/zb/store/00bgd045z0d4icpbc2yyz4gx48aku4la-net-tools-1.60_p20170221182432",
		err:  true,
	},
	{
		path:       "/zb/store/ffffffffffffffffffffffffffffffff-x",
		dir:        "/zb/store",
		base:       "ffffffffffffffffffffffffffffffff-x",
		digestPart: "ffffffffffffffffffffffffffffffff",
		namePart:   "x",
	},
	{
		path:       `C:\zb\store\ffffffffffffffffffffffffffffffff-x`,
		windows:    true,
		dir:        `C:\zb\store`,
		base:       "ffffffffffffffffffffffffffffffff-x",
		digestPart: "ffffffffffffffffffffffffffffffff",
		namePart:   "x",
	},
	{
		path:       "/zb/store/ffffffffffffffffffffffffffffffff-x/",
		dir:        "/zb/store",
		base:       "ffffffffffffffffffffffffffffffff-x",
		digestPart: "ffffffffffffffffffffffffffffffff",
		namePart:   "x",
	},
	{
		path:       "/zb/store/foo/../ffffffffffffffffffffffffffffffff-x",
		dir:        "/zb/store",
		base:       "ffffffffffffffffffffffffffffffff-x",
		digestPart: "ffffffffffffffffffffffffffffffff",
		namePart:   "x",
	},
	{
		path:       "/zb/store/s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1",
		dir:        "/zb/store",
		base:       "s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1",
		digestPart: "s66mzxpvicwk07gjbjfw9izjfa797vsw",
		namePart:   "hello-2.12.1",
	},
	{
		path:       `C:\zb\store\s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1`,
		windows:    true,
		dir:        `C:\zb\store`,
		base:       "s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1",
		digestPart: "s66mzxpvicwk07gjbjfw9izjfa797vsw",
		namePart:   "hello-2.12.1",
	},
	{
		path:         "/zb/store/ib3sh3pcz10wsmavxvkdbayhqivbghlq-hello-2.12.1.drv",
		dir:          "/zb/store",
		base:         "ib3sh3pcz10wsmavxvkdbayhqivbghlq-hello-2.12.1.drv",
		digestPart:   "ib3sh3pcz10wsmavxvkdbayhqivbghlq",
		namePart:     "hello-2.12.1.drv",
		isDerivation: true,
	},
	{
		path:       "/zb/store/00bgd045z0d4icpbc2yyz4gx48ak44la-net-tools-1.60_p20170221182432",
		dir:        "/zb/store",
		base:       "00bgd045z0d4icpbc2yyz4gx48ak44la-net-tools-1.60_p20170221182432",
		digestPart: "00bgd045z0d4icpbc2yyz4gx48ak44la",
		namePart:   "net-tools-1.60_p20170221182432",
	},
}

func TestParseStorePath(t *testing.T) {
	for _, test := range storePathTests {
		storePath, err := ParseStorePath(test.path)
		if test.err {
			if err == nil {
				t.Errorf("ParseStorePath(%q) = %q, <nil>; want _, <error>", test.path, storePath)
			}
			continue
		}
		if want := StorePath(cleanPathForTest(test.path, test.windows)); storePath != want || err != nil {
			t.Errorf("ParseStorePath(%q) = %q, %v; want %q, <nil>", test.path, storePath, err, want)
		}
		if err != nil {
			continue
		}
		if got, want := storePath.Dir(), test.dir; got != want {
			t.Errorf("ParseStorePath(%q).Dir() = %q; want %q", test.path, got, want)
		}
		if got, want := storePath.Base(), test.base; got != want {
			t.Errorf("ParseStorePath(%q).Base() = %q; want %q", test.path, got, want)
		}
		if got, want := storePath.Digest(), test.digestPart; got != want {
			t.Errorf("ParseStorePath(%q).Digest() = %q; want %q", test.path, got, want)
		}
		if got, want := storePath.Name(), test.namePart; got != want {
			t.Errorf("ParseStorePath(%q).Name() = %q; want %q", test.path, got, want)
		}
		if got, want := storePath.IsDerivation(), test.isDerivation; got != want {
			t.Errorf("ParseStorePath(%q).IsDerivation() = %t; want %t", test.path, got, want)
		}
	}
}

func TestStoreDirectoryObject(t *testing.T) {
	for _, test := range storePathTests {
		if test.err {
			continue
		}
		got, err := test.dir.Object(test.base)
		want := StorePath(cleanPathForTest(test.path, test.windows))
		if got != want || err != nil {
			t.Errorf("StoreDirectory(%q).Object(%q) = %q, %v; want %q, <nil>",
				test.dir, test.base, got, err, want)
		}
	}

	badObjectNames := []string{
		"",
		".",
		"..",
		"foo/bar",
	}
	for _, name := range badObjectNames {
		got, err := DefaultUnixStoreDirectory.Object(name)
		if err == nil {
			t.Errorf("StoreDirectory(%q).Object(%q) = %q, <nil>; want _, <error>",
				DefaultUnixStoreDirectory, name, got)
		}
		got, err = DefaultWindowsStoreDirectory.Object(name)
		if err == nil {
			t.Errorf("StoreDirectory(%q).Object(%q) = %q, <nil>; want _, <error>",
				DefaultWindowsStoreDirectory, name, got)
		}
	}
}

func TestStoreDirectoryParsePath(t *testing.T) {
	type parsePathTest struct {
		dir  StoreDirectory
		path string

		want StorePath
		sub  string
		err  bool
	}
	tests := []parsePathTest{
		{
			dir:  "/zb/store",
			path: "/zb/store/s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1/bin/hello",

			want: "/zb/store/s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1",
			sub:  "bin/hello",
		},
		{
			dir:  `C:\zb\store`,
			path: `C:\zb\store\s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1\bin\hello`,

			want: `C:\zb\store\s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1`,
			sub:  `bin\hello`,
		},
		{
			dir:  "/zb/store",
			path: "",
			err:  true,
		},
		{
			dir:  "/zb/store",
			path: "",
			err:  true,
		},
		{
			dir:  "/zb/store",
			path: "zb/store",
			err:  true,
		},
		{
			dir:  "foo",
			path: "foo/ffffffffffffffffffffffffffffffff-x",
			err:  true,
		},
		{
			dir:  "/foo",
			path: "/bar/ffffffffffffffffffffffffffffffff-x",
			err:  true,
		},
		{
			dir:  "/foo",
			path: "/foo/ffffffffffffffffffffffffffffffff-x/../../bar/ffffffffffffffffffffffffffffffff-x",
			err:  true,
		},
		{
			dir:  "/zb",
			path: "/zb/store",
			err:  true,
		},
		{
			dir:  "/zb/store",
			path: "/zb/store/00bgd045z0d4icpbc2yyz4gx48ak44la-net-tools-1.60_p20170221182432/bin/arp",
			want: "/zb/store/00bgd045z0d4icpbc2yyz4gx48ak44la-net-tools-1.60_p20170221182432",
			sub:  "bin/arp",
		},
	}
	for _, test := range storePathTests {
		if test.err {
			continue
		}
		tests = append(tests, parsePathTest{
			dir:  test.dir,
			path: test.path,
			want: StorePath(cleanPathForTest(test.path, test.windows)),
		})
	}

	for _, test := range tests {
		got, sub, err := test.dir.ParsePath(test.path)
		if got != test.want || sub != test.sub || (err != nil) != test.err {
			errString := "<nil>"
			if test.err {
				errString = "<error>"
			}
			t.Errorf("StoreDirectory(%q).ParsePath(%q) = %q, %q, %v; want %q, %q, %s",
				test.dir, test.path, got, sub, err, test.want, test.sub, errString)
		}
	}
}

func cleanPathForTest(path string, windows bool) string {
	if windows {
		return windowspath.Clean(path)
	} else {
		return posixpath.Clean(path)
	}
}
