// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"zb.256lights.llc/pkg/bytebuffer"
	"zb.256lights.llc/pkg/internal/backend"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/internal/xio"
	"zb.256lights.llc/pkg/internal/zbstorerpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
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
		Short:                 "inspect and transfer store objects",
		DisableFlagsInUseLine: true,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	c.AddCommand(
		newStoreObjectInfoCommand(g),
		newStoreObjectImportCommand(g),
		newStoreObjectExportCommand(g),
		newStoreObjectDeleteCommand(g),
		newStoreObjectRegisterCommand(g),
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
	var buf []byte
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

		buf = buf[:0]
		if i > 0 {
			// Blank line between entries.
			buf = append(buf, '\n')
		}
		buf, err = backend.NewObjectInfo(path, resp.Info).AppendText(buf)
		if err != nil {
			return err
		}
		if _, err := os.Stdout.Write(buf); err != nil {
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
		if *outputPath == "" && term.IsTerminal(int(os.Stdout.Fd())) {
			return errors.New("refusing to send binary export to stdout (a tty). Pass --output=- to override.")
		}
		if *outputPath == "" {
			*outputPath = "-"
		}
		var err error
		opts.output, err = openOutputFile(*outputPath)
		if err != nil {
			return err
		}

		opts.paths = args
		return runStoreObjectExport(cmd.Context(), g, opts)
	}
	return c
}

func runStoreObjectExport(ctx context.Context, g *globalConfig, opts *storeObjectExportOptions) error {
	closer := xio.CloseOnce(opts.output)
	defer closer.Close()

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
	if err := closer.Close(); err != nil {
		return err
	}
	return nil
}

type nopReceiver struct{}

func (nopReceiver) Write(p []byte) (n int, err error)         { return len(p), nil }
func (nopReceiver) ReceiveNAR(trailer *zbstore.ExportTrailer) {}

type storeObjectImportOptions struct {
	paths []string
}

func newStoreObjectImportCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "import [options] [PATH [...]]",
		Short:                 "import store objects from a previous `zb store object export` command",
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := new(storeObjectImportOptions)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.paths = args
		return runStoreObjectImport(cmd.Context(), g, opts)
	}
	return c
}

func runStoreObjectImport(ctx context.Context, g *globalConfig, opts *storeObjectImportOptions) error {
	storeClient, waitStoreClient := g.storeClient(nil)
	defer func() {
		storeClient.Close()
		waitStoreClient()
	}()

	inputPaths := opts.paths
	if len(inputPaths) == 0 {
		inputPaths = []string{"-"}
	}
	if len(inputPaths) == 1 && inputPaths[0] == "-" && term.IsTerminal(int(os.Stdin.Fd())) {
		log.Infof(ctx, "Waiting for data on stdin...")
	}

	storePaths, err := catExports(ctx, storeClient, inputPaths)
	if err != nil {
		return err
	}
	ok := true
	for _, path := range storePaths {
		var exists bool
		err := jsonrpc.Do(ctx, storeClient, zbstorerpc.ExistsMethod, &exists, &zbstorerpc.ExistsRequest{
			Path: string(path),
		})
		if err != nil {
			log.Errorf(ctx, "Checking for existence of %s: %v", path, err)
		} else if !exists {
			log.Errorf(ctx, "Importing %s failed", path)
		} else {
			log.Infof(ctx, "Imported %s", path)
		}
	}
	if !ok {
		return errors.New("one or more paths not successfully imported")
	}
	return nil
}

