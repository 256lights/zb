// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/zb/internal/jsonrpc"
	"zombiezen.com/go/zb/zbstore"
)

type serveOptions struct {
}

func newServeCommand(g *globalConfig) *cobra.Command {
	c := &cobra.Command{
		Use:                   "serve [options]",
		Short:                 "run a build server",
		DisableFlagsInUseLine: true,
		Args:                  cobra.NoArgs,
		SilenceErrors:         true,
		SilenceUsage:          true,
	}
	opts := new(serveOptions)
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return runServe(cmd.Context(), g, opts)
	}
	return c
}

func runServe(ctx context.Context, g *globalConfig, opts *serveOptions) error {
	if !g.storeDir.IsNative() {
		return fmt.Errorf("%s cannot be used on this system", g.storeDir)
	}
	if err := os.MkdirAll(filepath.Dir(string(g.storeDir)), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(string(g.storeDir), 0o755|os.ModeSticky); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(g.storeSocket), 0o755); err != nil {
		return err
	}

	l, err := net.Listen("unix", g.storeSocket)
	if err != nil {
		return err
	}
	defer l.Close()

	log.Infof(ctx, "Listening on %s", g.storeSocket)
	srv := &storeServer{
		dir: g.storeDir,
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			recv := &storeNARReceiver{
				dir: g.storeDir,
			}
			jsonrpc.Serve(ctx, zbstore.NewServerCodec(conn, recv), srv)
			recv.cleanup(ctx)
		}()
	}
}

type storeServer struct {
	dir zbstore.Directory
}

func (s *storeServer) JSONRPC(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	return jsonrpc.ServeMux{
		zbstore.ExistsMethod:  jsonrpc.HandlerFunc(s.exists),
		zbstore.RealizeMethod: jsonrpc.HandlerFunc(s.realize),
	}.JSONRPC(ctx, req)
}

func (s *storeServer) exists(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	var args zbstore.ExistsRequest
	if err := json.Unmarshal(req.Params, &args); err != nil {
		return nil, jsonrpc.Error(jsonrpc.InvalidParams, err)
	}
	p, sub, err := s.dir.ParsePath(args.Path)
	if err != nil {
		return &jsonrpc.Response{
			Result: json.RawMessage("false"),
		}, nil
	}
	if _, err := os.Lstat(p.Join(sub)); err != nil {
		return &jsonrpc.Response{
			Result: json.RawMessage("false"),
		}, nil
	}
	return &jsonrpc.Response{
		Result: json.RawMessage("true"),
	}, nil
}

func (s *storeServer) realize(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	return nil, fmt.Errorf("TODO(soon)")
}

type storeNARReceiver struct {
	dir     zbstore.Directory
	tmpFile *os.File
}

func (s *storeNARReceiver) Write(p []byte) (n int, err error) {
	if s.tmpFile == nil {
		s.tmpFile, err = os.CreateTemp("", "zb-serve-receive-*.nar")
		if err != nil {
			return 0, err
		}
	}
	return s.tmpFile.Write(p)
}

func (s *storeNARReceiver) ReceiveNAR(trailer *zbstore.ExportTrailer) {
	ctx := context.TODO()
	if s.tmpFile == nil {
		// No bytes written? Not a valid NAR.
		return
	}
	if _, err := s.tmpFile.Seek(0, io.SeekStart); err != nil {
		log.Warnf(ctx, "Unable to seek in store temp file: %v", err)
		s.cleanup(ctx)
		return
	}
	defer func() {
		if err := s.tmpFile.Truncate(0); err != nil {
			log.Warnf(ctx, "Unable to truncate store temp file: %v", err)
			s.cleanup(ctx)
			return
		}
	}()

	if trailer.StorePath.Dir() != s.dir {
		log.Warnf(ctx, "Rejecting %s (not in %s)", trailer.StorePath, s.dir)
		return
	}
	// TODO(soon): Prevent this from racing.
	if _, err := os.Lstat(string(trailer.StorePath)); err == nil {
		log.Debugf(ctx, "Received NAR for %s. Exists on disk, skipping...", trailer.StorePath)
		return
	}

	if err := importNAR(s.tmpFile, trailer); err != nil {
		log.Warnf(ctx, "Import of %s failed: %v", trailer.StorePath, err)
		if err := os.RemoveAll(string(trailer.StorePath)); err != nil {
			log.Errorf(ctx, "Failed to clean up partial import of %s: %v", trailer.StorePath, err)
		}
		return
	}
}

func importNAR(r io.Reader, trailer *zbstore.ExportTrailer) error {
	nr := nar.NewReader(r)
	for {
		hdr, err := nr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p := trailer.StorePath.Join(hdr.Path)
		switch typ := hdr.Mode.Type(); typ {
		case 0:
			perm := os.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				perm = 0o755
			}
			f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, nr)
			err2 := f.Close()
			if err != nil {
				return err
			}
			if err2 != nil {
				return err2
			}
		case fs.ModeDir:
			if err := os.Mkdir(p, 0o755); err != nil {
				return err
			}
		case fs.ModeSymlink:
			if err := os.Symlink(hdr.LinkTarget, p); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unhandled type %v", typ)
		}
	}
}

func (s *storeNARReceiver) cleanup(ctx context.Context) {
	if s.tmpFile == nil {
		return
	}
	name := s.tmpFile.Name()
	s.tmpFile.Close()
	s.tmpFile = nil
	if err := os.Remove(name); err != nil {
		log.Warnf(ctx, "Unable to remove store temp file: %v", err)
	}
}
