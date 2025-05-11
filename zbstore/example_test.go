// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package zbstore_test

import (
	"fmt"

	"zb.256lights.llc/pkg/zbstore"
)

func ExampleParseOutputReference() {
	ref, err := zbstore.ParseOutputReference("/opt/zb/store/ib3sh3pcz10wsmavxvkdbayhqivbghlq-hello-2.12.1.drv!out")
	if err != nil {
		panic(err)
	}
	fmt.Println(ref.DrvPath)
	fmt.Println(ref.OutputName)
	// Output:
	// /opt/zb/store/ib3sh3pcz10wsmavxvkdbayhqivbghlq-hello-2.12.1.drv
	// out
}
