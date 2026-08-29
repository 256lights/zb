// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package zbstorerpc

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	"golang.org/x/sync/errgroup"
	"zb.256lights.llc/pkg/internal/jsonrpc"
	"zb.256lights.llc/pkg/sets"
	"zb.256lights.llc/pkg/zbstore"
	"zombiezen.com/go/log"
)

// Client implements [zbstore.Store], [zbstore.Importer], and [zbstore.Exporter] via JSON-RPC.
type Client struct {
	client *jsonrpc.Client
}

// NewClient returns a [*Client] that opens connections using the given function.
// The caller is responsible for calling [*Client.Close] when the client is no longer in use.
func NewClient(ctx context.Context, openConn func(context.Context) (io.ReadWriteCloser, error)) *Client {
	s := new(Client)
	s.client = jsonrpc.NewClient(ctx, func(ctx context.Context) (jsonrpc.ClientCodec, error) {
		conn, err := openConn(ctx)
		if err != nil {
			return nil, err
		}
		return newClientCodec(ctx, conn), nil
	})
	return s
}

// Close closes the client connection.
func (c *Client) Close() error {
	return c.client.Close()
}

// JSONRPC implements [jsonrpc.Handler] by sending a request to the server.
func (c *Client) JSONRPC(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	if req.Method == ExportMethod {
		// Prevent users from initiating exports except through [*Client.StoreExport].
		return nil, jsonrpc.Error(jsonrpc.MethodNotFound, fmt.Errorf("method %q not found", req.Method))
	}
	return c.client.JSONRPC(ctx, req)
}

// Object implements [zbstore.Store] by making a [InfoRequest] to s.Handler.
//
// Calling [zbstore.Object.WriteNAR] on the returned object
// depends on s.Handler being wired up to [*Client.Import].
// Otherwise, WriteNAR will block until ctx.Done() is closed.
func (c *Client) Object(ctx context.Context, path zbstore.Path) (zbstore.Object, error) {
	resp := new(InfoResponse)
	err := jsonrpc.Do(ctx, c.client, InfoMethod, resp, &InfoRequest{Path: path})
	if err != nil {
		return nil, fmt.Errorf("stat %s: %v", path, err)
	}
	if resp.Info == nil {
		return nil, fmt.Errorf("stat %s: %w", path, zbstore.ErrNotFound)
	}
	return &object{
		store: c,
		info:  *resp.Info.WithPath(path),
	}, nil
}

// StoreImport implements [zbstore.Importer]
// by sending the `nix-store --export` data over the underlying connection.
// StoreImport will return an error if s.Handler is not a [*jsonrpc.Client] using a [*codec].
func (c *Client) StoreImport(ctx context.Context, r io.Reader) error {
	generic, releaseConn, err := c.client.Codec(ctx)
	if err != nil {
		return err
	}
	zc, ok := generic.(*clientCodec)
	if !ok {
		releaseConn()
		return fmt.Errorf("import store objects: store connection is %T (want %T)", generic, (*clientCodec)(nil))
	}
	err = writeExport(ctx, zc.w, "", r)
	releaseConn()
	if err != nil {
		return fmt.Errorf("import store objects: %v", err)
	}

	// Add sync point via doing a no-op RPC.
	// This ensures that the export has been processed before returning.
	if err := jsonrpc.Do(ctx, c.client, NopMethod, nil, nil); err != nil {
		return fmt.Errorf("import store objects: wait for export to complete: %v", err)
	}
	return nil
}

// StoreExport implements [zbstore.Exporter]
// by sending an [ExportRequest] to s.Handler.
//
// StoreExport depends on s.Handler being wired up to [*Client.Import].
// Otherwise, StoreExport will block until ctx.Done() is closed.
func (c *Client) StoreExport(ctx context.Context, dst io.Writer, paths sets.Set[zbstore.Path], opts *zbstore.ExportOptions) error {
	if err := c.export(ctx, dst, NewExportRequest(paths, opts)); err != nil {
		return fmt.Errorf("export store objects: %w", err)
	}
	return nil
}

