// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package hal

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/google/go-cmp/cmp"
)

var unmarshalTests = []struct {
	filename string
	want     Resource
}{
	{
		filename: "empty.json",
		want:     Resource{},
	},
	{
		filename: "first_spec_example.json",
		want: Resource{
			Links: map[string]ArrayOrObject[*Link]{
				"self": Object(&Link{
					HRef: "/orders/523",
				}),
				"warehouse": Object(&Link{
					HRef: "/warehouse/56",
				}),
				"invoice": Object(&Link{
					HRef: "/invoices/873",
				}),
			},
			Properties: map[string]jsontext.Value{
				"currency": jsontext.Value(`"USD"`),
				"status":   jsontext.Value(`"shipped"`),
				"total":    jsontext.Value(`10.20`),
			},
		},
	},
	{
		filename: "example.json",
		want: Resource{
			Links: map[string]ArrayOrObject[*Link]{
				"self": Object(&Link{
					HRef: "/orders",
				}),
				"next": Object(&Link{
					HRef: "/orders?page=2",
				}),
				"find": Object(&Link{
					HRef:      "/orders{?id}",
					Templated: true,
				}),
			},
			Embedded: map[string]ArrayOrObject[*Resource]{
				"orders": Array([]*Resource{
					{
						Links: map[string]ArrayOrObject[*Link]{
							"self": Object(&Link{
								HRef: "/orders/123",
							}),
							"basket": Object(&Link{
								HRef: "/baskets/98712",
							}),
							"customer": Object(&Link{
								HRef: "/customers/7809",
							}),
						},
						Properties: map[string]jsontext.Value{
							"total":    jsontext.Value(`30.00`),
							"currency": jsontext.Value(`"USD"`),
							"status":   jsontext.Value(`"shipped"`),
						},
					},
					{
						Links: map[string]ArrayOrObject[*Link]{
							"self": Object(&Link{
								HRef: "/orders/124",
							}),
							"basket": Object(&Link{
								HRef: "/baskets/97213",
							}),
							"customer": Object(&Link{
								HRef: "/customers/12369",
							}),
						},
						Properties: map[string]jsontext.Value{
							"total":    jsontext.Value(`20.00`),
							"currency": jsontext.Value(`"USD"`),
							"status":   jsontext.Value(`"processing"`),
						},
					},
				}),
			},
			Properties: map[string]jsontext.Value{
				"currentlyProcessing": jsontext.Value(`14`),
				"shippedToday":        jsontext.Value(`20`),
			},
		},
	},
}

func TestUnmarshal(t *testing.T) {
	for _, test := range unmarshalTests {
		t.Run(fileNameToTestName(test.filename), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", test.filename))
			if err != nil {
				t.Fatal(err)
			}

			var got Resource
			if err := jsonv2.Unmarshal(data, &got); err != nil {
				t.Error("Unmarshal:", err)
			}
			if diff := cmp.Diff(&test.want, &got); diff != "" {
				t.Errorf("-want +got:\n%s", diff)
			}
		})
	}
}

func FuzzMarshal(f *testing.F) {
	for _, test := range unmarshalTests {
		data, err := os.ReadFile(filepath.Join("testdata", test.filename))
		if err != nil {
			f.Error(err)
			continue
		}

		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		got1 := new(Resource)
		if err := jsonv2.Unmarshal(data, got1); err != nil {
			t.Skip("Unmarshal #1:", err)
		}
		data2, err := jsonv2.Marshal(got1)
		if err != nil {
			t.Fatal("Re-marshal:", err)
		}

		got2 := new(Resource)
		if err := jsonv2.Unmarshal(data2, got2); err != nil {
			t.Error("Unmarshal #2:", err)
		}
		if diff := cmp.Diff(got1, got2, cmp.Transformer("decodeRawMessage", decodeRawMessage)); diff != "" {
			t.Error(diff)
		}
	})
}

func TestLinkExpand(t *testing.T) {
	tests := []struct {
		href      string
		templated bool
		data      any
		want      *url.URL
	}{
		{
			href: "/orders/523",
			want: &url.URL{Path: "/orders/523"},
		},
		{
			href:      "/orders{?id}",
			templated: true,
			data: map[string]string{
				"id": "123",
			},
			want: &url.URL{
				Path:     "/orders",
				RawQuery: "id=123",
			},
		},
	}

	for _, test := range tests {
		l := &Link{
			HRef:      test.href,
			Templated: test.templated,
		}
		got, err := l.Expand(test.data)
		if err != nil || got.String() != test.want.String() {
			t.Errorf("(&Link{HRef: %q, Templated: %t}).Expand(%#v) = %v, %v; want %v, <nil>",
				test.href, test.templated, test.data, got, err, test.want)
		}
	}
}

func decodeRawMessage(msg jsontext.Value) any {
	var x any
	if err := jsonv2.Unmarshal(msg, &x); err != nil {
		panic(err)
	}
	return x
}

func fileNameToTestName(fileName string) string {
	words := strings.Split(strings.TrimSuffix(fileName, ".json"), "_")
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, "")
}
