// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"zb.256lights.llc/pkg/internal/frontend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

func newDerivationCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "derivation COMMAND",
		Short:                 "query derivations",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	c.AddCommand(
		newDerivationEnvCommand(g),
		newDerivationShowCommand(g),
	)
	return c
}

type derivationShowOptions struct {
	evalOptions
	jsonFormat bool
}

func newDerivationShowCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "show [options] [PATH [...]]",
		Short:                 "show the contents of one or more derivations",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := new(derivationShowOptions)
	c.Flags().StringVar(&opts.expr, "expr", "", "interpret arguments as attribute paths relative to the Lua expression `expr`")
	c.Flags().StringVar(&opts.file, "file", "", "interpret arguments as attribute paths relative to the Lua expression stored in `path`")
	addEnvAllowListFlag(c.Flags(), &opts.allowEnv)
	c.Flags().BoolVar(&opts.jsonFormat, "json", false, "print derivation as JSON")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.installables = args
		return runDerivationShow(cmd.Context(), g, opts)
	}
	return c
}

func runDerivationShow(ctx context.Context, g *globalConfig, opts *derivationShowOptions) error {
	switch {
	case opts.expr != "" && opts.file != "":
		return fmt.Errorf("can specify at most one of --expr or --file")
	case opts.expr == "" && opts.file == "":
		return runDerivationShowFiles(ctx, g, opts)
	}

	storeClient, waitStoreClient := g.storeClient(nil)
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()
	eval, err := opts.newEval(g, storeClient)
	if err != nil {
		return err
	}
	defer func() {
		if err := eval.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	var results []any
	switch {
	case opts.expr != "":
		results, err = eval.Expression(ctx, opts.expr, opts.installables)
	case opts.file != "":
		results, err = eval.File(ctx, opts.file, opts.installables)
	default:
		panic("unreachable")
	}
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("no evaluation results")
	}

	for _, result := range results {
		drv, _ := result.(*frontend.Derivation)
		if drv == nil {
			return fmt.Errorf("%v is not a derivation", result)
		}

		if !opts.jsonFormat {
			drvBytes, err := drv.MarshalText()
			if err != nil {
				return err
			}
			if len(results) > 1 {
				drvBytes = append(drvBytes, '\n')
			}
			if _, err := os.Stdout.Write(drvBytes); err != nil {
				return err
			}
			continue
		}

		jsonData, err := marshalDerivationJSON(string(drv.Path), drv.Derivation)
		if err != nil {
			return err
		}
		jsonData = append(jsonData, '\n')
		if _, err := os.Stdout.Write(jsonData); err != nil {
			return err
		}
	}

	return nil
}

func runDerivationShowFiles(ctx context.Context, g *globalConfig, opts *derivationShowOptions) error {
	if len(opts.installables) == 0 {
		return fmt.Errorf("no files")
	}

	for _, drvPath := range opts.installables {
		drvPath, err := filepath.Abs(drvPath)
		if err != nil {
			return err
		}
		dir, err := zbstore.CleanDirectory(filepath.Dir(drvPath))
		if err != nil {
			return err
		}
		drvBytes, err := os.ReadFile(drvPath)
		if err != nil {
			return err
		}

		if !opts.jsonFormat {
			if len(opts.installables) > 1 {
				drvBytes = append(drvBytes, '\n')
			}
			if _, err := os.Stdout.Write(drvBytes); err != nil {
				return err
			}
			continue
		}

		drv, err := zbstore.ParseDerivation(dir, inferDerivationName(drvPath), drvBytes)
		if err != nil {
			return fmt.Errorf("parse %s: %v", drvPath, err)
		}

		jsonData, err := marshalDerivationJSON(drvPath, drv)
		if err != nil {
			return err
		}
		jsonData = append(jsonData, '\n')
		if _, err := os.Stdout.Write(jsonData); err != nil {
			return err
		}
	}

	return nil
}

func inferDerivationName(path string) string {
	baseName := filepath.Base(path)
	// Strip digest if the path looks like a store object.
	if path, err := zbstore.DefaultDirectory().Object(baseName); err == nil {
		baseName = path.Name()
	}
	return strings.TrimSuffix(baseName, zbstore.DerivationExt)
}

