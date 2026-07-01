// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package httpcache

import (
	"io"
	"net/http"
)

// maxBytesReader is an [io.Reader] that returns an [http.MaxBytesError]
// after n+1 bytes read.
type maxBytesReader struct {
	r   io.Reader
	i   int64
	n   int64
	err error
}

func newMaxBytesReader(r io.Reader, n int64) *maxBytesReader {
	return &maxBytesReader{
		r: r,
		i: n,
		n: n + 1,
	}
}

func (mbr *maxBytesReader) Read(p []byte) (n int, err error) {
	if mbr.err != nil {
		return 0, mbr.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if int64(len(p)) > mbr.n {
		p = p[:mbr.n]
	}
	n, err = mbr.r.Read(p)
	if int64(n) > mbr.n-1 {
		mbr.n = 0
		mbr.err = &http.MaxBytesError{Limit: mbr.i}
		return n, mbr.err
	}
	mbr.n -= int64(n)
	mbr.err = err
	return n, err
}

// limitedCopy copies limit+1 bytes (or until an error) from src to dst.
// If limit <= 0, then it is identical to [io.Copy].
// It returns the number of bytes copied
// and the earliest error encountered while copying.
//
// If dst implements [io.ReaderFrom], the copy is implemented using it.
func limitedCopy(dst io.Writer, src io.Reader, limit int64) (written int64, err error) {
	if limit <= 0 {
		return io.Copy(dst, src)
	}

	written, err = io.Copy(dst, newMaxBytesReader(src, limit))
	if written < limit && err == nil {
		err = io.EOF
	}
	return written, err
}
