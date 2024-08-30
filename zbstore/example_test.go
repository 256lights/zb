// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package zbstore_test

import (
	"fmt"

	"zombiezen.com/go/zb/zbstore"
)

func ExampleParseOutputReference() {
	ref, err := zbstore.ParseOutputReference("/zb/store/ib3sh3pcz10wsmavxvkdbayhqivbghlq-hello-2.12.1.drv!out")
	if err != nil {
		panic(err)
	}
	fmt.Println(ref.DrvPath)
	fmt.Println(ref.OutputName)
	// Output:
	// /zb/store/ib3sh3pcz10wsmavxvkdbayhqivbghlq-hello-2.12.1.drv
	// out
}
