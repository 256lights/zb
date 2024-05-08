/* Copyright (C) 2002-2013 Mark Adler, all rights reserved
 * Copyright (C) 2021 Jeremiah Orians
 * This file is part of mescc-tools-extra
 *
 * mescc-tools-extra is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * mescc-tools-extra is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with mescc-tools-extra.  If not, see <http://www.gnu.org/licenses/>.
 */

/* puff.c
 * Copyright (C) 2002-2013 Mark Adler, all rights reserved
 * version 2.3, 21 Jan 2013
 * This software is provided 'as-is', without any express or implied
 * warranty.  In no event will the author be held liable for any damages
 * arising from the use of this software.
 * Permission is granted to anyone to use this software for any purpose,
 * including commercial applications, and to alter it and redistribute it
 * freely, subject to the following restrictions:
 * 1. The origin of this software must not be misrepresented; you must not
 *    claim that you wrote the original software. If you use this software
 *    in a product, an acknowledgment in the product documentation would be
 *    appreciated but is not required.
 * 2. Altered source versions must be plainly marked as such, and must not be
 *    misrepresented as being the original software.
 * 3. This notice may not be removed or altered from any source distribution.
 * Mark Adler    madler@alumni.caltech.edu
 */

/* ungz.c is a gz file decompression utility that leverages puff.c to provide
 * the deflate algorithm with multiple modifications to enable being built by
 * M2-Planet with M2libc.
 *
 *
 * puff.c is a simple inflate written to be an unambiguous way to specify the
 * deflate format.  It is not written for speed but rather simplicity.  As a
 * side benefit, this code might actually be useful when small code is more
 * important than speed, such as bootstrap applications.  For typical deflate
 * data, zlib's inflate() is about four times as fast as puff().  zlib's
 * inflate compiles to around 20K on my machine, whereas puff.c compiles to
 * around 4K on my machine (a PowerPC using GNU cc).  If the faster decode()
 * function here is used, then puff() is only twice as slow as zlib's
 * inflate().
 *
 * All dynamically allocated memory comes from the stack.  The stack required
 * is less than 2K bytes.  This code is compatible with 16-bit int's and
 * assumes that long's are at least 32 bits.  puff.c uses the short data type,
 * assumed to be 16 bits, for arrays in order to conserve memory.  The code
 * works whether integers are stored big endian or little endian.
 *
 * In the comments below are "Format notes" that describe the inflate process
 * and document some of the less obvious aspects of the format.  This source
 * code is meant to supplement RFC 1951, which formally describes the deflate
 * format:
 *
 *    http://www.zlib.org/rfc-deflate.html
 */

/*
 * Change history:
 *
 * 1.0  10 Feb 2002     - First version
 * 1.1  17 Feb 2002     - Clarifications of some comments and notes
 *                      - Update puff() dest and source pointers on negative
 *                        errors to facilitate debugging deflators
 *                      - Remove longest from struct huffman -- not needed
 *                      - Simplify offs[] index in construct()
 *                      - Add input size and checking, using longjmp() to
 *                        maintain easy readability
 *                      - Use short data type for large arrays
 *                      - Use pointers instead of long to specify source and
 *                        destination sizes to avoid arbitrary 4 GB limits
 * 1.2  17 Mar 2002     - Add faster version of decode(), doubles speed (!),
 *                        but leave simple version for readabilty
 *                      - Make sure invalid distances detected if pointers
 *                        are 16 bits
 *                      - Fix fixed codes table error
 *                      - Provide a scanning mode for determining size of
 *                        uncompressed data
 * 1.3  20 Mar 2002     - Go back to lengths for puff() parameters [Gailly]
 *                      - Add a puff.h file for the interface
 *                      - Add braces in puff() for else do [Gailly]
 *                      - Use indexes instead of pointers for readability
 * 1.4  31 Mar 2002     - Simplify construct() code set check
 *                      - Fix some comments
 *                      - Add FIXLCODES #define
 * 1.5   6 Apr 2002     - Minor comment fixes
 * 1.6   7 Aug 2002     - Minor format changes
 * 1.7   3 Mar 2003     - Added test code for distribution
 *                      - Added zlib-like license
 * 1.8   9 Jan 2004     - Added some comments on no distance codes case
 * 1.9  21 Feb 2008     - Fix bug on 16-bit integer architectures [Pohland]
 *                      - Catch missing end-of-block symbol error
 * 2.0  25 Jul 2008     - Add #define to permit distance too far back
 *                      - Add option in TEST code for puff to write the data
 *                      - Add option in TEST code to skip input bytes
 *                      - Allow TEST code to read from piped stdin
 * 2.1   4 Apr 2010     - Avoid variable initialization for happier compilers
 *                      - Avoid unsigned comparisons for even happier compilers
 * 2.2  25 Apr 2010     - Fix bug in variable initializations [Oberhumer]
 *                      - Add const where appropriate [Oberhumer]
 *                      - Split if's and ?'s for coverage testing
 *                      - Break out test code to separate file
 *                      - Move NIL to puff.h
 *                      - Allow incomplete code only if single code length is 1
 *                      - Add full code coverage test to Makefile
 * 2.3  21 Jan 2013     - Check for invalid code length codes in dynamic blocks
 * ??   22 May 2021     - Convert to M2-Planet C subset for bootstrapping purposes.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "M2libc/bootstrappable.h"

/*
 * Maximums for allocations and loops.  It is not useful to change these --
 * they are fixed by the deflate format.
 */
#define MAXBITS 15              /* maximum bits in a code */
#define MAXLCODES 286           /* maximum number of literal/length codes */
#define MAXDCODES 30            /* maximum number of distance codes */
#define MAXCODES 316            /* maximum codes lengths to read (MAXLCODES+MAXDCODES) */
#define FIXLCODES 288           /* number of fixed literal/length codes */

