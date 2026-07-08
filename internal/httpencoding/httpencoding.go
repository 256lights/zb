// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

// Package httpencoding provides a function to decompress an HTTP request body.
package httpencoding

import (
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"

	"github.com/dsnet/compress/brotli"
)

// Accept is the value of an [Accept-Encoding header field]
// that advertises the algorithms that [Decode] supports.
//
// [Accept-Encoding header field]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Accept-Encoding
const Accept = "br,gzip,deflate"

// Decode returns an [io.ReadCloser] that reads from r
// according to the value of an [Content-Encoding header field].
//
// [Content-Encoding header field]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Encoding
func Decode(r io.Reader, contentEncoding string) (io.ReadCloser, error) {
	switch contentEncoding {
	case "":
		return io.NopCloser(r), nil
	case "br":
		return brotli.NewReader(r, nil)
	case "gzip", "x-gzip":
		return gzip.NewReader(r)
	case "deflate":
		return flate.NewReader(r), nil
	default:
		return nil, unsupportedError{contentEncoding}
	}
}

// IsUnsupported reports whether err represents an unknown Content-Encoding value
// passed to [Decode].
func IsUnsupported(err error) bool {
	_, ok := errors.AsType[unsupportedError](err)
	return ok
}

type unsupportedError struct {
	contentEncoding string
}

func (cee unsupportedError) Error() string {
	return fmt.Sprintf("unsupported Content-Encoding %s", cee.contentEncoding)
}
