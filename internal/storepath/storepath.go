// Copyright 2024 Roxy Light
// SPDX-License-Identifier: MIT

// Package storepath provides shared internal logic for store paths.
package storepath

import (
	"hash"
	"io"

	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nixbase32"
)

// MakeDigest computes the digest of a store path.
// h must be a SHA-256 hash (as obtained by [crypto/sha256.New])
// and the type must have already been written to it.
func MakeDigest(h hash.Hash, dir string, hash nix.Hash, name string) string {
	io.WriteString(h, ":")
	io.WriteString(h, hash.Base16())
	io.WriteString(h, ":")
	io.WriteString(h, dir)
	io.WriteString(h, ":")
	io.WriteString(h, name)
	fingerprintHash := h.Sum(nil)
	compressed := make([]byte, 20)
	nix.CompressHash(compressed, fingerprintHash)
	return nixbase32.EncodeToString(compressed)
}
