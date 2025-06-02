// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"zb.256lights.llc/pkg/bytebuffer"
	"zombiezen.com/go/nix"
)

func TestRewrite(t *testing.T) {
	machoSelfReferenceNAR, err := readFileString(filepath.Join("testdata", "macho-selfref-aarch64.nar"))
	if err != nil {
		t.Fatal(err)
	}
	machoZeroedNAR, err := readFileString(filepath.Join("testdata", "macho-zeroed-aarch64.nar"))
	if err != nil {
		t.Fatal(err)
	}
	machoRewrittenNAR, err := readFileString(filepath.Join("testdata", "macho-rewritten-aarch64.nar"))
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
	machoUniversalRewrittenNAR, err := readFileString(filepath.Join("testdata", "macho-rewritten-universal.nar"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		sourceNAR string
		newDigest string
		rewrites  []Rewriter
		want      string
	}{
		{
			name: "SelfReference",
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
			newDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			rewrites: []Rewriter{
				SelfReferenceOffset(106),
			},
			want: "\x0d\x00\x00\x00\x00\x00\x00\x00" +
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
		},
		{
			name:      "MachOSingleArchitectureSelfReferenceSignatureOnly",
			newDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sourceNAR: machoZeroedNAR[:1352] + machoSelfReferenceNAR[1352:1368] + machoZeroedNAR[1368:],
			rewrites: []Rewriter{
				SelfReferenceOffset(16386),
				&MachOSignatureRewrite{
					ImageStart: 128,
					CodeEnd:    49552,
					HashType:   nix.SHA256,
					PageSize:   1 << 12,
					HashOffset: 49682,
				},
			},
			want: machoSelfReferenceNAR,
		},
		{
			name:      "MachOSingleArchitectureSelfReference",
			newDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sourceNAR: machoZeroedNAR,
			rewrites: []Rewriter{
				SelfReferenceOffset(16386),
				&MachOUUIDRewrite{
					ImageStart: 128,
					UUIDStart:  1352,
					CodeEnd:    49552,
				},
				&MachOSignatureRewrite{
					ImageStart: 128,
					CodeEnd:    49552,
					HashType:   nix.SHA256,
					PageSize:   1 << 12,
					HashOffset: 49682,
				},
			},
			want: machoRewrittenNAR,
		},
		{
			name:      "MachOMultiArchitectureSelfReferenceSignatureOnly",
			newDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sourceNAR: machoUniversalZeroedNAR[:34120] + machoUniversalSelfReferenceNAR[34120:34136] + machoUniversalZeroedNAR[34136:],
			rewrites: []Rewriter{
				SelfReferenceOffset(8193),
				SelfReferenceOffset(49154),
				&MachOSignatureRewrite{
					ImageStart: 32896,
					CodeEnd:    82320,
					HashType:   nix.SHA256,
					PageSize:   1 << 12,
					HashOffset: 82450,
				},
			},
			want: machoUniversalSelfReferenceNAR,
		},
		{
			name:      "MachOMultiArchitectureSelfReference",
			newDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			sourceNAR: machoUniversalZeroedNAR,
			rewrites: []Rewriter{
				SelfReferenceOffset(8193),
				SelfReferenceOffset(49154),
				&MachOUUIDRewrite{
					ImageStart: 32896,
					UUIDStart:  34120,
					CodeEnd:    82320,
				},
				&MachOSignatureRewrite{
					ImageStart: 32896,
					CodeEnd:    82320,
					HashType:   nix.SHA256,
					PageSize:   1 << 12,
					HashOffset: 82450,
				},
			},
			want: machoUniversalRewrittenNAR,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := bytebuffer.New([]byte(test.sourceNAR))
			if err := Rewrite(f, 0, test.newDigest, test.rewrites); err != nil {
				t.Error("Rewrite:", err)
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(f)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff([]byte(test.want), got); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}