/* input and output state */
struct state {
	/* output state */
	char *out;                  /* output buffer */
	size_t outlen;              /* available space at out */
	size_t outcnt;              /* bytes written to out so far */

	/* input state */
	char *in;                   /* input buffer */
	size_t inlen;               /* available input at in */
	size_t incnt;               /* bytes read so far */
	int bitbuf;                 /* bit buffer */
	int bitcnt;                 /* number of bits in bit buffer */
};

/*
 * Return need bits from the input stream.  This always leaves less than
 * eight bits in the buffer.  bits() works properly for need == 0.
 *
 * Format notes:
 *
 * - Bits are stored in bytes from the least significant bit to the most
 *   significant bit.  Therefore bits are dropped from the bottom of the bit
 *   buffer, using shift right, and new bytes are appended to the top of the
 *   bit buffer, using shift left.
 */
int bits(struct state *s, int need)
{
	long val;           /* bit accumulator (can use up to 20 bits) */
	long hold;

	/* load at least need bits into val */
	val = s->bitbuf;
	while (s->bitcnt < need)
	{
		if (s->incnt == s->inlen)
		{
			fputs("out of input\n", stderr);
			exit(EXIT_FAILURE);
		}
		hold = (s->in[s->incnt] & 0xFF);
		s->incnt = s->incnt + 1;
		val = val | (hold << s->bitcnt);  /* load eight bits */
		s->bitcnt = s->bitcnt + 8;
	}

	/* drop need bits and update buffer, always zero to seven bits left */
	s->bitbuf = (val >> need);
	s->bitcnt = s->bitcnt - need;

	/* return need bits, zeroing the bits above that */
	val = (val & ((1 << need) - 1));
	#if defined(DEBUG)
		fputs(int2str(val, 16, FALSE), stderr);
		fputs(" : bits\n", stderr);
	#endif
	return val;
}

/*
 * Process a stored block.
 *
 * Format notes:
 *
 * - After the two-bit stored block type (00), the stored block length and
 *   stored bytes are byte-aligned for fast copying.  Therefore any leftover
 *   bits in the byte that has the last bit of the type, as many as seven, are
 *   discarded.  The value of the discarded bits are not defined and should not
 *   be checked against any expectation.
 *
 * - The second inverted copy of the stored block length does not have to be
 *   checked, but it's probably a good idea to do so anyway.
 *
 * - A stored block can have zero length.  This is sometimes used to byte-align
 *   subsets of the compressed data for random access or partial recovery.
 */
int stored(struct state *s)
{
	unsigned len;       /* length of stored block */

	/* discard leftover bits from current byte (assumes s->bitcnt < 8) */
	s->bitbuf = 0;
	s->bitcnt = 0;

	/* get length and check against its one's complement */
	if ((s->incnt + 4) > s->inlen) return 2;    /* not enough input */
	len = s->in[s->incnt];
	s->incnt = s->incnt + 1;
	len = len | (s->in[s->incnt] << 8);
	s->incnt = s->incnt + 1;
	if(s->in[s->incnt] != (~len & 0xff)) return -2;                              /* didn't match complement! */
	s->incnt = s->incnt + 1;
	if(s->in[s->incnt] != ((~len >> 8) & 0xff)) return -2;                              /* didn't match complement! */
	s->incnt = s->incnt + 1;

	/* copy len bytes from in to out */
	if ((s->incnt + len) > s->inlen) return 2;                               /* not enough input */
	if (s->out != 0)
	{
		if ((s->outcnt + len) > s->outlen) return 1;                           /* not enough output space */
		while (0 != len)
		{
			len = len - 1;
			s->out[s->outcnt] = s->in[s->incnt];
			s->outcnt = s->outcnt + 1;
			s->incnt = s->incnt + 1;
		}
	}
	else
	{                                      /* just scanning */
		s->outcnt = s->outcnt + len;
		s->incnt = s->incnt + len;
	}

	/* done with a valid stored block */
	return 0;
}

/*
 * Huffman code decoding tables.  count[1..MAXBITS] is the number of symbols of
 * each length, which for a canonical code are stepped through in order.
 * symbol[] are the symbol values in canonical order, where the number of
 * entries is the sum of the counts in count[].  The decoding process can be
 * seen in the function decode() below.
 */
struct huffman
{
	int *count;       /* number of symbols of each length */
	int *symbol;      /* canonically ordered symbols */
};

/*
 * Decode a code from the stream s using huffman table h.  Return the symbol or
 * a negative value if there is an error.  If all of the lengths are zero, i.e.
 * an empty code, or if the code is incomplete and an invalid code is received,
 * then -10 is returned after reading MAXBITS bits.
 *
 * Format notes:
 *
 * - The codes as stored in the compressed data are bit-reversed relative to
 *   a simple integer ordering of codes of the same lengths.  Hence below the
 *   bits are pulled from the compressed data one at a time and used to
 *   build the code value reversed from what is in the stream in order to
 *   permit simple integer comparisons for decoding.  A table-based decoding
 *   scheme (as used in zlib) does not need to do this reversal.
 *
 * - The first code for the shortest length is all zeros.  Subsequent codes of
 *   the same length are simply integer increments of the previous code.  When
 *   moving up a length, a zero bit is appended to the code.  For a complete
 *   code, the last code of the longest length will be all ones.
 *
 * - Incomplete codes are handled by this decoder, since they are permitted
 *   in the deflate format.  See the format notes for fixed() and dynamic().
 */
int decode(struct state *s, struct huffman *h)
{
	int len;            /* current number of bits in code */
	int code = 0;       /* len bits being decoded */
	int first = 0;      /* first code of length len */
	int count;          /* number of codes of length len */
	int index = 0;      /* index of first code of length len in symbol table */
	long hold;

	for (len = 1; len <= MAXBITS; len = len + 1)
	{
		hold = bits(s, 1);              /* get next bit */
		code = code | hold;
		count = h->count[len];
		if ((code - count) < first)
		{
			hold = index + (code - first);
			return h->symbol[hold]; /* if length len, return symbol */
		}
		index = index + count;                 /* else update for next length */
		first = first + count;
		first = first << 1;
		code = code << 1;
	}
	return -10;                         /* ran out of codes */
}

