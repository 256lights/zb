// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package osutil

import (
	"os/user"
	"slices"
	"testing"
)

func TestParseUser(t *testing.T) {
	tests := []struct {
		line string
		want *user.User
	}{
		{
			line: "zbld1:x:30001:30000:zb build user 1:/var/empty:/sbin/nologin",
			want: &user.User{
				Username: "zbld1",
				Gid:      "30000",
			},
		},
	}
	for _, test := range tests {
		gotName, gotGID := parseUser([]byte(test.line))
		if string(gotName) != test.want.Username || string(gotGID) != test.want.Gid {
			t.Errorf("parseUser(%q) = %q, %q; want %q, %q",
				test.line, gotName, gotGID, test.want.Username, test.want.Gid)
		}
	}
}

func TestParseGroup(t *testing.T) {
	tests := []struct {
		line          string
		want          *user.Group
		wantUsernames []string
	}{
		{
			line: "light:x:1000:",
			want: &user.Group{
				Gid:  "1000",
				Name: "light",
			},
		},
		{
			line: "zbld:x:30000:zbld1,zbld2,zbld3,zbld4,zbld5,zbld6,zbld7,zbld8,zbld9,zbld10,zbld11,zbld12,zbld13,zbld14,zbld15,zbld16,zbld17,zbld18,zbld19,zbld20,zbld21,zbld22,zbld23,zbld24,zbld25,zbld26,zbld27,zbld28,zbld29,zbld30,zbld31,zbld32",
			want: &user.Group{
				Gid:  "30000",
				Name: "zbld",
			},
			wantUsernames: []string{
				"zbld1",
				"zbld2",
				"zbld3",
				"zbld4",
				"zbld5",
				"zbld6",
				"zbld7",
				"zbld8",
				"zbld9",
				"zbld10",
				"zbld11",
				"zbld12",
				"zbld13",
				"zbld14",
				"zbld15",
				"zbld16",
				"zbld17",
				"zbld18",
				"zbld19",
				"zbld20",
				"zbld21",
				"zbld22",
				"zbld23",
				"zbld24",
				"zbld25",
				"zbld26",
				"zbld27",
				"zbld28",
				"zbld29",
				"zbld30",
				"zbld31",
				"zbld32",
			},
		},
	}
	for _, test := range tests {
		got, gotUsernames := parseGroup(test.line)
		if got.Gid != test.want.Gid || got.Name != test.want.Name || !slices.Equal(gotUsernames, test.wantUsernames) {
			t.Errorf("parseGroup(%q) = %+v, %q; want %+v, %q", test.line, got, gotUsernames, test.want, test.wantUsernames)
		}
	}
}
