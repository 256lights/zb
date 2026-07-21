// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package xhttp

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestSplitList(t *testing.T) {
	tests := []struct {
		value string
		want  []string
	}{
		{"", nil},
		{",", nil},
		{",   ,", nil},
		{"foo,bar", []string{"foo", "bar"}},
		{"foo, bar", []string{"foo", "bar"}},
		{"foo ,bar,", []string{"foo", "bar"}},
		{"  foo \t , bar \t ", []string{"foo", "bar"}},
		{"foo , ,bar,charlie", []string{"foo", "bar", "charlie"}},
		{`no-cache="X-Foo,\"X-Bar",max-age=5`, []string{`no-cache="X-Foo,\"X-Bar"`, "max-age=5"}},
		{`no-cache="\`, []string{`no-cache="\`}},
	}

	for _, test := range tests {
		got := slices.Collect(SplitList(test.value))
		if diff := cmp.Diff(test.want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("SplitList(%q) (-want +got):\n%s", test.value, diff)
		}
	}
}

func TestIsTokenChar(t *testing.T) {
	sb := new(strings.Builder)
	for i := range byte(0x80) {
		if IsTokenChar(rune(i)) {
			sb.WriteByte(i)
		}
	}
	got := sb.String()
	const want = "!#$%&'*+-.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ^_`abcdefghijklmnopqrstuvwxyz|~"
	if got != want {
		t.Errorf("token characters:\n got: %s\nwant: %s", got, want)
	}
}