/*
 * Given the list of code lengths length[0..n-1] representing a canonical
 * Huffman code for n symbols, construct the tables required to decode those
 * codes.  Those tables are the number of codes of each length, and the symbols
 * sorted by length, retaining their original order within each length.  The
 * return value is zero for a complete code set, negative for an over-
 * subscribed code set, and positive for an incomplete code set.  The tables
 * can be used if the return value is zero or positive, but they cannot be used
 * if the return value is negative.  If the return value is zero, it is not
 * possible for decode() using that table to return an error--any stream of
 * enough bits will resolve to a symbol.  If the return value is positive, then
 * it is possible for decode() using that table to return an error for received
 * codes past the end of the incomplete lengths.
 *
 * Not used by decode(), but used for error checking, h->count[0] is the number
 * of the n symbols not in the code.  So n - h->count[0] is the number of
 * codes.  This is useful for checking for incomplete codes that have more than
 * one symbol, which is an error in a dynamic block.
 *
 * Assumption: for all i in 0..n-1, 0 <= length[i] <= MAXBITS
 * This is assured by the construction of the length arrays in dynamic() and
 * fixed() and is not verified by construct().
 *
 * Format notes:
 *
 * - Permitted and expected examples of incomplete codes are one of the fixed
 *   codes and any code with a single symbol which in deflate is coded as one
 *   bit instead of zero bits.  See the format notes for fixed() and dynamic().
 *
 * - Within a given code length, the symbols are kept in ascending order for
 *   the code bits definition.
 */
int construct(struct huffman *h, int *length, int n)
{
	int symbol;         /* current symbol when stepping through length[] */
	int len;            /* current length when stepping through h->count[] */
	int left;           /* number of possible codes left of current length */
	int* offs;          /* offsets in symbol table for each length */
	offs = calloc(MAXBITS+1, sizeof(int));
	long hold;

	#if defined(DEBUG)
		int i;
		fputs(int2str(n, 16, FALSE), stderr);
		fputs(" : construct 0\n", stderr);

		for(i = 0; i < n; i = i + 1)
		{
			fputs(int2str(length[i], 16, FALSE), stderr);
			fputs(" : construct 2\n", stderr);
		}
	#endif

	/* count number of codes of each length */
	for (len = 0; len <= MAXBITS; len = len + 1)
	{
		h->count[len] = 0;
	}

	for (symbol = 0; symbol < n; symbol = symbol + 1)
	{
		hold = length[symbol];
		h->count[hold] = h->count[hold] + 1;    /* assumes lengths are within bounds */
	}

	if (h->count[0] == n) return 0;             /* no codes! complete, but decode() will fail */

	/* check for an over-subscribed or incomplete set of lengths */
	left = 1;                                   /* one possible code of zero length */
	for (len = 1; len <= MAXBITS; len = len + 1)
	{
		left = left << 1;                       /* one more bit, double codes left */
		left = left - h->count[len];            /* deduct count from possible codes */
		if (left < 0) return left;              /* over-subscribed--return negative */
	}                                           /* left > 0 means incomplete */

	/* generate offsets into symbol table for each length for sorting */
	offs[1] = 0;
	for (len = 1; len < MAXBITS; len = len + 1)
	{
		offs[len + 1] = offs[len] + h->count[len];
	}

	/*
	 * put symbols in table sorted by length, by symbol order within each
	 * length
	 */
	for (symbol = 0; symbol < n; symbol = symbol + 1)
	{
		if (length[symbol] != 0)
		{
			hold = length[symbol];
			hold = offs[hold];
			h->symbol[hold] = symbol;
			hold = length[symbol];
			offs[hold] = offs[hold] + 1;
		}
	}

	/* return zero for complete set, positive for incomplete set */
	return left;
}

/*
 * Decode literal/length and distance codes until an end-of-block code.
 *
 * Format notes:
 *
 * - Compressed data that is after the block type if fixed or after the code
 *   description if dynamic is a combination of literals and length/distance
 *   pairs terminated by and end-of-block code.  Literals are simply Huffman
 *   coded bytes.  A length/distance pair is a coded length followed by a
 *   coded distance to represent a string that occurs earlier in the
 *   uncompressed data that occurs again at the current location.
 *
 * - Literals, lengths, and the end-of-block code are combined into a single
 *   code of up to 286 symbols.  They are 256 literals (0..255), 29 length
 *   symbols (257..285), and the end-of-block symbol (256).
 *
 * - There are 256 possible lengths (3..258), and so 29 symbols are not enough
 *   to represent all of those.  Lengths 3..10 and 258 are in fact represented
 *   by just a length symbol.  Lengths 11..257 are represented as a symbol and
 *   some number of extra bits that are added as an integer to the base length
 *   of the length symbol.  The number of extra bits is determined by the base
 *   length symbol.  These are in the static arrays below, lens[] for the base
 *   lengths and lext[] for the corresponding number of extra bits.
 *
 * - The reason that 258 gets its own symbol is that the longest length is used
 *   often in highly redundant files.  Note that 258 can also be coded as the
 *   base value 227 plus the maximum extra value of 31.  While a good deflate
 *   should never do this, it is not an error, and should be decoded properly.
 *
 * - If a length is decoded, including its extra bits if any, then it is
 *   followed a distance code.  There are up to 30 distance symbols.  Again
 *   there are many more possible distances (1..32768), so extra bits are added
 *   to a base value represented by the symbol.  The distances 1..4 get their
 *   own symbol, but the rest require extra bits.  The base distances and
 *   corresponding number of extra bits are below in the static arrays dist[]
 *   and dext[].
 *
 * - Literal bytes are simply written to the output.  A length/distance pair is
 *   an instruction to copy previously uncompressed bytes to the output.  The
 *   copy is from distance bytes back in the output stream, copying for length
 *   bytes.
 *
 * - Distances pointing before the beginning of the output data are not
 *   permitted.
 *
 * - Overlapped copies, where the length is greater than the distance, are
 *   allowed and common.  For example, a distance of one and a length of 258
 *   simply copies the last byte 258 times.  A distance of four and a length of
 *   twelve copies the last four bytes three times.  A simple forward copy
 *   ignoring whether the length is greater than the distance or not implements
 *   this correctly.  You should not use memcpy() since its behavior is not
 *   defined for overlapped arrays.  You should not use memmove() or bcopy()
 *   since though their behavior -is- defined for overlapping arrays, it is
 *   defined to do the wrong thing in this case.
 */

