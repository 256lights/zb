// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

func TestSourceSHA256ContentAddress(t *testing.T) {
	machoSelfReferenceNAR, err := readFileString(filepath.Join("testdata", "macho-selfref-aarch64.nar"))
	if err != nil {
		t.Fatal(err)
	}
	machoZeroedNAR, err := readFileString(filepath.Join("testdata", "macho-zeroed-aarch64.nar"))
	if err != nil {
		t.Fatal(err)
	}
	machoUniversalSelfReferenceNAR, err := readFileString(filepath.Join("testdata", "macho-selfref-universal.nar"))
	if err != nil {
		t.Fatal(err)
	}
	machoUniversalZeroedNAR, err := readFileString(filepath.Join("testdata", "macho-zeroed-universal.nar"))
	if err != nil {
		t.Fatal(err)
	}
	machoNoRefsNAR, err := readFileString(filepath.Join("testdata", "macho-norefs-aarch64.nar"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		digest    string
		sourceNAR string

		wantCleartext string
		wantAnalysis  SelfReferenceAnalysis
	}{
		{
			name:   "NoSelfReference",
			digest: "",
			sourceNAR: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x0e\x00\x00\x00\x00\x00\x00\x00" +
				"Hello, World!\n\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00",
			wantCleartext: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x0e\x00\x00\x00\x00\x00\x00\x00" +
				"Hello, World!\n\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00|",
		},
		{
			name:   "SelfReference1",
			digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sourceNAR: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00",
			wantCleartext: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00||106",
			wantAnalysis: SelfReferenceAnalysis{
				Rewrites: []Rewriter{SelfReferenceOffset(106)},
				Paths: []nar.Header{
					{
						Mode:          0o444,
						ContentOffset: 96,
						Size:          52,
					},
				},
			},
		},
		{
			name:   "SelfReference2",
			digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			sourceNAR: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00",
			wantCleartext: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00||106",
			wantAnalysis: SelfReferenceAnalysis{
				Rewrites: []Rewriter{SelfReferenceOffset(106)},
				Paths: []nar.Header{
					{
						Mode:          0o444,
						ContentOffset: 96,
						Size:          52,
					},
				},
			},
		},
		{
			name:   "SelfReferenceLink",
			digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sourceNAR: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"symlink\x00" +
				"\x06\x00\x00\x00\x00\x00\x00\x00" +
				"target\x00\x00" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00",
			wantCleartext: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"symlink\x00" +
				"\x06\x00\x00\x00\x00\x00\x00\x00" +
				"target\x00\x00" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00||106",
			wantAnalysis: SelfReferenceAnalysis{
				Rewrites: []Rewriter{SelfReferenceOffset(106)},
				Paths: []nar.Header{
					{
						Mode:          fs.ModeSymlink | 0o777,
						ContentOffset: 96,
						Size:          52,
					},
				},
			},
		},
		{
			name: "SameContentAsSelfReference",
			sourceNAR: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00",
			wantCleartext: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
				"nix-archive-1\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				"(\x00\x00\x00\x00\x00\x00\x00" +
				"\x04\x00\x00\x00\x00\x00\x00\x00" +
				"type\x00\x00\x00\x00" +
				"\x07\x00\x00\x00\x00\x00\x00\x00" +
				"regular\x00" +
				"\x08\x00\x00\x00\x00\x00\x00\x00" +
				"contents" +
				"\x34\x00\x00\x00\x00\x00\x00\x00" +
				"/zb/store/\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00-path.txt\n\x00\x00\x00\x00" +
				"\x01\x00\x00\x00\x00\x00\x00\x00" +
				")\x00\x00\x00\x00\x00\x00\x00|",
		},
		{
			name:          "MachOSingleArchitectureSelfReference",
			digest:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sourceNAR:     machoSelfReferenceNAR,
			wantCleartext: machoZeroedNAR + "||16386",
			wantAnalysis: SelfReferenceAnalysis{
				Rewrites: []Rewriter{
					&MachOSignatureRewrite{
						ImageStart: 128,
						CodeEnd:    49552,
						HashType:   nix.SHA256,
						PageSize:   1 << 12,
						HashOffset: 49682,
					},
					SelfReferenceOffset(16386),
				},
				Paths: []nar.Header{
					{
						Mode:          0o555,
						ContentOffset: 128,
						Size:          49976,
					},
				},
			},
		},
		{
			name:          "MachOMultiArchitectureSelfReference",
			digest:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sourceNAR:     machoUniversalSelfReferenceNAR,
			wantCleartext: machoUniversalZeroedNAR + string("||8193|49154"),
			wantAnalysis: SelfReferenceAnalysis{
				Rewrites: []Rewriter{
					SelfReferenceOffset(8193),
					&MachOSignatureRewrite{
						ImageStart: 32896,
						CodeEnd:    82320,
						HashType:   nix.SHA256,
						PageSize:   1 << 12,
						HashOffset: 82450,
					},
					SelfReferenceOffset(49154),
				},
				Paths: []nar.Header{
					{
						Mode:          0o555,
						ContentOffset: 128,
						Size:          82744,
					},
				},
			},
		},
		{
			name:          "MachOSingleArchitectureNoReferences",
			digest:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sourceNAR:     machoNoRefsNAR,
			wantCleartext: machoNoRefsNAR + "|",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, gotAnalysis, err := SourceSHA256ContentAddress(strings.NewReader(test.sourceNAR), &ContentAddressOptions{
				Digest: test.digest,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !got.IsRecursiveFile() {
				t.Errorf("content address = %v; want recursive file", got)
			}

			h := nix.NewHasher(nix.SHA256)
			h.WriteString(test.wantCleartext)
			if got, want := got.Hash(), h.SumHash(); !got.Equal(want) {
				t.Errorf("content address hash = %v; want %v", got, want)
			}

			diff := cmp.Diff(
				&test.wantAnalysis, gotAnalysis,
				cmpopts.EquateEmpty(),
				transformSortedSet[string](),
			)
			if diff != "" {
				t.Errorf("analysis (-want +got):\n%s", diff)
			}
		})
	}
}

func readFileString(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sb := new(strings.Builder)
	if info, err := f.Stat(); err == nil {
		sb.Grow(int(info.Size()))
	}
	_, err = io.Copy(sb, f)
	return sb.String(), err
}
