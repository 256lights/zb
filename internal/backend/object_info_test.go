// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package backend_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	. "zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix"
)

type objectInfoMarshalTest struct {
	name string
	text string
	info *ObjectInfo
}

func objectInfoMarshalTests(tb testing.TB) []*objectInfoMarshalTest {
	return []*objectInfoMarshalTest{
		{
			name: "NoReferences",
			text: "StorePath: /zb/store/z5yrbqk8sjlzyvw8wpicsn2ybk0sc470-busybox-1.36.1\n" +
				"NarHash: sha256:1d99d4f5hjl24w30hwgrmn00kryvd1yxvyydpkm76hgmcig9mllc\n" +
				"NarSize: 1228440\n" +
				"CA: fixed:r:sha256:143sdn30fdykpz8gpyw45m9m6m4gz858w9kc6myy7p0v74v5qq4m\n",
			info: &ObjectInfo{
				StorePath: "/zb/store/z5yrbqk8sjlzyvw8wpicsn2ybk0sc470-busybox-1.36.1",
				NARHash:   mustParseHash(tb, "sha256:1d99d4f5hjl24w30hwgrmn00kryvd1yxvyydpkm76hgmcig9mllc"),
				NARSize:   1228440,
				CA:        nix.RecursiveFileContentAddress(mustParseHash(tb, "sha256:143sdn30fdykpz8gpyw45m9m6m4gz858w9kc6myy7p0v74v5qq4m")),
			},
		},
		{
			name: "OneReference",
			text: "StorePath: /zb/store/9n2ccy3mcsb04q47npp28jwkd9py3wdj-gcc-4.2.1\n" +
				"NarHash: sha256:0a9pvsidbxbdcrj9aj3gz7sp0ibfzlhmp6jwljjqya4xjwc0lnzr\n" +
				"NarSize: 95499656\n" +
				"References: 9n2ccy3mcsb04q47npp28jwkd9py3wdj-gcc-4.2.1\n" +
				"CA: fixed:r:sha256:0pikk3c4s55bhh1nzbj2150ksbnsxnvl4xarap58kjp9qv5d4z1k\n",
			info: &ObjectInfo{
				StorePath: "/zb/store/9n2ccy3mcsb04q47npp28jwkd9py3wdj-gcc-4.2.1",
				NARHash:   mustParseHash(tb, "sha256:0a9pvsidbxbdcrj9aj3gz7sp0ibfzlhmp6jwljjqya4xjwc0lnzr"),
				NARSize:   95499656,
				References: *sets.NewSorted[zbstore.Path](
					"/zb/store/9n2ccy3mcsb04q47npp28jwkd9py3wdj-gcc-4.2.1",
				),
				CA: nix.RecursiveFileContentAddress(mustParseHash(tb, "sha256:0pikk3c4s55bhh1nzbj2150ksbnsxnvl4xarap58kjp9qv5d4z1k")),
			},
		},
		{
			name: "TwoReferences",
			text: "StorePath: /zb/store/h9z0bp4cnw57ngi33l4781n4rrfb2a8q-gcc-4.2.1\n" +
				"NarHash: sha256:1ksdfhnnlywdh1d0vlxrc6kdyv0vqqagkrhx2l145s5mhk2cm70z\n" +
				"NarSize: 16832\n" +
				"References: 9n2ccy3mcsb04q47npp28jwkd9py3wdj-gcc-4.2.1 z5yrbqk8sjlzyvw8wpicsn2ybk0sc470-busybox-1.36.1\n" +
				"CA: fixed:r:sha256:073lrg7m3rrqbn9wgy7wrf94h77hhhjmnvwhh8vqpnbflsgzb8dk\n",
			info: &ObjectInfo{
				StorePath: "/zb/store/h9z0bp4cnw57ngi33l4781n4rrfb2a8q-gcc-4.2.1",
				NARHash:   mustParseHash(tb, "sha256:1ksdfhnnlywdh1d0vlxrc6kdyv0vqqagkrhx2l145s5mhk2cm70z"),
				NARSize:   16832,
				References: *sets.NewSorted[zbstore.Path](
					"/zb/store/9n2ccy3mcsb04q47npp28jwkd9py3wdj-gcc-4.2.1",
					"/zb/store/z5yrbqk8sjlzyvw8wpicsn2ybk0sc470-busybox-1.36.1",
				),
				CA: nix.RecursiveFileContentAddress(mustParseHash(tb, "sha256:073lrg7m3rrqbn9wgy7wrf94h77hhhjmnvwhh8vqpnbflsgzb8dk")),
			},
		},
		{
			name: "LineNoise",
			text: "\n: r",
		},
	}
}

func TestObjectInfoMarshal(t *testing.T) {
	for _, test := range objectInfoMarshalTests(t) {
		if test.info == nil {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			got, err := test.info.MarshalText()
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(test.text, string(got)); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}

func TestObjectInfoUnmarshal(t *testing.T) {
	for _, test := range objectInfoMarshalTests(t) {
		t.Run(test.name, func(t *testing.T) {
			got := new(ObjectInfo)
			if err := got.UnmarshalText([]byte(test.text)); err != nil {
				t.Log("UnmarshalText:", err)
				if test.info != nil {
					t.Fail()
				}
				return
			}
			if test.info == nil {
				t.Fatal("Unmarshal succeeded")
			}
			diff := cmp.Diff(
				test.info, got,
				transformSortedSet[zbstore.Path](),
			)
			if diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}

func FuzzObjectInfoMarshal(f *testing.F) {
	for _, test := range objectInfoMarshalTests(f) {
		f.Add(test.text)
	}

	f.Fuzz(func(t *testing.T, text string) {
		got := new(ObjectInfo)
		if err := got.UnmarshalText([]byte(text)); err != nil {
			t.Skip(err)
		}
		text2, err := got.MarshalText()
		if err != nil {
			t.Fatal("re-marshal:", err)
		}
		got2 := new(ObjectInfo)
		if err := got2.UnmarshalText([]byte(text2)); err != nil {
			t.Fatal("round-trip:", err)
		}
		diff := cmp.Diff(
			got, got2,
			transformSortedSet[zbstore.Path](),
		)
		if diff != "" {
			t.Errorf("round-trip (-first +second):\n%s", diff)
		}
	})
}
