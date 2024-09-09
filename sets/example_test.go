// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package sets_test

import (
	"fmt"

	"zombiezen.com/go/zb/sets"
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