int* codes_lens()
{
	/* Size base for length codes 257..285 */
	int* r = calloc(30, sizeof(int));
	r[0] = 3;
	r[1] = 4;
	r[2] = 5;
	r[3] = 6;
	r[4] = 7;
	r[5] = 8;
	r[6] = 9;
	r[7] = 10;
	r[8] = 11;
	r[9] = 13;
	r[10] = 15;
	r[11] = 17;
	r[12] = 19;
	r[13] = 23;
	r[14] = 27;
	r[15] = 31;
	r[16] = 35;
	r[17] = 43;
	r[18] = 51;
	r[19] = 59;
	r[20] = 67;
	r[21] = 83;
	r[22] = 99;
	r[23] = 115;
	r[24] = 131;
	r[25] = 163;
	r[26] = 195;
	r[27] = 227;
	r[28] = 258;
	return r;
}

int* codes_lext()
{
	/* Extra bits for length codes 257..285 */
	int* r = calloc(30, sizeof(int));
	r[0] = 0;
	r[1] = 0;
	r[2] = 0;
	r[3] = 0;
	r[4] = 0;
	r[5] = 0;
	r[6] = 0;
	r[7] = 0;
	r[8] = 1;
	r[9] = 1;
	r[10] = 1;
	r[11] = 1;
	r[12] = 2;
	r[13] = 2;
	r[14] = 2;
	r[15] = 2;
	r[16] = 3;
	r[17] = 3;
	r[18] = 3;
	r[19] = 3;
	r[20] = 4;
	r[21] = 4;
	r[22] = 4;
	r[23] = 4;
	r[24] = 5;
	r[25] = 5;
	r[26] = 5;
	r[27] = 5;
	r[28] = 0;
	return r;
}

int* codes_dists()
{
	/* Offset base for distance codes 0..29 */
	int* r = calloc(31, sizeof(int));
	r[0] = 1;
	r[1] = 2;
	r[2] = 3;
	r[3] = 4;
	r[4] = 5;
	r[5] = 7;
	r[6] = 9;
	r[7] = 13;
	r[8] = 17;
	r[9] = 25;
	r[10] = 33;
	r[11] = 49;
	r[12] = 65;
	r[13] = 97;
	r[14] = 129;
	r[15] = 193;
	r[16] = 257;
	r[17] = 385;
	r[18] = 513;
	r[19] = 769;
	r[20] = 1025;
	r[21] = 1537;
	r[22] = 2049;
	r[23] = 3073;
	r[24] = 4097;
	r[25] = 6145;
	r[26] = 8193;
	r[27] = 12289;
	r[28] = 16385;
	r[29] = 24577;
	return r;
}

int* codes_dext()
{
	/* Extra bits for distance codes 0..29 */
	int* r = calloc(31, sizeof(int));
	r[0] = 0;
	r[1] = 0;
	r[2] = 0;
	r[3] = 0;
	r[4] = 1;
	r[5] = 1;
	r[6] = 2;
	r[7] = 2;
	r[8] = 3;
	r[9] = 3;
	r[10] = 4;
	r[11] = 4;
	r[12] = 5;
	r[13] = 5;
	r[14] = 6;
	r[15] = 6;
	r[16] = 7;
	r[17] = 7;
	r[18] = 8;
	r[19] = 8;
	r[20] = 9;
	r[21] = 9;
	r[22] = 10;
	r[23] = 10;
	r[24] = 11;
	r[25] = 11;
	r[26] = 12;
	r[27] = 12;
	r[28] = 13;
	r[29] = 13;
	return r;
}

int codes(struct state *s, struct huffman *lencode, struct huffman *distcode)
{
	int symbol;         /* decoded symbol */
	int len;            /* length for copy */
	unsigned dist;      /* distance for copy */
	int* lens = codes_lens();
	int* lext = codes_lext();
	int* dists = codes_dists();
	int* dext = codes_dext();

	/* decode literals and length/distance pairs */
	do
	{
		symbol = decode(s, lencode);
		if (symbol < 0) return symbol;          /* invalid symbol */
		if (symbol < 256)                       /* literal: symbol is the byte */
		{
			/* write out the literal */
			if (s->out != 0)
			{
				if (s->outcnt == s->outlen) return 1;
				s->out[s->outcnt] = symbol;
			}
			s->outcnt = s->outcnt + 1;
		}
		else if (symbol > 256)                  /* length */
		{
			/* get and compute length */
			symbol = symbol - 257;
			if (symbol >= 29) return -10;       /* invalid fixed code */
			len = lens[symbol] + bits(s, lext[symbol]);

			/* get and check distance */
			symbol = decode(s, distcode);
			if (symbol < 0) return symbol;      /* invalid symbol */
			dist = dists[symbol] + bits(s, dext[symbol]);
			if (dist > s->outcnt) return -11;   /* distance too far back */

			/* copy length bytes from distance bytes back */
			if (s->out != 0)
			{
				if (s->outcnt + len > s->outlen) return 1;
				while (0 != len)
				{
					len = len - 1;
					if(dist > s->outcnt) s->out[s->outcnt] = 0;
					else s->out[s->outcnt] = s->out[s->outcnt - dist];
					s->outcnt = s->outcnt + 1;
				}
			}
			else s->outcnt = s->outcnt + len;
		}
	} while (symbol != 256);            /* end of block symbol */

	/* done with a valid fixed or dynamic block */
	return 0;
}

