// Copyright 2024 Ross Light
// SPDX-License-Identifier: MIT

package zb

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/zb/internal/sortedset"
)

type nixImporter struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	header bool
}

func startImport(ctx context.Context) (*nixImporter, error) {
	c := exec.CommandContext(ctx, "nix-store", "--import")
	c.Stderr = os.Stderr
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("nix-store --import: %v", err)
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("nix-store --import: %v", err)
	}
	return &nixImporter{
		cmd:   c,
		stdin: stdin,
	}, nil
}

func (imp *nixImporter) Write(p []byte) (int, error) {
	if !imp.header {
		if _, err := io.WriteString(imp.stdin, "\x01\x00\x00\x00\x00\x00\x00\x00"); err != nil {
			imp.close()
			return 0, err
		}
		imp.header = true
	}
	n, err := imp.stdin.Write(p)
	if err != nil {
		imp.close()
	}
	return n, err
}

type nixExportTrailer struct {
	storePath  nix.StorePath
	references sortedset.Set[nix.StorePath]
	deriver    nix.StorePath
}

func (imp *nixImporter) Trailer(t *nixExportTrailer) error {
	if !imp.header {
		return fmt.Errorf("write nix store export trailer: NAR not yet written")
	}
	imp.header = false

	log.Debugf(context.TODO(), "Imported store path %s", t.storePath)
	trailer := []byte{
		'N', 'I', 'X', 'E', 0, 0, 0, 0,
	}
	trailer = appendNARString(trailer, string(t.storePath))
	trailer = binary.LittleEndian.AppendUint64(trailer, uint64(t.references.Len()))
	for i := 0; i < t.references.Len(); i++ {
		trailer = appendNARString(trailer, string(t.references.At(i)))
	}
	trailer = appendNARString(trailer, string(t.deriver))
	trailer = append(trailer, 0, 0, 0, 0, 0, 0, 0, 0)
	if _, err := imp.stdin.Write(trailer); err != nil {
		imp.close()
		return err
	}
	return nil
}

func (imp *nixImporter) Close() error {
	if imp.cmd == nil {
		return errors.New("nix-store --import finished")
	}

	var errs [2]error
	_, errs[0] = io.WriteString(imp.stdin, "\x00\x00\x00\x00\x00\x00\x00\x00")
	errs[1] = imp.close()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (imp *nixImporter) close() error {
	var errs [2]error
	errs[0] = imp.stdin.Close()
	// TODO(soon): Send SIGTERM.
	errs[1] = imp.cmd.Wait()
	imp.cmd = nil
	imp.stdin = nil

	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func appendNARString(dst []byte, s string) []byte {
	dst = binary.LittleEndian.AppendUint64(dst, uint64(len(s)))
	dst = append(dst, s...)
	if off := len(s) % 8; off != 0 {
		for i := 0; i < 8-off; i++ {
			dst = append(dst, 0)
		}
	}
	return dst
}
