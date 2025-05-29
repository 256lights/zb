// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

// Package uuid8 provides a function to generate version 8 UUIDs
// as specified in [RFC 9562].
//
// [RFC 9562]: https://datatracker.ietf.org/doc/html/rfc9562#section-5.8
package uuid8

import "github.com/google/uuid"

// FromBytes returns a Version 8 UUID constructed from the given bytes.
// If b contains more than 122 bits, then the bits are cyclically XORed together
// to form the final UUID.
// If b contains less than 122 bits, then the bits are zero-extended from the end
// to fill the UUID.
func FromBytes(b []byte) uuid.UUID {
	var result uuid.UUID
	result[6] = 0x80        // Version 8
	result[8] = 0b10_000000 // RFC 9562 variant

	iter := bitIterator{bits: b}
	for i := 0; len(iter.bits) > 0; {
		switch i {
		case 6:
			result[i] ^= iter.next(4)
		case 8:
			result[i] ^= iter.next(6)
		default:
			result[i] ^= iter.next(8)
		}
		i++
		if i >= len(result) {
			i -= len(result)
		}
	}
	return result
}

type bitIterator struct {
	bits   []byte
	offset uint8
}

func (iter *bitIterator) next(n uint8) byte {
	switch {
	case n > 8:
		panic("too many bits in one call")
	case len(iter.bits) == 0:
		return 0
	default:
		b := (iter.bits[0] << iter.offset) >> (8 - n)
		iter.offset += n
		if iter.offset >= 8 {
			iter.offset -= 8
			iter.bits = iter.bits[1:]
			if len(iter.bits) > 0 {
				b |= iter.bits[0] >> (8 - iter.offset)
			}
		}
		return b
	}
}
