// Copyright 2026 The zb Authors
// SPDX-License-Identifier: MIT

package backend

import (
	"crypto/ed25519"
	"errors"
	"slices"

	"zb.256lights.llc/pkg/zbstore"
)

// A Keyring is a set of private keys to use for signing.
// Nil or the zero value is an empty set of keys.
type Keyring struct {
	Ed25519 []ed25519.PrivateKey
}

// Clone returns a new keyring with contents identical to k.
// If k is nil, then Clone returns nil.
func (k *Keyring) Clone() *Keyring {
	if k == nil {
		return nil
	}
	k2 := new(Keyring)
	if len(k.Ed25519) > 0 {
		k2.Ed25519 = make([]ed25519.PrivateKey, len(k.Ed25519))
		for i, key := range k.Ed25519 {
			k2.Ed25519[i] = slices.Clone(key)
		}
	}
	return k2
}

// Sign creates signatures for the realization using all the private keys in the keyring.
func (k *Keyring) Sign(ref zbstore.RealizationOutputReference, r *zbstore.Realization) ([]*zbstore.RealizationSignature, error) {
	n := 0
	if k != nil {
		n = len(k.Ed25519)
	}
	if n == 0 {
		return nil, nil
	}

	result := make([]*zbstore.RealizationSignature, 0, n)
	var returnedError error
	for _, key := range k.Ed25519 {
		sig, err := zbstore.SignRealizationWithEd25519(ref, r, key)
		if err != nil {
			returnedError = errors.Join(returnedError, err)
			continue
		}
		result = append(result, sig)
	}
	return result, returnedError
}