// catExports concatenates the exports from the given files into a single export
// and sends it to the store connected via the given client.
func catExports(ctx context.Context, client *jsonrpc.Client, exportFiles []string) ([]zbstore.Path, error) {
	// If there are no files, then no-op.
	if len(exportFiles) == 0 {
		return nil, nil
	}

	// If there is a single file, then copy the file verbatim.
	if len(exportFiles) == 1 {
		f, err := openInputFile(exportFiles[0])
		if err != nil {
			return nil, err
		}
		defer f.Close()
		size := int64(-1)
		if info, err := f.Stat(); err != nil {
			log.Warnf(ctx, "Unable to get size of %s: %v", inputFileName(exportFiles[0]), err)
		} else if info.Mode().IsRegular() {
			size = info.Size()
		}

		// We still need to parse the export to determine store paths to confirm.
		// If this fails, don't fail the overall operation.
		pr, pw := io.Pipe()
		ch := make(chan []zbstore.Path)
		go func() {
			rec := &exportPathRecorder{ctx: ctx}
			if err := zbstore.ReceiveExport(rec, pr); err != nil {
				log.Warnf(ctx, "Invalid store export format in %s: %v", inputFileName(exportFiles[0]), err)
			}
			// If we encountered a parse error, still consume the rest of the stream.
			io.Copy(io.Discard, pr)
			pr.Close()
			ch <- rec.paths
		}()

		err = importToStore(ctx, client, io.TeeReader(f, pw), size)
		pw.Close()
		paths := <-ch
		return paths, err
	}

	// Start sending to the store.
	pr, pw := io.Pipe()
	ch := make(chan error)
	go func() {
		err := importToStore(ctx, client, pr, -1)
		pr.CloseWithError(err)
		ch <- err
		close(ch)
	}()
	defer func() { <-ch }()

	// Copy each NAR inside each export file.
	var storePaths []zbstore.Path
	exporter := zbstore.NewExporter(pw)
	for _, path := range exportFiles {
		var err error
		storePaths, err = copyToExporter(ctx, storePaths, exporter, path)
		if err != nil {
			return storePaths, err
		}
	}
	if err := exporter.Close(); err != nil {
		return storePaths, err
	}
	if err := pw.Close(); err != nil {
		return storePaths, err
	}
	err := <-ch
	return storePaths, err
}

// copyToExporter reads the file at path in the `nix-store --export` format
// and copies each NAR file to the exporter.
// It appends each of the store paths encountered to storePaths.
func copyToExporter(ctx context.Context, storePaths []zbstore.Path, exporter *zbstore.Exporter, path string) ([]zbstore.Path, error) {
	f, err := openInputFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	recv := &passthroughReceiver{exporter: exporter}
	rec := &exportPathRecorder{
		ctx:     ctx,
		paths:   storePaths,
		wrapped: recv,
	}
	if err := zbstore.ReceiveExport(rec, f); err != nil {
		return rec.paths, fmt.Errorf("copying %s: %v", inputFileName(path), err)
	}
	if recv.err != nil {
		return rec.paths, fmt.Errorf("copying %s: %v", inputFileName(path), recv.err)
	}
	return rec.paths, nil
}

// passthroughReceiver copies NAR files to an exporter.
// It is a helper for [copyToExporter].
type passthroughReceiver struct {
	exporter *zbstore.Exporter
	err      error
}

func (pr *passthroughReceiver) Write(p []byte) (int, error) {
	if pr.err != nil {
		return 0, pr.err
	}
	var n int
	n, pr.err = pr.exporter.Write(p)
	return n, pr.err
}

func (pr *passthroughReceiver) ReceiveNAR(trailer *zbstore.ExportTrailer) {
	if pr.err == nil {
		pr.err = pr.exporter.Trailer(trailer)
	}
}

// importToStore sends the content of r to client as an application/zb-store-export message.
// If size is non-negative, then it is used as the message's Content-Length header.
func importToStore(ctx context.Context, client *jsonrpc.Client, r io.Reader, size int64) error {
	generic, releaseConn, err := client.Codec(ctx)
	if err != nil {
		return err
	}
	defer releaseConn()
	codec, ok := generic.(*zbstorerpc.Codec)
	if !ok {
		return fmt.Errorf("store connection is %T (want %T)", generic, (*zbstorerpc.Codec)(nil))
	}

	var header jsonrpc.Header
	if size >= 0 {
		header = make(jsonrpc.Header)
		header.Set("Content-Length", strconv.FormatInt(size, 10))
		r = io.LimitReader(r, size)
	}
	return codec.Export(header, r)
}

// exportPathRecorder is a [zbstore.NARReceiver] that records the store paths encountered.
// It optionally passes through its methods to wrapped.
type exportPathRecorder struct {
	ctx     context.Context
	paths   []zbstore.Path
	wrapped zbstore.NARReceiver
}

func (rec *exportPathRecorder) Write(p []byte) (n int, err error) {
	if rec.wrapped == nil {
		return len(p), nil
	}
	return rec.wrapped.Write(p)
}

func (rec *exportPathRecorder) ReceiveNAR(trailer *zbstore.ExportTrailer) {
	log.Debugf(rec.ctx, "Found trailer for %s", trailer.StorePath)
	rec.paths = append(rec.paths, trailer.StorePath)

	if rec.wrapped != nil {
		rec.wrapped.ReceiveNAR(trailer)
	}
}

