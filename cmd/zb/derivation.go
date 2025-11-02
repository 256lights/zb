// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"github.com/spf13/cobra"
	"zb.256lights.llc/pkg/internal/frontend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/xmaps"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
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
		Args: func(c *cobra.Command, args []string) error {
			if expr, _ := c.Flags().GetBool("expression"); expr {
				return cobra.ExactArgs(1)(c, args)
			}
			return cobra.MinimumNArgs(1)(c, args)
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	opts := new(derivationShowOptions)
	c.Flags().BoolVarP(&opts.expression, "expression", "e", false, "interpret argument as Lua expression")
	addEnvAllowListFlag(c.Flags(), &g.AllowEnv)
	c.Flags().BoolVar(&opts.jsonFormat, "json", false, "print derivation as JSON")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.args = args
		return runDerivationShow(cmd.Context(), g, opts)
	}
	return c
}

func runDerivationShow(ctx context.Context, g *globalConfig, opts *derivationShowOptions) error {
	var drvPaths []string
	if !opts.expression {
		// The handling of arguments for `derivation show` is slightly different than other commands.
		// If the user passes .drv file paths as arguments,
		// we'll show the .drv file directly rather than trying to interpret it as a Lua file.
		// These can be interspersed with other URLs.
		drvPaths = make([]string, len(opts.args))
		for i, arg := range opts.args {
			u, err := frontend.ParseURL(arg)
			if err != nil {
				return err
			}
			if (u.Scheme == "" || u.Scheme == "file") && u.Fragment == "" &&
				strings.HasSuffix(u.Path, zbstore.DerivationExt) {
				drvPaths[i], err = frontend.URLToPath(u)
				if err != nil {
					return err
				}
			}
		}
		if !slices.Contains(drvPaths, "") {
			// Fast path: don't connect to the store. All arguments are local paths to .drv files.
			for _, drvPath := range drvPaths {
				drvBytes, err := showDerivationFile(drvPath, opts.jsonFormat)
				if err != nil {
					return err
				}
				if !opts.jsonFormat && len(opts.args) > 1 {
					drvBytes = append(drvBytes, '\n')
				}
				if _, err := os.Stdout.Write(drvBytes); err != nil {
					return err
				}
			}
			return nil
		}
	}

	di := new(zbstorerpc.DeferredImporter)
	storeClient, waitStoreClient := g.storeClient(&zbstorerpc.CodecOptions{
		Importer: di,
	})
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()
	eval, err := opts.newEval(g, storeClient, di)
	if err != nil {
		return err
	}
	defer func() {
		if err := eval.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	var results []any
	if opts.expression {
		results = make([]any, 1)
		results[0], err = eval.Expression(ctx, opts.args[0])
	} else {
		urls := make([]string, 0, len(opts.args))
		for i, arg := range opts.args {
			if drvPaths[i] != "" {
				continue
			}
			urls = append(urls, arg)
		}
		results, err = eval.URLs(ctx, urls)
	}
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("no evaluation results")
	}

	resultIndex := 0
	for i := range opts.args {
		var drvBytes []byte
		var err error
		if i < len(drvPaths) && drvPaths[i] != "" {
			drvBytes, err = showDerivationFile(drvPaths[i], opts.jsonFormat)
		} else {
			result := results[resultIndex]
			resultIndex++
			drv, _ := result.(*frontend.Derivation)
			if drv == nil {
				return fmt.Errorf("%v is not a derivation", result)
			}
			drvBytes, err = showDerivation(drv, opts.jsonFormat)
		}
		if err != nil {
			return err
		}
		if !opts.jsonFormat && len(results) > 1 {
			drvBytes = append(drvBytes, '\n')
		}
		if _, err := os.Stdout.Write(drvBytes); err != nil {
			return err
		}
	}

	return nil
}

func showDerivationFile(drvPath string, jsonFormat bool) ([]byte, error) {
	drvPath, err := filepath.Abs(drvPath)
	if err != nil {
		return nil, err
	}
	dir, err := zbstore.CleanDirectory(filepath.Dir(drvPath))
	if err != nil {
		return nil, err
	}
	drvBytes, err := os.ReadFile(drvPath)
	if err != nil {
		return nil, err
	}
	if !jsonFormat {
		// If we're not outputting JSON, no need to parse. Pass through, even if it's invalid.
		return drvBytes, nil
	}
	drv, err := zbstore.ParseDerivation(dir, inferDerivationName(drvPath), drvBytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %v", drvPath, err)
	}

	jsonData, err := marshalDerivationJSON(drvPath, drv)
	if err != nil {
		return nil, err
	}
	jsonData = append(jsonData, '\n')
	return jsonData, nil
}

func showDerivation(drv *frontend.Derivation, jsonFormat bool) ([]byte, error) {
	if !jsonFormat {
		return drv.MarshalText()
	}
	jsonData, err := marshalDerivationJSON(string(drv.Path), drv.Derivation)
	if err != nil {
		return nil, err
	}
	jsonData = append(jsonData, '\n')
	return jsonData, nil
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

	data, err := jsonv2.Marshal(j, jsonv2.Deterministic(true))
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
		Args: func(c *cobra.Command, args []string) error {
			if expr, _ := c.Flags().GetBool("expr"); expr {
				return cobra.ExactArgs(1)(c, args)
			}
			return cobra.MinimumNArgs(1)(c, args)
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	opts := new(derivationEnvOptions)
	c.Flags().BoolVarP(&opts.expression, "expression", "e", false, "interpret argument as Lua expression")
	addEnvAllowListFlag(c.Flags(), &g.AllowEnv)
	c.Flags().BoolVar(&opts.jsonFormat, "json", false, "print environments as JSON")
	c.Flags().StringVar(&opts.tempDir, "temp-dir", os.TempDir(), "temporary `dir`ectory to fill in")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.args = args
		return runDerivationEnv(cmd.Context(), g, opts)
	}
	return c
}

func runDerivationEnv(ctx context.Context, g *globalConfig, opts *derivationEnvOptions) error {
	di := new(zbstorerpc.DeferredImporter)
	storeClient, waitStoreClient := g.storeClient(&zbstorerpc.CodecOptions{
		Importer: di,
	})
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()
	eval, err := opts.newEval(g, storeClient, di)
	if err != nil {
		return err
	}
	defer func() {
		if err := eval.Close(); err != nil {
			log.Errorf(ctx, "%v", err)
		}
	}()

	var results []any
	if opts.expression {
		results = make([]any, 1)
		results[0], err = eval.Expression(ctx, opts.args[0])
	} else {
		results, err = eval.URLs(ctx, opts.args)
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
	expandResponse := new(zbstorerpc.RealizeResponse)
	err = jsonrpc.Do(ctx, storeClient, zbstorerpc.ExpandMethod, expandResponse, &zbstorerpc.ExpandRequest{
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
			Expand jsontext.Value `json:"expand"`
		}
		if err := jsonv2.Unmarshal(rawBuild, &parsed); err != nil {
			return fmt.Errorf("%s: %v", drv.Path, err)
		}
		if err := parsed.Expand.Compact(); err != nil {
			return fmt.Errorf("%s: %v", drv.Path, err)
		}
		jsonBytes := append(slices.Clip([]byte(parsed.Expand)), '\n')
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
