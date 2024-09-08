// Copyright 2024 Roxy Light
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
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/zb"
	"zombiezen.com/go/zb/zbstore"
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

	storeClient, waitStoreClient := g.storeClient(new(clientRPCHandler), nil)
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()
	eval, err := zb.NewEval(g.storeDir, storeClient, g.cacheDB)
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
		drv, _ := result.(*zbstore.Derivation)
		if drv == nil {
			return fmt.Errorf("%v is not a derivation", result)
		}
		// TODO(someday): Evaluation should store the path of the exported result.
		drvInfo, _, drvBytes, err := drv.Export(nix.SHA256)
		if err != nil {
			return err
		}

		if !opts.jsonFormat {
			if len(results) > 1 {
				drvBytes = append(drvBytes, '\n')
			}
			if _, err := os.Stdout.Write(drvBytes); err != nil {
				return err
			}
			continue
		}

		jsonData, err := marshalDerivationJSON(string(drvInfo.StorePath), drv)
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

func collectStringSlice[S ~string](seq iter.Seq[S]) []string {
	var slice []string
	for s := range seq {
		slice = append(slice, string(s))
	}
	return slice
}
