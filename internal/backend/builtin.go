// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package backend

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"zombiezen.com/go/zb/zbstore"
)

const (
	builtinSystem        = "builtin"
	builtinBuilderPrefix = "builtin:"
)

// runBuiltin runs a pre-defined builder function.
// It satisfies the [runnerFunc] signature.
func runBuiltin(ctx context.Context, drv *zbstore.Derivation, dir string, logWriter io.Writer) error {
	switch drv.Builder {
	case builtinBuilderPrefix + "fetchurl":
		if err := fetchURL(ctx, drv); err != nil {
			fmt.Fprintf(logWriter, "%s: %v\n", drv.Builder, err)
			return fmt.Errorf("%s failed", drv.Builder)
		}
		return nil
	default:
		return fmt.Errorf("builtin %q not found", drv.Builder)
	}
}

func fetchURL(ctx context.Context, drv *zbstore.Derivation) error {
	href := drv.Env["url"]
	if href == "" {
		return fmt.Errorf("missing url environment variable")
	}
	outputPath := drv.Env[zbstore.DefaultDerivationOutputName]
	if outputPath == "" {
		return fmt.Errorf("missing %s environment variable", zbstore.DefaultDerivationOutputName)
	}
	if !drv.Outputs[zbstore.DefaultDerivationOutputName].IsFixed() {
		return fmt.Errorf("output is not fixed")
	}
	executable := drv.Env["executable"] != ""

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, href, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("%s returned HTTP %s", href, resp.Status)
	}
	perm := os.FileMode(0o644)
	if executable {
		perm |= 0o111
	}
	f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	_, err1 := io.Copy(f, resp.Body)
	err2 := f.Close()
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return nil
}
