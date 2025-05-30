// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/nix/nar"
)

func newNARCommand() *cobra.Command {
	c := &cobra.Command{
		Use:                   "nar COMMAND",
		Short:                 "operate on NAR files",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	c.AddCommand(
		newPackNARCommand(),
	)
	return c
}

func newPackNARCommand() *cobra.Command {
	c := &cobra.Command{
		Use:                   "pack [options] PATH",
		Short:                 "serialize a filesystem object to NAR format",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ExactArgs(1),
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	outputPath := c.Flags().StringP("output", "o", "", "`file` to write to (default is stdout)")
	selfRefs := c.Flags().Bool("self-references", false, "rewrite any self-references and print path to stdout (must use with --output)")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if *selfRefs && *outputPath == "" {
			return errors.New("--self-references given without --output")
		}
		outputFile := os.Stdout
		if *outputPath != "" {
			var err error
			outputFile, err = os.Create(*outputPath)
			if err != nil {
				return err
			}
		}
		var err1 error
		if *selfRefs {
			err1 = runPackNARSelfRefs(outputFile, args[0])
		} else {
			err1 = nar.DumpPath(outputFile, args[0])
		}
		var err2 error
		if *outputPath != "" {
			err2 = outputFile.Close()
		}
		return cmp.Or(err1, err2)
	}
	return c
}

func runPackNARSelfRefs(dst io.ReadWriteSeeker, path string) error {
	type caResult struct {
		ca       zbstore.ContentAddress
		analysis *zbstore.SelfReferenceAnalysis
		err      error
	}

	// Interpret path as a store path, using the parent directory as the store directory.
	// This ensures that the path includes a valid digest.
	var err error
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	storePath, err := zbstore.ParsePath(path)
	if err != nil {
		return err
	}

	// Serialize as NAR, noting any occurrence of the digest as it is written.
	originalDigest := storePath.Digest()
	pr, pw := io.Pipe()
	c := make(chan caResult)
	go func() {
		var result caResult
		result.ca, result.analysis, result.err = zbstore.SourceSHA256ContentAddress(pr, &zbstore.ContentAddressOptions{
			Digest: originalDigest,
		})
		c <- result
	}()
	err = nar.DumpPath(io.MultiWriter(pw, dst), path)
	pw.CloseWithError(err)
	result := <-c // Always wait on goroutine.
	if err != nil {
		return err
	}
	if result.err != nil {
		return err
	}

	// Compute the final store path.
	finalStorePath, err := zbstore.FixedCAOutputPath(storePath.Dir(), storePath.Name(), result.ca, zbstore.References{
		Self: result.analysis.HasSelfReferences(),
	})
	if err != nil {
		return err
	}

	// If the digest differs, rewrite each occurrence.
	if finalDigest := finalStorePath.Digest(); finalDigest != originalDigest {
		if err := zbstore.Rewrite(dst, 0, finalDigest, result.analysis.Rewrites); err != nil {
			return err
		}
	}

	// Print the full store path to stdout.
	if _, err := fmt.Println(finalStorePath); err != nil {
		return err
	}

	return nil
}
