// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore

import (
	"testing"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type realizationOutputReferenceJSONTest struct {
	json                       map[string]any
	realizationOutputReference RealizationOutputReference
	err                        bool
}

func realizationOutputReferenceJSONTests(tb testing.TB) []realizationOutputReferenceJSONTest {
	return []realizationOutputReferenceJSONTest{
		{
			realizationOutputReference: RealizationOutputReference{},
			json:                       nil,
		},
		{
			realizationOutputReference: RealizationOutputReference{
				DerivationHash: mustParseHash(tb, "sha256-E7vF8sqxzZakF3PIARooaSxc9nHFK03NE+tZ7JTtIvQ="),
				OutputName:     "out",
			},
			json: map[string]any{
				"derivationHash": map[string]any{
					"algorithm": "sha256",
					"digest":    "E7vF8sqxzZakF3PIARooaSxc9nHFK03NE+tZ7JTtIvQ=",
				},
				"outputName": "out",
			},
		},
		{
			json: map[string]any{},
			err:  true,
		},
		{
			json: map[string]any{
				"outputName": "out",
			},
			err: true,
		},
		{
			json: map[string]any{
				"derivationHash": map[string]any{
					"algorithm": "sha256",
					"digest":    "E7vF8sqxzZakF3PIARooaSxc9nHFK03NE+tZ7JTtIvQ=",
				},
			},
			err: true,
		},
	}
}

func (test realizationOutputReferenceJSONTest) inputJSON(tb testing.TB) []byte {
	tb.Helper()
	if test.json == nil {
		return []byte("null")
	}
	gotJSON, err := jsonv2.Marshal(test.json)
	if err != nil {
		tb.Fatal(err)
	}
	return gotJSON
}

func TestRealizationOutputReferenceMarshalJSON(t *testing.T) {
	for _, test := range realizationOutputReferenceJSONTests(t) {
		if test.err {
			continue
		}
		gotJSON, err := jsonv2.Marshal(test.realizationOutputReference)
		if err != nil {
			t.Errorf("%v: %v", test.realizationOutputReference, err)
			continue
		}
		var got map[string]any
		if err := jsonv2.Unmarshal(gotJSON, &got); err != nil {
			t.Errorf("%v: %v", test.realizationOutputReference, err)
			continue
		}
		if diff := cmp.Diff(test.json, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("marshal %v (-want +got):\n%s", test.realizationOutputReference, diff)
		}
	}
}

func TestRealizationOutputReferenceUnmarshalJSON(t *testing.T) {
	for _, test := range realizationOutputReferenceJSONTests(t) {
		inputJSON := test.inputJSON(t)
		var got RealizationOutputReference
		err := jsonv2.Unmarshal(inputJSON, &got, jsonv2.RejectUnknownMembers(true))
		if err != nil && !test.err {
			t.Errorf("%v: %v", test.realizationOutputReference, err)
			continue
		} else if test.err {
			if err == nil {
				t.Errorf("%s did not produce an error", inputJSON)
			} else {
				t.Logf("%s error (as expected): %v", inputJSON, err)
			}
			continue
		}
		if diff := cmp.Diff(test.realizationOutputReference, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("unmarshal %v (-want +got):\n%s", test.realizationOutputReference, diff)
		}
	}
}

func FuzzRealizationOutputReferenceJSON(f *testing.F) {
	for _, test := range realizationOutputReferenceJSONTests(f) {
		f.Add(test.inputJSON(f))
	}

	unmarshalOptions := jsonv2.JoinOptions(
		jsonv2.RejectUnknownMembers(true),
	)
	f.Fuzz(func(t *testing.T, inputJSON []byte) {
		var got1 RealizationOutputReference
		if err := jsonv2.Unmarshal(inputJSON, &got1, unmarshalOptions); err != nil {
			t.Skip(err)
		}
		gotJSON, err := jsonv2.Marshal(got1)
		if err != nil {
			t.Fatal(err)
		}
		var got2 RealizationOutputReference
		err = jsonv2.Unmarshal(gotJSON, &got2, unmarshalOptions)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(got1, got2, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("after remarshaling (-want +got):\n%s", diff)
		}
	})
}
