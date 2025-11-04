// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
)

func TestDefaultGlobalConfig(t *testing.T) {
	got := defaultGlobalConfig()
	if got.Directory == "" {
		t.Errorf("defaultGlobalConfig().Directory is empty")
	}
	if got.StoreSocket == "" {
		t.Errorf("defaultGlobalConfig().Directory is empty")
	}
}

func TestGlobalConfigMergeFiles(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  globalConfig
	}{
		{
			name: "MergeScalar",
			files: []string{
				`{"debug": true, "storeDirectory": "/foo"}` + "\n",
				`{"storeDirectory": "/bar"}` + "\n",
			},
			want: globalConfig{
				Debug:     true,
				Directory: "/bar",
			},
		},
		{
			name: "DontMergeAllowEnvironment",
			files: []string{
				`{"allowEnvironment": ["FOO"]}` + "\n",
				`{"allowEnvironment": ["BAR"]}` + "\n",
			},
			want: globalConfig{
				AllowEnv: stringAllowList{
					set: sets.New("BAR"),
				},
			},
		},
		{
			name: "BooleanClearsSet",
			files: []string{
				`{"allowEnvironment": ["FOO"]}` + "\n",
				`{"allowEnvironment": true}` + "\n",
			},
			want: globalConfig{
				AllowEnv: stringAllowList{all: true},
			},
		},
		{
			name: "MergePublicKeys",
			files: []string{
				`{"trustedPublicKeys": [{"format": "ed25519", "publicKey": "+NMDNfvjCmdT9mLr9zadYQXwF/mPLsToMw36yX7w6HCVCSK9J2WsMGPCAT9U2Y959NFgAfdiSWGRvWbXYlGUcA=="}]}` + "\n",
				`{"trustedPublicKeys": [{"format": "foo", "publicKey": "YmFy"}]}` + "\n",
			},
			want: globalConfig{
				TrustedPublicKeys: []*zbstore.RealizationPublicKey{
					{
						Format: "ed25519",
						Data: []byte{
							0xf8, 0xd3, 0x03, 0x35, 0xfb, 0xe3, 0x0a, 0x67,
							0x53, 0xf6, 0x62, 0xeb, 0xf7, 0x36, 0x9d, 0x61,
							0x05, 0xf0, 0x17, 0xf9, 0x8f, 0x2e, 0xc4, 0xe8,
							0x33, 0x0d, 0xfa, 0xc9, 0x7e, 0xf0, 0xe8, 0x70,
							0x95, 0x09, 0x22, 0xbd, 0x27, 0x65, 0xac, 0x30,
							0x63, 0xc2, 0x01, 0x3f, 0x54, 0xd9, 0x8f, 0x79,
							0xf4, 0xd1, 0x60, 0x01, 0xf7, 0x62, 0x49, 0x61,
							0x91, 0xbd, 0x66, 0xd7, 0x62, 0x51, 0x94, 0x70,
						},
					},
					{
						Format: "foo",
						Data:   []byte{0x62, 0x61, 0x72},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			paths := make([]string, len(test.files))
			for i, content := range test.files {
				path := filepath.Join(dir, fmt.Sprintf("config%d.jwcc", i+1))
				if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
					t.Fatal(err)
				}
				paths[i] = path
			}

			got := new(globalConfig)
			err := got.mergeFiles(slices.Values(paths))
			if err != nil {
				t.Error("mergeFiles:", err)
			}
			if diff := cmp.Diff(&test.want, got, globalConfigCompareOptions); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}

func FuzzConfigMarshal(f *testing.F) {
	f.Add([]byte(`{"debug": true, "storeDirectory": "/foo"}` + "\n"))
	f.Add([]byte(`{"storeDirectory": "/bar"}` + "\n"))
	f.Add([]byte(`{"storeSocket": "/var/foo.socket"}` + "\n"))
	f.Add([]byte(`{"cacheDB": "/var/cache.db"}` + "\n"))
	f.Add([]byte(`{"trustedPublicKeys": []}` + "\n"))
	f.Add([]byte(`{"trustedPublicKeys": [{"format": "ed25519", "publicKey": "+NMDNfvjCmdT9mLr9zadYQXwF/mPLsToMw36yX7w6HCVCSK9J2WsMGPCAT9U2Y959NFgAfdiSWGRvWbXYlGUcA=="}]}` + "\n"))
	f.Add([]byte(`{"trustedPublicKeys": [{"format": "foo", "publicKey": "YmFy"}]}`))

	f.Fuzz(func(t *testing.T, in []byte) {
		init := defaultGlobalConfig()
		if err := jsonv2.Unmarshal(in, &init); err != nil {
			t.Skip(err)
		}
		marshalled, err := jsonv2.Marshal(init)
		if err != nil {
			t.Fatal(err)
		}
		got := new(globalConfig)
		if err := jsonv2.Unmarshal(marshalled, got, jsonv2.RejectUnknownMembers(true)); err != nil {
			t.Error("Unmarshal:", err)
		}
		if diff := cmp.Diff(init, got, globalConfigCompareOptions); diff != "" {
			t.Errorf("Marshal result -want +got:\n%s", diff)
		}
	})
}

var globalConfigCompareOptions = cmp.Options{
	cmp.AllowUnexported(stringAllowList{}),
	cmpopts.EquateEmpty(),
}
