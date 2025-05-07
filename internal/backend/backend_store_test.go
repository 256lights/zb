// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"zb.256lights.llc/pkg/zbstore"
)

// FuzzBuilderLogPath ensures that filepath.Dir(builderLogPath(dir, buildID, drvPath)) == buildLogRoot(dir, buildID)
// for all values.
func FuzzBuilderLogPath(f *testing.F) {
	f.Add("/opt/zb/var/log/zb", "/opt/zb/store/q4dz47g15qmlsm01aijr737w8avkaac6-hello.drv")

	f.Fuzz(func(t *testing.T, dir string, rawDrvPath string) {
		drvPath, err := zbstore.ParsePath(rawDrvPath)
		if err != nil {
			t.Skip(err)
		}
		buildID, err := uuid.NewRandom()
		if err != nil {
			t.Fatal(err)
		}
		gotDir := buildLogRoot(dir, buildID)
		gotFile := builderLogPath(dir, buildID, drvPath)
		if filepath.Dir(gotFile) != gotDir {
			t.Errorf("builderLogPath(%q, %v, %q) = %q; want to be in %q",
				dir, buildID, rawDrvPath, gotFile, gotDir)
		}
	})
}