/*
 * Process a fixed codes block.
 *
 * Format notes:
 *
 * - This block type can be useful for compressing small amounts of data for
 *   which the size of the code descriptions in a dynamic block exceeds the
 *   benefit of custom codes for that block.  For fixed codes, no bits are
 *   spent on code descriptions.  Instead the code lengths for literal/length
 *   codes and distance codes are fixed.  The specific lengths for each symbol
 *   can be seen in the "for" loops below.
 *
 * - The literal/length code is complete, but has two symbols that are invalid
 *   and should result in an error if received.  This cannot be implemented
 *   simply as an incomplete code since those two symbols are in the "middle"
 *   of the code.  They are eight bits long and the longest literal/length\
 *   code is nine bits.  Therefore the code must be constructed with those
 *   symbols, and the invalid symbols must be detected after decoding.
 *
 * - The fixed distance codes also have two invalid symbols that should result
 *   in an error if received.  Since all of the distance codes are the same
 *   length, this can be implemented as an incomplete code.  Then the invalid
 *   codes are detected while decoding.
 */
int fixed(struct state *s)
{
	int* lencnt = calloc((MAXBITS + 1), sizeof(int));
	int* lensym = calloc(FIXLCODES, sizeof(int));
	int* distcnt = calloc((MAXBITS + 1), sizeof(int));
	int* distsym = calloc(MAXDCODES, sizeof(int));
	struct huffman* lencode = calloc(1, sizeof(struct huffman));
	struct huffman* distcode = calloc(1, sizeof(struct huffman));
	int hold;

	/* build fixed huffman tables if first call (may not be thread safe) */
	int symbol;
	int* lengths = calloc(FIXLCODES, sizeof(int));

	/* construct lencode and distcode */
	lencode->count = lencnt;
	lencode->symbol = lensym;
	distcode->count = distcnt;
	distcode->symbol = distsym;

	/* literal/length table */
	for (symbol = 0; symbol < 144; symbol = symbol + 1)
	{
		lengths[symbol] = 8;
	}

	while(symbol < 256)
	{
		lengths[symbol] = 9;
		symbol = symbol + 1;
	}

	while(symbol < 280)
	{
		lengths[symbol] = 7;
		symbol = symbol + 1;
	}

	while(symbol < FIXLCODES)
	{
		lengths[symbol] = 8;
		symbol = symbol + 1;
	}

	construct(lencode, lengths, FIXLCODES);

	/* distance table */
	for (symbol = 0; symbol < MAXDCODES; symbol = symbol + 1)
	{
		lengths[symbol] = 5;
	}

	construct(distcode, lengths, MAXDCODES);

	/* decode data until end-of-block code */
	hold = codes(s, lencode, distcode);
	return hold;
}

/*
 * Process a dynamic codes block.
 *
 * Format notes:
 *
 * - A dynamic block starts with a description of the literal/length and
 *   distance codes for that block.  New dynamic blocks allow the compressor to
 *   rapidly adapt to changing data with new codes optimized for that data.
 *
 * - The codes used by the deflate format are "canonical", which means that
 *   the actual bits of the codes are generated in an unambiguous way simply
 *   from the number of bits in each code.  Therefore the code descriptions
 *   are simply a list of code lengths for each symbol.
 *
 * - The code lengths are stored in order for the symbols, so lengths are
 *   provided for each of the literal/length symbols, and for each of the
 *   distance symbols.
 *
 * - If a symbol is not used in the block, this is represented by a zero as
 *   as the code length.  This does not mean a zero-length code, but rather
 *   that no code should be created for this symbol.  There is no way in the
 *   deflate format to represent a zero-length code.
 *
 * - The maximum number of bits in a code is 15, so the possible lengths for
 *   any code are 1..15.
 *
 * - The fact that a length of zero is not permitted for a code has an
 *   interesting consequence.  Normally if only one symbol is used for a given
 *   code, then in fact that code could be represented with zero bits.  However
 *   in deflate, that code has to be at least one bit.  So for example, if
 *   only a single distance base symbol appears in a block, then it will be
 *   represented by a single code of length one, in particular one 0 bit.  This
 *   is an incomplete code, since if a 1 bit is received, it has no meaning,
 *   and should result in an error.  So incomplete distance codes of one symbol
 *   should be permitted, and the receipt of invalid codes should be handled.
 *
 * - It is also possible to have a single literal/length code, but that code
 *   must be the end-of-block code, since every dynamic block has one.  This
 *   is not the most efficient way to create an empty block (an empty fixed
 *   block is fewer bits), but it is allowed by the format.  So incomplete
 *   literal/length codes of one symbol should also be permitted.
 *
 * - If there are only literal codes and no lengths, then there are no distance
 *   codes.  This is represented by one distance code with zero bits.
 *
 * - The list of up to 286 length/literal lengths and up to 30 distance lengths
 *   are themselves compressed using Huffman codes and run-length encoding.  In
 *   the list of code lengths, a 0 symbol means no code, a 1..15 symbol means
 *   that length, and the symbols 16, 17, and 18 are run-length instructions.
 *   Each of 16, 17, and 18 are follwed by extra bits to define the length of
 *   the run.  16 copies the last length 3 to 6 times.  17 represents 3 to 10
 *   zero lengths, and 18 represents 11 to 138 zero lengths.  Unused symbols
 *   are common, hence the special coding for zero lengths.
 *
 * - The symbols for 0..18 are Huffman coded, and so that code must be
 *   described first.  This is simply a sequence of up to 19 three-bit values
 *   representing no code (0) or the code length for that symbol (1..7).
 *
 * - A dynamic block starts with three fixed-size counts from which is computed
 *   the number of literal/length code lengths, the number of distance code
 *   lengths, and the number of code length code lengths (ok, you come up with
 *   a better name!) in the code descriptions.  For the literal/length and
 *   distance codes, lengths after those provided are considered zero, i.e. no
 *   code.  The code length code lengths are received in a permuted order (see
 *   the order[] array below) to make a short code length code length list more
 *   likely.  As it turns out, very short and very long codes are less likely
 *   to be seen in a dynamic code description, hence what may appear initially
 *   to be a peculiar ordering.
 *
 * - Given the number of literal/length code lengths (nlen) and distance code
 *   lengths (ndist), then they are treated as one long list of nlen + ndist
 *   code lengths.  Therefore run-length coding can and often does cross the
 *   boundary between the two sets of lengths.
 *
 * - So to summarize, the code description at the start of a dynamic block is
 *   three counts for the number of code lengths for the literal/length codes,
 *   the distance codes, and the code length codes.  This is followed by the
 *   code length code lengths, three bits each.  This is used to construct the
 *   code length code which is used to read the remainder of the lengths.  Then
 *   the literal/length code lengths and distance lengths are read as a single
 *   set of lengths using the code length codes.  Codes are constructed from
 *   the resulting two sets of lengths, and then finally you can start
 *   decoding actual compressed data in the block.
 *
 * - For reference, a "typical" size for the code description in a dynamic
 *   block is around 80 bytes.
 */

