// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/zbstore"
)

func newStoreCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "store COMMAND",
		Short:                 "inspect the store",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	c.AddCommand(
		newStoreObjectCommand(g),
	)
	return c
}

func newStoreObjectCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "object COMMAND",
		Short:                 "inspect store objects",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	c.AddCommand(
		newStoreObjectInfoCommand(g),
	)
	return c
}

type storeObjectInfoOptions struct {
	paths      []string
	jsonFormat bool
}

func newStoreObjectInfoCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "info [options] [PATH [...]]",
		Short:                 "show metadata of one or more store objects",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := new(storeObjectInfoOptions)
	c.Flags().BoolVar(&opts.jsonFormat, "json", false, "print object info as JSON")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.paths = args
		return runStoreObjectInfo(cmd.Context(), g, opts)
	}
	return c
}

func runStoreObjectInfo(ctx context.Context, g *globalConfig, opts *storeObjectInfoOptions) error {
	storeClient, waitStoreClient := g.storeClient(nil)
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()

	const errNotExist = "does not exist"

	// TODO(someday): Batch.
	buf := new(bytes.Buffer)
	for i, p := range opts.paths {
		path, err := zbstore.ParsePath(p)
		if err != nil {
			return err
		}

		req := &zbstore.InfoRequest{
			Path: path,
		}
		if opts.jsonFormat {
			// Dump info response directly to preserve unknown fields.
			var partialParsed struct {
				Info json.RawMessage `json:"info"`
			}
			err = jsonrpc.Do(ctx, storeClient, zbstore.InfoMethod, &partialParsed, req)
			if err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
			if string(partialParsed.Info) == "null" {
				return fmt.Errorf("%s: %v", path, errNotExist)
			}
			jsonBytes, err := dedentJSON(partialParsed.Info)
			if err != nil {
				return fmt.Errorf("%s: %v", path, err)
			}
			jsonBytes = append(jsonBytes, '\n')
			if _, err := os.Stdout.Write(jsonBytes); err != nil {
				return err
			}
			continue
		}

		resp := new(zbstore.InfoResponse)
		err = jsonrpc.Do(ctx, storeClient, zbstore.InfoMethod, resp, req)
		if err != nil {
			return fmt.Errorf("%s: %v", path, err)
		}
		if resp.Info == nil {
			return fmt.Errorf("%s: %v", path, errNotExist)
		}

		buf.Reset()
		if i > 0 {
			// Blank line between entries.
			buf.WriteByte('\n')
		}
		fmt.Fprintf(buf, "StorePath: %s\n", path)
		fmt.Fprintf(buf, "NarHash: %v\n", resp.Info.NARHash.Base32())
		fmt.Fprintf(buf, "NarSize: %d\n", resp.Info.NARSize)
		if len(resp.Info.References) > 0 {
			buf.WriteString("References:")
			for _, ref := range resp.Info.References {
				buf.WriteByte(' ')
				buf.WriteString(ref.Base())
			}
			buf.WriteByte('\n')
		}
		if !resp.Info.CA.IsZero() {
			fmt.Fprintf(buf, "CA: %v\n", resp.Info.CA)
		}
		if _, err := os.Stdout.Write(buf.Bytes()); err != nil {
			return err
		}
	}

	return nil
}