type storeObjectDeleteOptions struct {
	paths     []zbstore.Path
	recursive bool
	dbPath    string
}

func newStoreObjectDeleteCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "delete [options] PATH [...]",
		Short:                 "delete one or more store objects",
		Aliases:               []string{"rm"},
		DisableFlagsInUseLine: true,
		Args:                  cobra.MinimumNArgs(1),
		SilenceErrors:         true,
		SilenceUsage:          true,
		Hidden:                true,
	}
	opts := &storeObjectDeleteOptions{
		dbPath: filepath.Join(defaultVarDir(), "db.sqlite"),
	}
	c.Flags().StringVar(&opts.dbPath, "db", opts.dbPath, "`path` to store database file")
	c.Flags().BoolVarP(&opts.recursive, "recursive", "r", false, "delete objects that depend on the paths")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		opts.paths = make([]zbstore.Path, 0, len(args))
		for _, arg := range args {
			arg, err := filepath.Abs(arg)
			if err != nil {
				return err
			}
			path, err := zbstore.ParsePath(arg)
			if err != nil {
				return err
			}
			opts.paths = append(opts.paths, path)
		}
		return runStoreObjectDelete(cmd.Context(), g, opts)
	}
	return c
}

func runStoreObjectDelete(ctx context.Context, g *globalConfig, opts *storeObjectDeleteOptions) error {
	backendServer := backend.NewServer(g.storeDir, opts.dbPath, &backend.Options{
		DatabasePoolSize:  1,
		DisableSandbox:    true,
		BuildLogRetention: -1,
	})
	defer backendServer.Close()

	f := backendServer.Delete
	if opts.recursive {
		f = backendServer.DeleteIncludingReferences
	}
	if err := f(ctx, sets.New(opts.paths...)); err != nil {
		return err
	}

	return nil
}

type storeObjectRegisterOptions struct {
	input  io.Reader
	dbPath string
}

//go:embed docs/store_object_register.txt
var storeObjectRegisterDoc string

func newStoreObjectRegisterCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "register [options]",
		Short:                 "add info for objects already present in the store directory",
		Long:                  storeObjectRegisterDoc,
		DisableFlagsInUseLine: true,
		Args:                  cobra.NoArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
		Hidden:                true,
	}
	opts := &storeObjectRegisterOptions{
		input:  os.Stdin,
		dbPath: filepath.Join(defaultVarDir(), "db.sqlite"),
	}
	c.Flags().StringVar(&opts.dbPath, "db", opts.dbPath, "`path` to store database file")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runStoreObjectRegister(cmd.Context(), g, opts)
	}
	return c
}

func runStoreObjectRegister(ctx context.Context, g *globalConfig, opts *storeObjectRegisterOptions) error {
	if err := os.MkdirAll(filepath.Dir(opts.dbPath), 0o755); err != nil {
		return err
	}

	backendServer := backend.NewServer(g.storeDir, opts.dbPath, &backend.Options{
		DatabasePoolSize:            1,
		DisableSandbox:              true,
		BuildLogRetention:           -1,
		ContentAddressBufferCreator: bytebuffer.TempFileCreator{Pattern: contentAddressTempFilePattern},
	})
	defer backendServer.Close()

	s := bufio.NewScanner(opts.input)
	s.Split(splitObjectInfos)
	ok := true
	for info := new(backend.ObjectInfo); s.Scan(); {
		err := info.UnmarshalText(s.Bytes())
		if err != nil {
			log.Errorf(ctx, "Invalid object (skipping): %v", err)
			ok = false
			continue
		}
		if err := backendServer.Register(ctx, info); err != nil {
			log.Errorf(ctx, "Failed: %v", err)
			ok = false
		}
	}
	if !ok {
		return fmt.Errorf("one or more objects were not registered")
	}
	return nil
}

func splitObjectInfos(data []byte, atEOF bool) (advance int, token []byte, err error) {
	switch i := bytes.Index(data, []byte("\nStorePath:")); {
	case i >= 0:
		return i + 1, data[:i+1], nil
	case atEOF && len(data) == 0:
		return 0, nil, bufio.ErrFinalToken
	case atEOF && len(data) > 0:
		return len(data), data, bufio.ErrFinalToken
	default:
		return 0, nil, nil
	}
}
