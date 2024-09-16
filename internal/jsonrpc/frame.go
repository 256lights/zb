// Copyright 2024 The zb Authors
// SPDX-License-Identifier: MIT

package jsonrpc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
)

// Header represents a message's header.
type Header = textproto.MIMEHeader

// A Reader reads framed messages from an underlying [io.Reader].
// Reader introduces its own buffering,
// so it may consume more bytes than needed to read a message.
type Reader struct {
	br            *bufio.Reader
	err           error
	bodyRemaining int64
	readStarted   bool
}

// NewReader returns a new [Reader] that reads from r.
func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReader(r)}
}

// NextMessage reads the next message header from the underlying reader.
// If the previous message's body was not fully read
// as determined by the Content-Length of the previous message,
// it will be discarded.
//
// The message's Content-Length will be returned as bodySize,
// or -1 if the current message does not contain a valid Content-Length.
// If bodySize is -1,
// then the caller should decide whether it wants to consume the message based on the header,
// since the caller will be responsible for reading exactly the number of bytes
// that the body consumes.
func (r *Reader) NextMessage() (header Header, bodySize int64, err error) {
	if r.err != nil {
		return nil, -1, fmt.Errorf("read rpc message: %w", r.err)
	}

	if r.bodyRemaining > 0 {
		if _, err := io.CopyN(io.Discard, r.br, r.bodyRemaining); err != nil {
			r.err = err
			return nil, -1, fmt.Errorf("read rpc message: %w", err)
		}
	}

	r.readStarted = false
	header, err = textproto.NewReader(r.br).ReadMIMEHeader()
	if err != nil {
		r.err = err
		return nil, -1, fmt.Errorf("read rpc message: %w", err)
	}
	r.bodyRemaining, err = contentLength(header)
	if err == errNoContentLength {
		r.bodyRemaining = -1
	} else if err != nil {
		r.err = err
		return header, -1, fmt.Errorf("read rpc message: %w", err)
	}
	return header, r.bodyRemaining, nil
}

// Read reads bytes from the body of a message.
// If the message header had a valid Content-Length,
// then Read will return [io.EOF] once the body's end has been reached.
func (r *Reader) Read(p []byte) (n int, err error) {
	if r.bodyRemaining == 0 {
		return 0, io.EOF
	}
	if r.err != nil {
		return 0, r.err
	}
	if len(p) == 0 {
		return 0, nil
	}

	if r.bodyRemaining > 0 && int64(len(p)) > r.bodyRemaining {
		p = p[:r.bodyRemaining]
	}
	n, r.err = r.br.Read(p)
	if r.bodyRemaining > 0 {
		r.bodyRemaining = max(0, r.bodyRemaining-int64(n))
	}
	if n > 0 {
		r.readStarted = true
	}
	if r.err == io.EOF && r.bodyRemaining > 0 {
		r.err = io.ErrUnexpectedEOF
	}

	err = r.err
	if r.bodyRemaining == 0 {
		// Avoid an extra call to Read.
		err = io.EOF
	}
	return n, err
}

// ReadByte reads a single byte from the body of a message.
// If the message header had a valid Content-Length,
// then ReadByte will return [io.EOF] once the body's end has been reached.
func (r *Reader) ReadByte() (byte, error) {
	if r.bodyRemaining == 0 {
		return 0, io.EOF
	}
	if r.err != nil {
		return 0, r.err
	}

	var b byte
	b, r.err = r.br.ReadByte()
	if r.err != nil {
		return 0, r.err
	}
	r.readStarted = true
	if r.bodyRemaining > 0 {
		r.bodyRemaining--
	}
	return b, nil
}

// UnreadByte unreads the last byte from the body of a message.
func (r *Reader) UnreadByte() error {
	if !r.readStarted {
		// Don't unread into the header.
		return bufio.ErrInvalidUnreadByte
	}
	if err := r.br.UnreadByte(); err != nil {
		return err
	}
	if r.bodyRemaining >= 0 {
		r.bodyRemaining++
	}
	r.err = nil
	return nil
}

// A Writer writes framed messages to an underlying [io.Writer].
type Writer struct {
	w         io.Writer
	headerBuf bytes.Buffer
	err       error
}

// NewWriter returns a new [Writer] that writes to w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteMessage writes a message to the connection.
func (c *Writer) WriteMessage(header Header, body io.Reader) error {
	n, err := contentLength(header)
	if err == errNoContentLength {
		n = -1
	} else if err != nil {
		return fmt.Errorf("write rpc message: %v", err)
	}

	if c.err != nil {
		return c.err
	}

	c.headerBuf.Reset()
	if err := writeHeader(&c.headerBuf, header); err != nil {
		return fmt.Errorf("write rpc message: %v", err)
	}
	if _, err := io.Copy(c.w, &c.headerBuf); err != nil {
		c.err = fmt.Errorf("write rpc message: aborted due to previous error: %v", err)
		return fmt.Errorf("write rpc message: %v", err)
	}

	if n == 0 {
		return nil
	}
	if n > 0 {
		body = io.LimitReader(body, n)
	}

	w := &errWriter{w: c.w}
	written, err := io.Copy(w, body)
	if n > 0 {
		if written == n {
			err = nil
		} else if written < n && err == nil {
			err = io.ErrUnexpectedEOF
		}
	}
	if err != nil {
		if w.err != nil {
			c.err = fmt.Errorf("write rpc message: aborted due to previous error: %v", w.err)
		} else {
			c.err = fmt.Errorf("write rpc message: previous message failed to complete")
		}
		return fmt.Errorf("write rpc message: %v", err)
	}
	return nil
}

func writeHeader(w io.StringWriter, h Header) error {
	for k, v := range h {
		for _, vv := range v {
			if strings.ContainsAny(vv, "\r\n") {
				return fmt.Errorf("write header: %s value contains newline", k)
			}
		}
	}

	keys := mapKeys(h)
	slices.Sort(keys)

	w2 := &errStringWriter{w: w}
	for _, k := range keys {
		for _, v := range h[k] {
			w2.WriteString(k)
			w2.WriteString(": ")
			w2.WriteString(v)
			w2.WriteString("\r\n")
		}
	}
	w2.WriteString("\r\n")
	return w2.err
}

func contentLength(header Header) (int64, error) {
	s := header.Get("Content-Length")
	if s == "" {
		return -1, errNoContentLength
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return -1, fmt.Errorf("invalid Content-Length %q", s)
	}
	return n, nil
}

var errNoContentLength = errors.New("Content-Length not provided")

func mapKeys[K comparable, V any, M ~map[K]V](m M) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

type errWriter struct {
	w   io.Writer
	err error
}

func (w *errWriter) Write(p []byte) (n int, err error) {
	if w.err != nil {
		return 0, w.err
	}
	n, w.err = w.w.Write(p)
	return n, w.err
}

type errStringWriter struct {
	w   io.StringWriter
	err error
}

func (w *errStringWriter) WriteString(s string) (n int, err error) {
	if w.err != nil {
		return 0, w.err
	}
	n, w.err = w.w.WriteString(s)
	return n, w.err
}