int* dynamic_order()
{
	/* permutation of code length codes */
	int* r = calloc(20, sizeof(int));
	r[0] = 16;
	r[1] = 17;
	r[2] = 18;
	r[3] = 0;
	r[4] = 8;
	r[5] = 7;
	r[6] = 9;
	r[7] = 6;
	r[8] = 10;
	r[9] = 5;
	r[10] = 11;
	r[11] = 4;
	r[12] = 12;
	r[13] = 3;
	r[14] = 13;
	r[15] = 2;
	r[16] = 14;
	r[17] = 1;
	r[18] = 15;
	return r;
}

int dynamic(struct state *s)
{
	#if defined(__M2__)
		int array = sizeof(int);
	#else
		int array = 1;
	#endif

	int nlen;
	int ndist;
	int ncode;                          /* number of lengths in descriptor */
	int index;                          /* index of lengths[] */
	int err;                            /* construct() return value */
	int* lengths = calloc(MAXCODES, sizeof(int));       /* descriptor code lengths */
	int* lencnt = calloc((MAXBITS + 1), sizeof(int));
	int* lensym = calloc(MAXLCODES, sizeof(int));       /* lencode memory */
	int* distcnt = calloc((MAXBITS + 1), sizeof(int));
	int* distsym = calloc(MAXDCODES, sizeof(int));      /* distcode memory */
	struct huffman* lencode = calloc(1, sizeof(struct huffman));
	struct huffman* distcode = calloc(1, sizeof(struct huffman));
	int* order = dynamic_order();
	long hold;
	int* set;

	/* construct lencode and distcode */
	lencode->count = lencnt;
	lencode->symbol = lensym;
	distcode->count = distcnt;
	distcode->symbol = distsym;

	/* get number of lengths in each table, check lengths */
	nlen = bits(s, 5) + 257;
	ndist = bits(s, 5) + 1;
	ncode = bits(s, 4) + 4;
	if (nlen > MAXLCODES) return -3;    /* bad counts */
	if(ndist > MAXDCODES) return -3;    /* bad counts */

	/* read code length code lengths (really), missing lengths are zero */
	for (index = 0; index < ncode; index = index + 1)
	{
		hold = order[index];
		lengths[hold] = bits(s, 3);
	}

	while(index < 19)
	{
		hold = order[index];
		lengths[hold] = 0;
		index = index + 1;
	}

	/* build huffman table for code lengths codes (use lencode temporarily) */
	err = construct(lencode, lengths, 19);
	if (err != 0) return -4;            /* require complete code set here */

	/* read length/literal and distance code length tables */
	index = 0;
	int symbol;                         /* decoded value */
	int len;                            /* last length to repeat */
	while (index < (nlen + ndist))
	{
		symbol = decode(s, lencode);
		if (symbol < 0) return symbol;  /* invalid symbol */

		if (symbol < 16)                /* length in 0..15 */
		{
			lengths[index] = symbol;
			index = index + 1;
		}
		else                            /* repeat instruction */
		{
			len = 0;                    /* assume repeating zeros */
			if (symbol == 16)           /* repeat last length 3..6 times */
			{
				if (index == 0) return -5;      /* no last length! */
				len = lengths[index - 1];       /* last length */
				symbol = 3 + bits(s, 2);
			}
			else if (symbol == 17) symbol = 3 + bits(s, 3); /* repeat zero 3..10 times */
			else symbol = 11 + bits(s, 7);      /* == 18, repeat zero 11..138 times */

			if ((index + symbol) > (nlen + ndist)) return -6;   /* too many lengths! */

			while(0 != symbol)            /* repeat last or zero symbol times */
			{
				lengths[index] = len;
				index = index + 1;
				symbol = symbol - 1;
			}
		}
	}

	/* check for end-of-block code -- there better be one! */
	if (lengths[256] == 0) return -9;

	/* build huffman table for literal/length codes */
	err = construct(lencode, lengths, nlen);

	/* incomplete code ok only for single length 1 code */
	if (err < 0) return -7;
	if((0 != err) && (nlen != (lencode->count[0] + lencode->count[1]))) return -7;

	/* build huffman table for distance codes */
	set = lengths + (nlen * array);
	err = construct(distcode, set, ndist);

	/* incomplete code ok only for single length 1 code */
	if (err < 0) return -8;
	if((0 != err) && (ndist != (distcode->count[0] + distcode->count[1]))) return -8;

	/* decode data until end-of-block code */
	hold = codes(s, lencode, distcode);
	return hold;
}