func (c *Client) export(ctx context.Context, dst io.Writer, req *ExportRequest) error {
	var id string
	writerChan := make(chan io.Writer)
	writeDone := make(chan error)
	rpcDone := make(chan error, 1)
	err := func() error {
		generic, releaseConn, err := c.client.Codec(ctx)
		if err != nil {
			return err
		}
		defer releaseConn()
		cc, ok := generic.(*clientCodec)
		if !ok {
			return fmt.Errorf("store connection is %T (want %T)", generic, (*clientCodec)(nil))
		}

		cc.mu.Lock()
		if cc.idPrefix == "" {
			var bits [9]byte
			rand.Read(bits[:])
			cc.idPrefix = base64.URLEncoding.EncodeToString(bits[:])
		}
		id = cc.idPrefix + strconv.FormatUint(cc.idCounter, 16)
		cc.idCounter++
		requestJSON, err := jsonv2.Marshal(&exportRPCRequest{
			id:     id,
			params: req,
		})
		if err != nil {
			cc.mu.Unlock()
			return err
		}
		cc.pendingExports[id] = pendingExport{
			w:         writerChan,
			writeDone: writeDone,
			rpcDone:   rpcDone,
		}
		cc.mu.Unlock()

		log.Debugf(ctx, "Sending export request for %v with id=%+q...", req.Paths, id)
		err = cc.WriteRequest(requestJSON)
		if err != nil {
			cc.mu.Lock()
			close(writerChan)
			delete(cc.pendingExports, id)
			cc.mu.Unlock()
			return err
		}

		return nil
	}()
	if err != nil {
		return err
	}

	select {
	case writerChan <- dst:
		// If [*clientCodec.receiveExport] started writing to dst,
		// then we need to wait until it is done before returning
		// to avoid writing to dst after export returns.
		importError := <-writeDone
		select {
		case rpcError := <-rpcDone:
			log.Debugf(ctx, "Export RPC finished: id=%+q err=%v", id, err)
			if errors.Is(rpcError, errInterrupt) {
				// Connection closed is not an interesting error,
				// especially if the import succeeded overall.
				rpcError = nil
			}
			return errors.Join(rpcError, importError)
		case <-ctx.Done():
			if importError != nil {
				return importError
			}
			return ctx.Err()
		}
	case err := <-rpcDone:
		log.Debugf(ctx, "Export RPC finished: id=%+q err=%v", id, err)
		if err == nil {
			err = errors.New("server did not send export")
		}
		return err
	case <-ctx.Done():
		close(writerChan)
		// TODO(someday): Send cancel message.
		// Would need to synchronize with [*jsonrpc.Client].
		return ctx.Err()
	}
}

type object struct {
	store *Client
	info  zbstore.ObjectInfo
}

func (obj *object) Info() *zbstore.ObjectInfo {
	return &obj.info
}

func (obj *object) WriteNAR(ctx context.Context, dst io.Writer) error {
	pr, pw := io.Pipe()
	grp, ctx := errgroup.WithContext(ctx)
	grp.Go(func() error {
		err := obj.store.export(ctx, pw, &ExportRequest{
			Paths:             []zbstore.Path{obj.info.StorePath},
			ExcludeReferences: true,
		})
		pw.CloseWithError(err)
		return err
	})
	grp.Go(func() error {
		err := zbstore.ReceiveExport(&singleNARReceiver{w: dst}, pr)
		pr.CloseWithError(err)
		return err
	})
	if err := grp.Wait(); err != nil {
		return fmt.Errorf("write nar for %s: %w", obj.info.StorePath, err)
	}
	return nil
}

type singleNARReceiver struct {
	w        io.Writer
	received bool
}

func (snr *singleNARReceiver) Write(p []byte) (n int, err error) {
	if snr.received {
		return 0, fmt.Errorf("received multiple store objects from export")
	}
	return snr.w.Write(p)
}

func (snr *singleNARReceiver) ReceiveNAR(trailer *zbstore.ExportTrailer) {
	snr.received = true
}

// clientCodec implements [jsonrpc.ClientCodec] on an [io.ReadWriteCloser]
// using the Language Server Protocol "base protocol" for framing.
type clientCodec struct {
	w *jsonrpc.Writer
	c io.Closer

	messages  <-chan jsontext.Value
	readError error // can only be read after messages is closed
	readDone  <-chan struct{}

	mu             sync.Mutex
	idPrefix       string
	idCounter      uint64
	pendingExports map[string]pendingExport
}

type pendingExport struct {
	w         <-chan io.Writer
	writeDone chan<- error
	rpcDone   chan<- error
}

func newClientCodec(ctx context.Context, rwc io.ReadWriteCloser) *clientCodec {
	c := new(clientCodec)
	messages := make(chan jsontext.Value)
	readDone := make(chan struct{})
	*c = clientCodec{
		w:              jsonrpc.NewWriter(rwc),
		c:              rwc,
		messages:       messages,
		readDone:       readDone,
		pendingExports: make(map[string]pendingExport),
	}
	go func() {
		defer func() {
			close(messages)
			close(readDone)
		}()
		c.readError = c.readLoop(ctx, messages, jsonrpc.NewReader(rwc))
	}()
	return c
}

func (cc *clientCodec) WriteRequest(request jsontext.Value) error {
	return writeRPCMessage(cc.w, request)
}

func (cc *clientCodec) ReadResponse() (jsontext.Value, error) {
	msg, ok := <-cc.messages
	if !ok {
		return nil, cc.readError
	}
	return msg, nil
}