func marshalDerivationJSON(drvPath string, drv *zbstore.Derivation) ([]byte, error) {
	type jsonDerivationOutputType struct {
		Path          string `json:"path,omitempty"`
		HashType      string `json:"hashAlgo,omitempty"`
		HashRawBase16 string `json:"hash,omitempty"`
	}

	type jsonOutputReference struct {
		DrvPath    string `json:"drvPath"`
		OutputName string `json:"outputName"`
	}

	type jsonDerivation struct {
		Path    string            `json:"drvPath"`
		Name    string            `json:"name"`
		System  string            `json:"system"`
		Builder string            `json:"builder"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`

		InputSources     []string            `json:"inputSrcs"`
		InputDerivations map[string][]string `json:"inputDrvs"`

		Outputs map[string]jsonDerivationOutputType `json:"outputs"`

		Placeholders map[string]jsonOutputReference `json:"placeholders"`
	}

	j := &jsonDerivation{
		Path:    drvPath,
		Name:    drv.Name,
		System:  drv.System,
		Builder: drv.Builder,
		Args:    drv.Args,
		Env:     drv.Env,

		InputSources: collectStringSlice(drv.InputSources.Values()),
		InputDerivations: maps.Collect(func(yield func(string, []string) bool) {
			for drvPath, outputs := range drv.InputDerivations {
				if !yield(string(drvPath), collectStringSlice(outputs.Values())) {
					return
				}
			}
		}),
		Outputs: maps.Collect(func(yield func(string, jsonDerivationOutputType) bool) {
			for outputName, outputType := range drv.Outputs {
				var j jsonDerivationOutputType
				if p, err := drv.OutputPath(outputName); err == nil {
					j.Path = string(p)
				}
				if ht, ok := outputType.HashType(); ok {
					j.HashType = ht.String()
					if outputType.IsRecursiveFile() {
						j.HashType = "r:" + j.HashType
					}
				}
				if ca, ok := outputType.FixedCA(); ok {
					j.HashRawBase16 = ca.Hash().RawBase16()
				}
				if !yield(outputName, j) {
					return
				}
			}
		}),
		Placeholders: maps.Collect(func(yield func(string, jsonOutputReference) bool) {
			for outputName := range drv.Outputs {
				placeholder := zbstore.HashPlaceholder(outputName)
				jref := jsonOutputReference{
					DrvPath:    drvPath,
					OutputName: outputName,
				}
				if !yield(placeholder, jref) {
					return
				}
			}
			for inputRef := range drv.InputDerivationOutputs() {
				placeholder := zbstore.UnknownCAOutputPlaceholder(inputRef)
				jref := jsonOutputReference{
					DrvPath:    string(inputRef.DrvPath),
					OutputName: inputRef.OutputName,
				}
				if !yield(placeholder, jref) {
					return
				}
			}
		}),
	}

	data, err := json.Marshal(j)
	if err != nil {
		return nil, fmt.Errorf("marshal derivation %s: %v", drvPath, err)
	}
	return data, nil
}

type derivationEnvOptions struct {
	evalOptions
	jsonFormat bool
	tempDir    string
}

func newDerivationEnvCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "env [options] [INSTALLABLE [...]]",
		Short:                 "show the environment of one or more derivations",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := new(derivationEnvOptions)
	c.Flags().StringVar(&opts.expr, "expr", "", "interpret installables as attribute paths relative to the Lua expression `expr`")
	c.Flags().StringVar(&opts.file, "file", "", "interpret installables as attribute paths relative to the Lua expression stored in `path`")
	addEnvAllowListFlag(c.Flags(), &opts.allowEnv)
	c.Flags().BoolVar(&opts.jsonFormat, "json", false, "print environments as JSON")
	c.Flags().StringVar(&opts.tempDir, "temp-dir", os.TempDir(), "temporary `dir`ectory to fill in")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.installables = args
		return runDerivationEnv(cmd.Context(), g, opts)
	}
	return c
}

func runDerivationEnv(ctx context.Context, g *globalConfig, opts *derivationEnvOptions) error {
	storeClient, waitStoreClient := g.storeClient(nil)
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()
	eval, err := opts.newEval(g, storeClient)
	if err != nil {
		return err
	}
	defer func() {
		if err := eval.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	var results []any
	switch {
	case opts.expr != "" && opts.file != "":
		return fmt.Errorf("can specify at most one of --expr or --file")
	case opts.expr != "":
		results, err = eval.Expression(ctx, opts.expr, opts.installables)
	case opts.file != "":
		results, err = eval.File(ctx, opts.file, opts.installables)
	default:
		return fmt.Errorf("installables not supported yet")
	}
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("no evaluation results")
	}
	if len(results) > 1 {
		return fmt.Errorf("can only expand one derivation")
	}

	drv, _ := results[0].(*frontend.Derivation)
	if drv == nil {
		return fmt.Errorf("%v is not a derivation", results[0])
	}
	expandResponse := new(zbstore.RealizeResponse)
	err = jsonrpc.Do(ctx, storeClient, zbstore.ExpandMethod, expandResponse, &zbstore.ExpandRequest{
		DrvPath:            drv.Path,
		TemporaryDirectory: opts.tempDir,
	})
	if err != nil {
		return err
	}
	build, rawBuild, err := waitForBuild(ctx, storeClient, expandResponse.BuildID)
	if err != nil {
		return err
	}
	if build.Expand == nil {
		return fmt.Errorf("build %s did not provide expand information", expandResponse.BuildID)
	}
	if opts.jsonFormat {
		// Dump expand response directly to preserve unknown fields.
		var parsed struct {
			Expand json.RawMessage `json:"expand"`
		}
		if err := json.Unmarshal(rawBuild, &parsed); err != nil {
			return fmt.Errorf("%s: %v", drv.Path, err)
		}
		jsonBytes, err := dedentJSON(parsed.Expand)
		if err != nil {
			return fmt.Errorf("%s: %v", drv.Path, err)
		}
		jsonBytes = append(jsonBytes, '\n')
		if _, err := os.Stdout.Write(jsonBytes); err != nil {
			return err
		}
		return nil
	}

	for k, v := range xmaps.Sorted(build.Expand.Env) {
		if _, err := fmt.Printf("%s=%s\n", k, v); err != nil {
			return err
		}
	}

	return nil
}

func collectStringSlice[S ~string](seq iter.Seq[S]) []string {
	var slice []string
	for s := range seq {
		slice = append(slice, string(s))
	}
	return slice
}