/*
 * Inflate source to dest.  On return, destlen and sourcelen are updated to the
 * size of the uncompressed data and the size of the deflate data respectively.
 * On success, the return value of puff() is zero.  If there is an error in the
 * source data, i.e. it is not in the deflate format, then a negative value is
 * returned.  If there is not enough input available or there is not enough
 * output space, then a positive error is returned.  In that case, destlen and
 * sourcelen are not updated to facilitate retrying from the beginning with the
 * provision of more input data or more output space.  In the case of invalid
 * inflate data (a negative error), the dest and source pointers are updated to
 * facilitate the debugging of deflators.
 *
 * puff() also has a mode to determine the size of the uncompressed output with
 * no output written.  For this dest must be (unsigned char *)0.  In this case,
 * the input value of *destlen is ignored, and on return *destlen is set to the
 * size of the uncompressed output.
 *
 * The return codes are:
 *
 *   2:  available inflate data did not terminate
 *   1:  output space exhausted before completing inflate
 *   0:  successful inflate
 *  -1:  invalid block type (type == 3)
 *  -2:  stored block length did not match one's complement
 *  -3:  dynamic block code description: too many length or distance codes
 *  -4:  dynamic block code description: code lengths codes incomplete
 *  -5:  dynamic block code description: repeat lengths with no first length
 *  -6:  dynamic block code description: repeat more than specified lengths
 *  -7:  dynamic block code description: invalid literal/length code lengths
 *  -8:  dynamic block code description: invalid distance code lengths
 *  -9:  dynamic block code description: missing end-of-block code
 * -10:  invalid literal/length or distance code in fixed or dynamic block
 * -11:  distance is too far back in fixed or dynamic block
 *
 * Format notes:
 *
 * - Three bits are read for each block to determine the kind of block and
 *   whether or not it is the last block.  Then the block is decoded and the
 *   process repeated if it was not the last block.
 *
 * - The leftover bits in the last byte of the deflate data after the last
 *   block (if it was a fixed or dynamic block) are undefined and have no
 *   expected values to check.
 */

struct puffer
{
	int error;
	size_t destlen;
	size_t sourcelen;
};

struct puffer* puff(char* dest, size_t destlen, char* source, size_t sourcelen)
{
	struct state* s = calloc(1, sizeof(struct state));             /* input/output state */
	int last;
	int type;                   /* block information */
	int err;                    /* return value */

	/* initialize output state */
	s->out = dest;
	s->outlen = destlen;                /* ignored if dest is NIL */
	s->outcnt = 0;

	/* initialize input state */
	s->in = source;
	s->inlen = sourcelen;
	s->incnt = 0;
	s->bitbuf = 0;
	s->bitcnt = 0;

	/* process blocks until last block or error */
	do
	{
		last = bits(s, 1);         /* one if last block */
		type = bits(s, 2);         /* block type 0..3 */

		if(0 == type)
		{
			err = stored(s);
		}
		else if(1 == type)
		{
			err = fixed(s);
		}
		else if(2 == type)
		{
			err = dynamic(s);
		}
		else err = -1;

		if (err != 0) break;                  /* return with error */
	} while (!last);

	/* update the lengths and return */
	struct puffer* r = calloc(1, sizeof(struct puffer));
	r->error = err;
	r->destlen = s->outcnt;
	r->sourcelen = s->incnt;
	return r;
}

void write_blob(char* s, int start, int len, FILE* f)
{
	char* table = "0123456789ABCDEF";
	if(start > len) return;

	int i = s[start] & 0xFF;
	fputc(table[(i >> 4)], f);
	fputc(table[(i & 0xF)], f);
	fputc(' ', f);

	if(start == len) fputc('\n', f);
	else fputc(' ', f);
	write_blob(s, start + 1, len, f);
}

#define FTEXT 0x01
#define FHCRC 0x02
#define FEXTRA 0x04
#define FNAME 0x08
#define FCOMMENT 0x10


struct gz
{
	char* HEADER;
	int ID;
	int CM;
	int FLG;
	int MTIME;
	int XFL;
	int OS;
	int XLEN;
	char* FLG_FEXTRA;
	char* FLG_FNAME;
	char* FLG_FCOMMENT;
	int CRC16;
	char* FLG_FHCRC;
	char* block;
	int CRC32;
	size_t ISIZE;
	size_t file_size;
};

/* Read the input file *name, or stdin if name is NULL, into allocated memory.
   Reallocate to larger buffers until the entire file is read in.  Return a
   pointer to the allocated data, or NULL if there was a memory allocation
   failure.  *len is the number of bytes of data read from the input file (even
   if load() returns NULL).  If the input file was empty or could not be opened
   or read, *len is zero. */
struct gz* load(char* name)
{
	struct gz* r = calloc(1, sizeof(struct gz));
	char* scratch = calloc(5, sizeof(char));
	FILE* f = fopen(name, "r");
	int count;
	int ID1;
	int ID2;
	int count1;
	int count2;
	int count3;
	int count4;
	int c;
	int i;
	char* s = calloc(11, sizeof(char));

	if(NULL == f)
	{
		fputs("unable to open file: ", stderr);
		fputs(name, stderr);
		fputs("\nfor reading\n", stderr);
		return NULL;
	}

	fseek(f, 0, SEEK_END);
	r->file_size = ftell(f);
	fseek(f, 0, SEEK_SET);
	count = fread(s, sizeof(char), 10, f);

	if(10 != count)
	{
		fputs("incomplete gzip header\n", stderr);
		return NULL;
	}

