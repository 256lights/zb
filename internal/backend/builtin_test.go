// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/google/go-cmp/cmp"
)

func TestExtractTar(t *testing.T) {
	type tarEntry struct {
		header tar.Header
		data   []byte
	}

	tests := []struct {
		name         string
		entries      []tarEntry
		dst          string
		want         fs.FS
		wantStripped fs.FS
	}{
		{
			name:    "Empty",
			entries: []tarEntry{},
			dst:     "foo",
			want: fstest.MapFS{
				"foo": {Mode: fs.ModeDir},
			},
		},
		{
			name: "SingleFile",
			entries: []tarEntry{
				{
					header: tar.Header{
						Name: "foo.txt",
						Size: int64(len("Hello, World!\n")),
					},
					data: []byte("Hello, World!\n"),
				},
			},
			dst: "foo",
			want: fstest.MapFS{
				"foo/foo.txt": {Data: []byte("Hello, World!\n")},
			},
			wantStripped: fstest.MapFS{
				"foo": {Data: []byte("Hello, World!\n")},
			},
		},
		{
			name: "MultipleFiles",
			entries: []tarEntry{
				{
					header: tar.Header{
						Name: "foo.txt",
						Size: int64(len("Hello, World!\n")),
					},
					data: []byte("Hello, World!\n"),
				},
				{
					header: tar.Header{
						Name: "bar.txt",
						Size: int64(len("again?\n")),
					},
					data: []byte("again?\n"),
				},
			},
			dst: "foo",
			want: fstest.MapFS{
				"foo/foo.txt": {Data: []byte("Hello, World!\n")},
				"foo/bar.txt": {Data: []byte("again?\n")},
			},
		},
		{
			name: "Directory",
			entries: []tarEntry{
				{
					header: tar.Header{Name: "a/"},
				},
				{
					header: tar.Header{
						Name: "a/b.txt",
						Size: int64(len("Hello, World!\n")),
					},
					data: []byte("Hello, World!\n"),
				},
				{
					header: tar.Header{
						Name: "a/c.txt",
						Size: int64(len("again?\n")),
					},
					data: []byte("again?\n"),
				},
				{
					header: tar.Header{Name: "a/d/"},
				},
				{
					header: tar.Header{
						Name: "a/d/e.txt",
						Size: int64(len("eeeee")),
					},
					data: []byte("eeeee"),
				},
			},
			dst: "foo",
			want: fstest.MapFS{
				"foo/a/b.txt":   {Data: []byte("Hello, World!\n")},
				"foo/a/c.txt":   {Data: []byte("again?\n")},
				"foo/a/d/e.txt": {Data: []byte("eeeee")},
			},
			wantStripped: fstest.MapFS{
				"foo/b.txt":   {Data: []byte("Hello, World!\n")},
				"foo/c.txt":   {Data: []byte("again?\n")},
				"foo/d/e.txt": {Data: []byte("eeeee")},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			w := tar.NewWriter(buf)
			for _, ent := range test.entries {
				if err := w.WriteHeader(&ent.header); err != nil {
					t.Fatalf("Write input tar %s: %v", ent.header.Name, err)
				}
				if len(ent.data) > 0 {
					if _, err := w.Write(ent.data); err != nil {
						t.Fatalf("Write input tar %s: %v", ent.header.Name, err)
					}
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal("Write input tar:", err)
			}

			t.Run("Default", func(t *testing.T) {
				dir := t.TempDir()
				err := extractTar(filepath.Join(dir, test.dst), bytes.NewReader(buf.Bytes()), false)
				if err != nil {
					t.Error("extractTar:", err)
				}
				if diff := diffFS(t, test.want, os.DirFS(dir)); diff != "" {
					t.Errorf("-want +got:\n%s", diff)
				}
			})

			t.Run("StripFirstComponent", func(t *testing.T) {
				dir := t.TempDir()
				err := extractTar(filepath.Join(dir, test.dst), bytes.NewReader(buf.Bytes()), true)
				if test.wantStripped == nil {
					if err == nil {
						t.Error("extractTar did not return an error")
					} else {
						t.Log("extractTar returned:", err)
					}
					return
				}
				if err != nil {
					t.Error("extractTar:", err)
				}
				if diff := diffFS(t, test.wantStripped, os.DirFS(dir)); diff != "" {
					t.Errorf("-want +got:\n%s", diff)
				}
			})
		})
	}
}

func TestExtractZip(t *testing.T) {
	type zipEntry struct {
		header zip.FileHeader
		data   []byte
	}

	tests := []struct {
		name         string
		entries      []zipEntry
		dst          string
		want         fs.FS
		wantStripped fs.FS
	}{
		{
			name:    "Empty",
			entries: []zipEntry{},
			dst:     "foo",
			want: fstest.MapFS{
				"foo": {Mode: fs.ModeDir},
			},
		},
		{
			name: "SingleFile",
			entries: []zipEntry{
				{
					header: zip.FileHeader{Name: "foo.txt"},
					data:   []byte("Hello, World!\n"),
				},
			},
			dst: "foo",
			want: fstest.MapFS{
				"foo/foo.txt": {Data: []byte("Hello, World!\n")},
			},
			wantStripped: fstest.MapFS{
				"foo": {Data: []byte("Hello, World!\n")},
			},
		},
		{
			name: "MultipleFiles",
			entries: []zipEntry{
				{
					header: zip.FileHeader{
						Name: "foo.txt",
					},
					data: []byte("Hello, World!\n"),
				},
				{
					header: zip.FileHeader{
						Name: "bar.txt",
					},
					data: []byte("again?\n"),
				},
			},
			dst: "foo",
			want: fstest.MapFS{
				"foo/foo.txt": {Data: []byte("Hello, World!\n")},
				"foo/bar.txt": {Data: []byte("again?\n")},
			},
		},
		{
			name: "Directory",
			entries: []zipEntry{
				{
					header: zip.FileHeader{Name: "a/"},
				},
				{
					header: zip.FileHeader{
						Name: "a/b.txt",
					},
					data: []byte("Hello, World!\n"),
				},
				{
					header: zip.FileHeader{
						Name: "a/c.txt",
					},
					data: []byte("again?\n"),
				},
				{
					header: zip.FileHeader{Name: "a/d/"},
				},
				{
					header: zip.FileHeader{
						Name: "a/d/e.txt",
					},
					data: []byte("eeeee"),
				},
			},
			dst: "foo",
			want: fstest.MapFS{
				"foo/a/b.txt":   {Data: []byte("Hello, World!\n")},
				"foo/a/c.txt":   {Data: []byte("again?\n")},
				"foo/a/d/e.txt": {Data: []byte("eeeee")},
			},
			wantStripped: fstest.MapFS{
				"foo/b.txt":   {Data: []byte("Hello, World!\n")},
				"foo/c.txt":   {Data: []byte("again?\n")},
				"foo/d/e.txt": {Data: []byte("eeeee")},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			w := zip.NewWriter(buf)
			for _, ent := range test.entries {
				hdr := new(zip.FileHeader)
				*hdr = ent.header
				fw, err := w.CreateHeader(hdr)
				if err != nil {
					t.Fatalf("Write input zip %s: %v", ent.header.Name, err)
				}
				if len(ent.data) > 0 {
					if _, err := fw.Write(ent.data); err != nil {
						t.Fatalf("Write input zip %s: %v", ent.header.Name, err)
					}
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal("Write input tar:", err)
			}

			t.Run("Default", func(t *testing.T) {
				dir := t.TempDir()
				err := extractZip(filepath.Join(dir, test.dst), bytes.NewReader(buf.Bytes()), int64(buf.Len()), false)
				if err != nil {
					t.Error("extractZip:", err)
				}
				if diff := diffFS(t, test.want, os.DirFS(dir)); diff != "" {
					t.Errorf("-want +got:\n%s", diff)
				}
			})

			t.Run("StripFirstComponent", func(t *testing.T) {
				dir := t.TempDir()
				err := extractZip(filepath.Join(dir, test.dst), bytes.NewReader(buf.Bytes()), int64(buf.Len()), true)
				if test.wantStripped == nil {
					if err == nil {
						t.Error("extractTar did not return an error")
					} else {
						t.Log("extractTar returned:", err)
					}
					return
				}
				if err != nil {
					t.Error("extractZip:", err)
				}
				if diff := diffFS(t, test.wantStripped, os.DirFS(dir)); diff != "" {
					t.Errorf("-want +got:\n%s", diff)
				}
			})
		})
	}
}

var mapFileType = reflect.TypeOf((*fstest.MapFile)(nil)).Elem()

func diffFS(tb testing.TB, fsys1, fsys2 fs.FS) string {
	map1 := loadFS(tb, fsys1)
	map2 := loadFS(tb, fsys2)
	return cmp.Diff(
		map1, map2,
		cmp.Comparer(func(mode1, mode2 fs.FileMode) bool {
			// Types and regular executable-ness must be equivalent.
			// If any executable bit is set, then the file is considered executable.
			return mode1.Type() == mode2.Type() &&
				(!mode1.IsRegular() || (mode1&0o111 == 0) == (mode2&0o111 == 0))
		}),
		cmp.FilterPath(
			func(p cmp.Path) bool {
				return p.Index(-2).Type() == mapFileType && p.Last().(cmp.StructField).Name() == "ModTime"
			},
			cmp.Ignore(),
		),
	)
}

// loadFS copies a filesystem into memory.
func loadFS(tb testing.TB, fsys fs.FS) fstest.MapFS {
	result := make(fstest.MapFS)
	fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			tb.Errorf("%s: %v", path, err)
		}
		f := &fstest.MapFile{
			Mode: entry.Type(),
		}
		if info, err := entry.Info(); err != nil {
			tb.Error(err)
		} else {
			f.Mode = info.Mode()
			f.ModTime = info.ModTime()
		}
		if f.Mode.IsRegular() {
			var err error
			f.Data, err = fs.ReadFile(fsys, path)
			if err != nil {
				tb.Error(err)
			}
		}
		result[path] = f
		return nil
	})
	return result
}
