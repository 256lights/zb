// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"crypto/ed25519"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRealizationSignature(t *testing.T) {
	testKey := ed25519.PrivateKey{
		0xf8, 0xd3, 0x03, 0x35, 0xfb, 0xe3, 0x0a, 0x67,
		0x53, 0xf6, 0x62, 0xeb, 0xf7, 0x36, 0x9d, 0x61,
		0x05, 0xf0, 0x17, 0xf9, 0x8f, 0x2e, 0xc4, 0xe8,
		0x33, 0x0d, 0xfa, 0xc9, 0x7e, 0xf0, 0xe8, 0x70,
		0x95, 0x09, 0x22, 0xbd, 0x27, 0x65, 0xac, 0x30,
		0x63, 0xc2, 0x01, 0x3f, 0x54, 0xd9, 0x8f, 0x79,
		0xf4, 0xd1, 0x60, 0x01, 0xf7, 0x62, 0x49, 0x61,
		0x91, 0xbd, 0x66, 0xd7, 0x62, 0x51, 0x94, 0x70,
	}
	testPublicKey := testKey.Public().(ed25519.PublicKey)

	tests := []struct {
		output      RealizationOutputReference
		realization *Realization
		want        string
		wantEd25519 []byte
	}{
		{
			output: RealizationOutputReference{
				DerivationHash: mustParseHash(t, "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="),
				OutputName:     "out",
			},
			realization: &Realization{
				OutputPath: "/opt/zb/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-foo",
			},
			want: `{"derivationHash":{"algorithm":"sha256","digest":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},"outputName":"out","outputPath":"/opt/zb/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-foo","referenceClasses":[]}`,
			wantEd25519: []byte{
				0xa3, 0x90, 0xc9, 0xfe, 0x3f, 0x60, 0xf5, 0xc9,
				0x10, 0xb6, 0xab, 0x0c, 0x6a, 0x4b, 0xb7, 0xcb,
				0xba, 0x48, 0x7c, 0x89, 0x5e, 0xa4, 0xc2, 0xa7,
				0x28, 0xcf, 0x26, 0x7f, 0xe5, 0x1b, 0xb6, 0x1d,
				0x4e, 0xdc, 0xe8, 0x64, 0x21, 0x06, 0x8d, 0x5d,
				0x5c, 0x7d, 0x88, 0x9d, 0x52, 0x8b, 0xd3, 0xe2,
				0xea, 0x3a, 0xea, 0x5e, 0xdb, 0xa2, 0x2b, 0x1d,
				0xdc, 0x77, 0x5d, 0x5b, 0xba, 0x23, 0x32, 0x06,
			},
		},
	}

	t.Run("SignRealizationWithEd25519", func(t *testing.T) {
		for _, test := range tests {
			got, err := SignRealizationWithEd25519(test.output, test.realization, testKey)
			if err != nil {
				t.Errorf("SignRealizationWithEd25519(%v, %+v, testKey): %v", test.output, test.realization, err)
				continue
			}
			want := &RealizationSignature{
				Format:    Ed25519SignatureFormat,
				PublicKey: testPublicKey,
				Signature: test.wantEd25519,
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("SignRealizationWithEd25519(%v, %+v, testKey) (-want +got):\n%s", test.output, test.realization, diff)
			}
		}
	})

	t.Run("VerifyRealizationSignature", func(t *testing.T) {
		for _, test := range tests {
			err := VerifyRealizationSignature(test.output, test.realization, &RealizationSignature{
				Format:    Ed25519SignatureFormat,
				PublicKey: testPublicKey,
				Signature: test.wantEd25519,
			})
			if err != nil {
				t.Errorf("VerifyRealizationSignature(%v, %+v, ...) with valid signature: %v",
					test.output, test.realization, err)
			}

			err = VerifyRealizationSignature(test.output, test.realization, &RealizationSignature{
				Format:    Ed25519SignatureFormat,
				PublicKey: testPublicKey,
				Signature: make([]byte, ed25519.SignatureSize),
			})
			if err == nil {
				t.Errorf("VerifyRealizationSignature(%v, %+v, ...) with zero signature succeeded",
					test.output, test.realization)
			}

			negated := slices.Clone(test.wantEd25519)
			for i := range negated {
				negated[i] = ^negated[i]
			}
			err = VerifyRealizationSignature(test.output, test.realization, &RealizationSignature{
				Format:    Ed25519SignatureFormat,
				PublicKey: testPublicKey,
				Signature: negated,
			})
			if err == nil {
				t.Errorf("VerifyRealizationSignature(%v, %+v, ...) with negated signature succeeded",
					test.output, test.realization)
			}
		}
	})

	t.Run("MarshalRealizationForSignature", func(t *testing.T) {
		for _, test := range tests {
			got, err := marshalRealizationForSignature(test.output, test.realization)
			if string(got) != test.want || err != nil {
				t.Errorf("marshalRealizationForSignature(%v, %+v) = %q, %v; want %q, <nil>",
					test.output, test.realization, got, err, test.want)
			}
		}
	})
}