	/* Verify header */
	r->HEADER = s;

	#if defined(DEBUG)
		write_blob(s, 0, 10, stderr);
	#endif

	ID1 = (s[0] & 0xFF);
	ID2 = (s[1] & 0xFF);
	r->ID = ((ID1 << 8) | ID2);
	if(0x1f8b != r->ID)
	{
		fputs("bad header\n", stderr);
		return NULL;
	}

	/* Verify Compression */
	r->CM = (r->HEADER[2] & 0xFF);
	if(8 != r->CM)
	{
		fputs("NOT DEFLATE COMPRESSION\n", stderr);
		return NULL;
	}

	/* Get specials specified in flag bits */
	r->FLG = (r->HEADER[3] & 0xFF);

	if(0 != (FEXTRA & r->FLG))
	{
		count = fread(scratch, sizeof(char), 4, f);
		count1 = (scratch[0] & 0xFF);
		count2 = (scratch[1] & 0xFF);
		count3 = (scratch[2] & 0xFF);
		count4 = (scratch[3] & 0xFF);
		count = (count1 << 24) | (count2 << 16) | (count3 << 8) | count4;
		require(0 < count, "FEXTRA field needs to be a positive number of bytes in size\n");
		require(100000000 > count, "we don't support FEXTRA fields greater than 100MB in size\n");
		r->FLG_FEXTRA = calloc(count + 1, sizeof(char));
		fread(r->FLG_FEXTRA, sizeof(char), count, f);
	}

	if(0 != (FNAME & r->FLG))
	{
		r->FLG_FNAME = calloc(r->file_size, sizeof(char));
		i = 0;
		do
		{
			c = fgetc(f);
			require(0 <= c, "received a non-null terminated filename in the file\n");
			r->FLG_FNAME[i] = c;
			i = i + 1;
		} while(0 != c);
	}

	if(0 != (FCOMMENT & r->FLG))
	{
		r->FLG_FCOMMENT = calloc(r->file_size, sizeof(char));
		i = 0;
		do
		{
			c = fgetc(f);
			require(0 <= c, "received a non-null terminated comment in the file\n");
			r->FLG_FCOMMENT[i] = c;
			i = i + 1;
		} while(0 != c);
	}

	if(0 != (FHCRC & r->FLG))
	{
		/* Not implemented */
		fputs("FHCRC is not implemented at this time\n", stderr);
		return NULL;
	}

	if(NULL == r->FLG_FNAME)
	{
		count = strlen(name) - 3;
		r->FLG_FNAME = calloc(count + 4, sizeof(char));
		i = 0;
		while(i < count)
		{
			r->FLG_FNAME[i] = name[i];
			i = i + 1;
		}
	}

	r->block = calloc(r->file_size, sizeof(char));
	count = fread(r->block, sizeof(char), r->file_size, f);
	r->ISIZE = count;
	fclose(f);
	return r;
}

int main(int argc, char **argv)
{
	struct puffer* ret;
	char* name;
	char* buffer;
	char *dest;
	struct gz* in;
	FILE* out;
	int FUZZING = FALSE;

	/* process arguments */
	int i = 1;
	while (i < argc)
	{
		if(NULL == argv[i])
		{
			i = i + 1;
		}
		else if(match(argv[i], "-f") || match(argv[i], "--file"))
		{
			name = argv[i+1];
			require(NULL != name, "the --file option requires a filename to be given\n");
			i = i + 2;
		}
		else if(match(argv[i], "-o") || match(argv[i], "--output"))
		{
			dest = argv[i+1];
			require(NULL != dest, "the --output option requires a filename to be given\n");
			i = i + 2;
		}
		else if(match(argv[i], "--chaos") || match(argv[i], "--fuzz-mode") || match(argv[i], "--fuzzing"))
		{
			FUZZING = TRUE;
			fputs("fuzz-mode enabled, preparing for chaos\n", stderr);
			i = i + 1;
		}
		else if(match(argv[i], "-h") || match(argv[i], "--help"))
		{
			fputs("Usage: ", stderr);
			fputs(argv[0], stderr);
			fputs(" --file $input.gz", stderr);
			fputs(" [--output $output] (or it'll use the internal filename)\n", stderr);
			fputs("--help to get this message\n", stderr);
			fputs("--fuzz-mode if you wish to fuzz this application safely\n", stderr);
			exit(EXIT_SUCCESS);
		}
		else
		{
			fputs("Unknown option:", stderr);
			fputs(argv[i], stderr);
			fputs("\nAborting to avoid problems\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	in = load(name);

	if (in == NULL)
	{
		fputs("memory allocation failure\nDidn't read file\n", stderr);
		exit(1);
	}

	ret = puff(0, 0, in->block, in->ISIZE);

	if(NULL == dest)
	{
		dest = in->FLG_FNAME;
	}

	fputs(name, stderr);
	fputs(" => ", stderr);
	fputs(dest, stderr);

	if (0 != ret->error)
	{
		fputs("puff() failed with return code ", stderr);
		fputs(int2str(ret->error, 10, TRUE), stderr);
		fputc('\n', stderr);
		exit(3);
	}
	else
	{
		fputs(": succeeded uncompressing ", stderr);
		fputs(int2str(ret->destlen, 10, FALSE), stderr);
		fputs(" bytes\n", stderr);
	}

	buffer = malloc(ret->destlen);
	if (buffer == NULL)
	{
		fputs("memory allocation failure\n", stderr);
		return 4;
	}

	ret = puff(buffer, ret->destlen, in->block, in->ISIZE);

	if(!FUZZING)
	{
		out = fopen(dest, "w");
		fwrite(buffer, 1, ret->destlen, out);
	}
	else
	{
		fputs("skipped write to file due to --fuzz-mode flag\n", stderr);
	}
	free(buffer);

	/* clean up */
	return 0;
}
