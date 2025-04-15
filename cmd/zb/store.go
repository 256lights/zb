// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
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
		newStoreObjectExportCommand(g),
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

		req := &zbstorerpc.InfoRequest{
			Path: path,
		}
		if opts.jsonFormat {
			// Dump info response directly to preserve unknown fields.
			var partialParsed struct {
				Info json.RawMessage `json:"info"`
			}
			err = jsonrpc.Do(ctx, storeClient, zbstorerpc.InfoMethod, &partialParsed, req)
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

		resp := new(zbstorerpc.InfoResponse)
		err = jsonrpc.Do(ctx, storeClient, zbstorerpc.InfoMethod, resp, req)
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

type storeObjectExportOptions struct {
	paths             []string
	includeReferences bool
	output            io.WriteCloser
}

func newStoreObjectExportCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "export [options] PATH [...]",
		Short:                 "export one or more store objects",
		DisableFlagsInUseLine: true,
		Args:                  cobra.MinimumNArgs(1),
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := new(storeObjectExportOptions)
	c.Flags().BoolVar(&opts.includeReferences, "references", true, "include referenced store objects")
	outputPath := c.Flags().StringP("output", "o", "", "output `file`")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		switch {
		case *outputPath == "" && term.IsTerminal(int(os.Stdout.Fd())):
			return errors.New("refusing to send binary export to stdout (a tty). Pass --output=- to override.")
		case *outputPath == "" || *outputPath == "*":
			opts.output = nopWriteCloser{os.Stdout}
		default:
			var err error
			opts.output, err = os.Create(*outputPath)
			if err != nil {
				return err
			}
		}

		opts.paths = args
		return runStoreObjectExport(cmd.Context(), g, opts)
	}
	return c
}

func runStoreObjectExport(ctx context.Context, g *globalConfig, opts *storeObjectExportOptions) error {
	closeFunc := sync.OnceValue(opts.output.Close)
	defer closeFunc()

	toOutput := zbstorerpc.ImportFunc(func(header jsonrpc.Header, body io.Reader) error {
		return zbstore.ReceiveExport(nopReceiver{}, io.TeeReader(body, opts.output))
	})
	storeClient, waitStoreClient := g.storeClient(&zbstorerpc.CodecOptions{
		Importer: toOutput,
	})
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()

	req := &zbstorerpc.ExportRequest{
		Paths:             make([]zbstore.Path, len(opts.paths)),
		ExcludeReferences: !opts.includeReferences,
	}
	for i, p := range opts.paths {
		var err error
		req.Paths[i], err = zbstore.ParsePath(p)
		if err != nil {
			return err
		}
	}
	if err := jsonrpc.Do(ctx, storeClient, zbstorerpc.ExportMethod, nil, req); err != nil {
		return err
	}

	// The export message is sent before the RPC response, so if we received the response,
	// the export is complete.
	if err := closeFunc(); err != nil {
		return err
	}
	return nil
}

type nopReceiver struct{}

func (nopReceiver) Write(p []byte) (n int, err error)         { return len(p), nil }
func (nopReceiver) ReceiveNAR(trailer *zbstore.ExportTrailer) {}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