func (cc *clientCodec) readLoop(ctx context.Context, messages chan<- jsontext.Value, r *jsonrpc.Reader) error {
	for {
		header, bodySize, err := r.NextMessage()
		if err != nil {
			return err
		}
		switch ct := header.Get("Content-Type"); ct {
		case rpcContentType:
			if bodySize < 0 {
				return fmt.Errorf("remote sent api message without valid Content-Length")
			}
			if bodySize > maxAPIMessageSize {
				return fmt.Errorf("remote sent large api message (%d bytes)", maxAPIMessageSize)
			}
			body, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			if !cc.interceptExportResponse(body) {
				messages <- body
			}
		case exportContentType:
			if err := cc.receiveExport(ctx, header, r); err != nil {
				if bodySize < 0 {
					return err
				}
				log.Warnf(ctx, "%v", err)
			}
		default:
			// Ignore, if possible.
			if bodySize < 0 {
				return fmt.Errorf("remote sent unknown Content-Type %q without valid Content-Length", ct)
			}
		}
	}
}

func (cc *clientCodec) interceptExportResponse(response jsontext.Value) bool {
	cc.mu.Lock()
	hasExports := len(cc.pendingExports) > 0
	cc.mu.Unlock()
	if !hasExports {
		// We don't need to parse payload if we don't have any pending exports.
		return false
	}

	var parsed struct {
		Version string         `json:"jsonrpc"`
		ID      string         `json:"id"`
		Error   jsontext.Value `json:"error"`
	}
	if err := jsonv2.Unmarshal(response, &parsed); err != nil {
		return false
	}
	if parsed.Version != "2.0" || parsed.ID == "" {
		return false
	}

	cc.mu.Lock()
	e, idKnown := cc.pendingExports[parsed.ID]
	delete(cc.pendingExports, parsed.ID)
	cc.mu.Unlock()

	if !idKnown {
		return false
	}
	var rpcError error
	if len(parsed.Error) > 0 {
		var errorObject struct {
			Code    jsonrpc.ErrorCode `json:"code"`
			Message string            `json:"message"`
		}
		if err := jsonv2.Unmarshal(parsed.Error, &errorObject); err != nil {
			// If we can't unmarshal, use the JSON directly.
			// Better than nothing for debugging.
			rpcError = errors.New(parsed.Error.String())
		} else if errorObject.Message != "" {
			rpcError = jsonrpc.Error(errorObject.Code, errors.New(errorObject.Message))
		} else {
			rpcError = jsonrpc.Error(errorObject.Code, fmt.Errorf("jsonrpc error %d", errorObject.Code))
		}
	}
	e.rpcDone <- rpcError
	return true
}

func (cc *clientCodec) receiveExport(ctx context.Context, header jsonrpc.Header, r io.Reader) error {
	id := header.Get(exportIDHeaderName)
	var w io.Writer
	var done chan<- error
	idKnown := false
	if id != "" {
		cc.mu.Lock()
		var e pendingExport
		e, idKnown = cc.pendingExports[id]
		if idKnown && e.w != nil {
			cc.pendingExports[id] = pendingExport{
				rpcDone: e.rpcDone,
			}
		}
		cc.mu.Unlock()

		// Mostly synchronous: sender is either blocking sending this or closed.
		if e.w == nil {
			log.Debugf(ctx, "Received duplicate export over RPC with id=%+q", id)
		} else {
			w = <-e.w
			if w == nil {
				log.Debugf(ctx, "Received export over RPC with id=%+q. No longer interested.", id)
			} else {
				done = e.writeDone
			}
		}
	}
	if !idKnown {
		log.Warnf(ctx, "Received unsolicited export over RPC with id=%+q", id)
	}
	var importError error
	if w == nil {
		importError = nopImporter{}.StoreImport(ctx, r)
	} else {
		log.Debugf(ctx, "Receiving export over RPC with id=%+q...", id)
		// The Importer.Import method determines the boundary of the body.
		// When we tee, we don't want copy failures downstream
		// to mess up our JSON-RPC connection.
		// We swallow the errors and try to read the `nix-store --export` data to the end.
		ecw := &errorCaptureWriter{w: w}
		importError = nopImporter{}.StoreImport(ctx, io.TeeReader(r, ecw))
		done <- cmp.Or(importError, ecw.err)
	}
	log.Debugf(ctx, "Finished receiving RPC export id=%+q err=%v", id, importError)
	if importError != nil {
		return fmt.Errorf("while receiving export: %w", importError)
	}
	return nil
}

func (cc *clientCodec) Close() error {
	err := cc.c.Close()
	<-cc.readDone

	cc.mu.Lock()
	for _, e := range cc.pendingExports {
		e.rpcDone <- errInterrupt
	}
	clear(cc.pendingExports)
	cc.mu.Unlock()

	return err
}

// errorCaptureWriter passes through writes to another [io.Writer]
// until an error occurs,
// but will never surface the error.
type errorCaptureWriter struct {
	w   io.Writer
	err error
}

// Write writes p to ecw.w unless an error has occurred.
// Write always returns len(p), nil.
func (ecw *errorCaptureWriter) Write(p []byte) (int, error) {
	if ecw.err == nil {
		_, ecw.err = ecw.w.Write(p)
	}
	return len(p), nil
}

var errInterrupt = errors.New("connection interrupted")
