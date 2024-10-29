// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package sets_test

import (
	"fmt"

	"zb.256lights.llc/pkg/sets"
)

func ExampleSet_Format() {
	s := sets.New(3.14159)
	fmt.Printf("%.2f\n", s)
	// Output:
	// {3.14}
}

func ExampleSorted_Format() {
	s := sets.NewSorted(3.14159, -1.234)
	fmt.Printf("%.2f\n", s)
	// Output:
	// {-1.23 3.14}
}
