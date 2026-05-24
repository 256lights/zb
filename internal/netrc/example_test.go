// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package netrc_test

import (
	"fmt"

	"zb.256lights.llc/pkg/internal/netrc"
)

func ExampleFind() {
	const netrcFile = "" +
		"machine example.com\n" +
		"login user\n" +
		"password qwerty\n"

	userinfo := netrc.Find([]byte(netrcFile), "example.com")
	fmt.Println(userinfo)
	// Output: user:qwerty
}

func ExampleFindUser() {
	const netrcFile = "" +
		"machine example.com\n" +
		"login user1\n" +
		"password qwerty\n" +
		"machine example.com\n" +
		"login user2\n" +
		"password uiop\n"

	// FindUser will search for specific users in the .netrc file.
	userinfo1 := netrc.FindUser([]byte(netrcFile), "example.com", "user1")
	fmt.Println(userinfo1)
	userinfo2 := netrc.FindUser([]byte(netrcFile), "example.com", "user2")
	fmt.Println(userinfo2)

	// FindUser will return a user-only *url.Userinfo
	// for a user that doesn't appear in the .netrc file.
	userinfo3 := netrc.FindUser([]byte(netrcFile), "example.com", "user3")
	fmt.Println(userinfo3)
	// Output:
	// user1:qwerty
	// user2:uiop
	// user3
}
