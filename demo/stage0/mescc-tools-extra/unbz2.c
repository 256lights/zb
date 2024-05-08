/* Copyright (C) 2003, 2007 Rob Landley <rob@landley.net>
 * Copyright (C) 2022 Paul Dersey <pdersey@gmail.com>
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

/* bzcat.c - bzip2 decompression
 *
 * Copyright 2003, 2007 Rob Landley <rob@landley.net>
 *
 * Based on a close reading (but not the actual code) of the original bzip2
 * decompression code by Julian R Seward (jseward@acm.org), which also
 * acknowledges contributions by Mike Burrows, David Wheeler, Peter Fenwick,
 * Alistair Moffat, Radford Neal, Ian H. Witten, Robert Sedgewick, and
 * Jon L. Bentley.
 *
 * No standard.
*/

/********************************************************************************
 * unbz2.c is a bz2 file decompression utility based on bzcat.c with            *
 * modifications to enable being built by M2-Planet with M2libc.                *
 ********************************************************************************/

#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <unistd.h>
#include <fcntl.h>
#include "M2libc/bootstrappable.h"

// Constants for huffman coding
#define MAX_GROUPS               6
#define GROUP_SIZE               50     /* 64 would have been more efficient */
#define MAX_HUFCODE_BITS         20     /* Longest huffman code allowed */
#define MAX_SYMBOLS              258    /* 256 literals + RUNA + RUNB */
#define SYMBOL_RUNA              0
#define SYMBOL_RUNB              1

// Other housekeeping constants
#define IOBUF_SIZE               4096

// Status return values
#define RETVAL_LAST_BLOCK        (-100)
#define RETVAL_NOT_BZIP_DATA     (-1)
#define RETVAL_DATA_ERROR        (-2)
#define RETVAL_OBSOLETE_INPUT    (-3)

#define INT_MAX 2147483647

// This is what we know about each huffman coding group
struct group_data
{
	int *limit;
	int *base;
	int *permute;
	char minLen;
	char maxLen;
};

// Data for burrows wheeler transform

struct bwdata
{
	unsigned origPtr;
	int *byteCount;
	// State saved when interrupting output
	int writePos;
	int writeRun;
	int writeCount;
	int writeCurrent;
	unsigned dataCRC;
	unsigned headerCRC;
	unsigned *dbuf;
};

// Structure holding all the housekeeping data, including IO buffers and
// memory that persists between calls to bunzip
struct bunzip_data
{
	// Input stream, input buffer, input bit buffer
	int in_fd;
	int inbufCount;
	int inbufPos;
	char *inbuf;
	unsigned inbufBitCount;
	unsigned inbufBits;

	// Output buffer
	char *outbuf;
	int outbufPos;

	unsigned totalCRC;

	// First pass decompression data (Huffman and MTF decoding)
	char *selectors;                  // nSelectors=15 bits
	struct group_data *groups;   // huffman coding tables
	int symTotal;
	int groupCount;
	int nSelectors;
	unsigned *symToByte;
	unsigned *mtfSymbol;

	// The CRC values stored in the block header and calculated from the data
	unsigned *crc32Table;

	// Second pass decompression data (burrows-wheeler transform)
	unsigned dbufSize;
	struct bwdata* bwdata;
};

int FUZZING;


void crc_init(unsigned *crc_table, int little_endian)
{
	unsigned i;
	unsigned j;
	unsigned c;

	// Init the CRC32 table (big endian)
	for(i = 0; i < 256; i += 1)
	{
		if(little_endian)
		{
			c = i;
		}
		else
		{
			c = i << 24;
		}

		for(j = 8; j > 0; j -= 1)
		{
			if(little_endian)
			{
				if(c & 1)
				{
					c = (c >> 1) ^ 0xEDB88320;
				}
				else
				{
					c = c >> 1;
				}
			}
			else
			{
				if(c & 0x80000000)
				{
					c = (c << 1) ^ 0x04C11DB7;
#if defined(__M2__)

					// & 0xFFFFFFFF not working
					if(sizeof(unsigned) == 8)
					{
						c <<= 32;
						c >>= 32;
					}

#endif
				}
				else
				{
					c = c << 1;
				}
			}
		}

		crc_table[i] = c;
	}
}

// Return the next nnn bits of input.  All reads from the compressed input
// are done through this function.  All reads are big endian.
unsigned get_bits(struct bunzip_data *bd, char bits_wanted)
{
	unsigned bits = 0;

	// If we need to get more data from the byte buffer, do so.  (Loop getting
	// one byte at a time to enforce endianness and avoid unaligned access.)
	while(bd->inbufBitCount < bits_wanted)
	{
		// If we need to read more data from file into byte buffer, do so
		if(bd->inbufPos == bd->inbufCount)
		{
			if(0 >= (bd->inbufCount = read(bd->in_fd, bd->inbuf, IOBUF_SIZE)))
			{
				exit(1);
			}

			bd->inbufPos = 0;
		}

		// Avoid 32-bit overflow (dump bit buffer to top of output)
		if(bd->inbufBitCount >= 24)
		{
			bits = bd->inbufBits & ((1 << bd->inbufBitCount) - 1);
			bits_wanted = bits_wanted - bd->inbufBitCount;
			bits = bits << bits_wanted;
			bd->inbufBitCount = 0;
		}

		// Grab next 8 bits of input from buffer.
		bd->inbufBits = (bd->inbufBits << 8) | (bd->inbuf[bd->inbufPos] & 0xFF);
		bd->inbufPos = bd->inbufPos + 1;
		bd->inbufBitCount = bd->inbufBitCount + 8;
	}

	// Calculate result
	bd->inbufBitCount = bd->inbufBitCount - bits_wanted;
	bits = bits | ((bd->inbufBits >> bd->inbufBitCount) & ((1 << bits_wanted) - 1));
	return bits;
}

/* Read block header at start of a new compressed data block.  Consists of:
 *
 * 48 bits : Block signature, either pi (data block) or e (EOF block).
 * 32 bits : bw->headerCRC
 * 1  bit  : obsolete feature flag.
 * 24 bits : origPtr (Burrows-wheeler unwind index, only 20 bits ever used)
 * 16 bits : Mapping table index.
 *[16 bits]: symToByte[symTotal] (Mapping table.  For each bit set in mapping
 *           table index above, read another 16 bits of mapping table data.
 *           If correspondig bit is unset, all bits in that mapping table
 *           section are 0.)
 *  3 bits : groupCount (how many huffman tables used to encode, anywhere
 *           from 2 to MAX_GROUPS)
 * variable: hufGroup[groupCount] (MTF encoded huffman table data.)
 */

int read_block_header(struct bunzip_data *bd, struct bwdata *bw)
{
	struct group_data *hufGroup;
	int hh;
	int ii;
	int jj;
	int kk;
	int symCount;
	int *base;
	int *limit;
	unsigned uc;
	unsigned *length = calloc(MAX_SYMBOLS, sizeof(unsigned));
	unsigned *temp = calloc(MAX_HUFCODE_BITS + 1, sizeof(unsigned));
	size_t minLen;
	size_t maxLen;
	int pp;
#if defined(__M2__)
	int int_array = sizeof(int);
	int group_data_array = sizeof(struct group_data);
#else
	int int_array = 1;
	int group_data_array = 1;
#endif
	size_t hold;
	// Read in header signature and CRC (which is stored big endian)
	ii = get_bits(bd, 24);
	jj = get_bits(bd, 24);
	bw->headerCRC = get_bits(bd, 32);

	// Is this the EOF block with CRC for whole file?  (Constant is "e")
	if(ii == 0x177245 && jj == 0x385090)
	{
		free(length);
		free(temp);
		return RETVAL_LAST_BLOCK;
	}

	// Is this a valid data block?  (Constant is "pi".)
	if(ii != 0x314159 || jj != 0x265359)
	{
		return RETVAL_NOT_BZIP_DATA;
	}

	// We can add support for blockRandomised if anybody complains.
	if(get_bits(bd, 1))
	{
		return RETVAL_OBSOLETE_INPUT;
	}

	if((bw->origPtr = get_bits(bd, 24)) > bd->dbufSize)
	{
		return RETVAL_DATA_ERROR;
	}

	// mapping table: if some byte values are never used (encoding things
	// like ascii text), the compression code removes the gaps to have fewer
	// symbols to deal with, and writes a sparse bitfield indicating which
	// values were present.  We make a translation table to convert the symbols
	// back to the corresponding bytes.
	hh = get_bits(bd, 16);
	bd->symTotal = 0;

	for(ii = 0; ii < 16; ii += 1)
	{
		if(hh & (1 << (15 - ii)))
		{
			kk = get_bits(bd, 16);

			for(jj = 0; jj < 16; jj += 1)
			{
				if(kk & (1 << (15 - jj)))
				{
					bd->symToByte[bd->symTotal] = (16 * ii) + jj;
					bd->symTotal += 1;
				}
			}
		}
	}

	// How many different huffman coding groups does this block use?
	bd->groupCount = get_bits(bd, 3);

	if(bd->groupCount < 2 || bd->groupCount > MAX_GROUPS)
	{
		return RETVAL_DATA_ERROR;
	}

	// nSelectors: Every GROUP_SIZE many symbols we switch huffman coding
	// tables.  Each group has a selector, which is an index into the huffman
	// coding table arrays.
	//
	// Read in the group selector array, which is stored as MTF encoded
	// bit runs.  (MTF = Move To Front.  Every time a symbol occurs its moved
	// to the front of the table, so it has a shorter encoding next time.)
	if(!(bd->nSelectors = get_bits(bd, 15)))
	{
		return RETVAL_DATA_ERROR;
	}

	for(ii = 0; ii < bd->groupCount; ii += 1)
	{
		bd->mtfSymbol[ii] = ii;
	}

	for(ii = 0; ii < bd->nSelectors; ii += 1)
	{
		// Get next value
		for(jj = 0; get_bits(bd, 1); jj += 1)
			if(jj >= bd->groupCount)
			{
				return RETVAL_DATA_ERROR;
			}

		// Decode MTF to get the next selector, and move it to the front.
		uc = bd->mtfSymbol[jj];

		while(jj)
		{
			jj = jj - 1;
			bd->mtfSymbol[jj + 1] = bd->mtfSymbol[jj];
		}

		bd->mtfSymbol[0] = bd->selectors[ii] = uc;
	}

	// Read the huffman coding tables for each group, which code for symTotal
	// literal symbols, plus two run symbols (RUNA, RUNB)
	symCount = bd->symTotal + 2;

	for(jj = 0; jj < bd->groupCount; jj += 1)
	{
		// Read lengths
		hh = get_bits(bd, 5);

		for(ii = 0; ii < symCount; ii += 1)
		{
			while(TRUE)
			{
				// !hh || hh > MAX_HUFCODE_BITS in one test.
				if(MAX_HUFCODE_BITS - 1 < hh - 1)
				{
					return RETVAL_DATA_ERROR;
				}

				// Grab 2 bits instead of 1 (slightly smaller/faster).  Stop if
				// first bit is 0, otherwise second bit says whether to
				// increment or decrement.
				kk = get_bits(bd, 2);

				if(kk & 2)
				{
					hh += (1 - ((kk & 1) << 1));
				}
				else
				{
					bd->inbufBitCount += 1;
					break;
				}
			}

			length[ii] = hh;
		}

		// Find largest and smallest lengths in this group
		minLen = maxLen = length[0];

		for(ii = 1; ii < symCount; ii += 1)
		{
			hold = length[ii];
			if(hold > maxLen)
			{
				maxLen = hold;
			}
			else if(hold < minLen)
			{
				minLen = hold;
			}
		}

		/* Calculate permute[], base[], and limit[] tables from length[].
		 *
		 * permute[] is the lookup table for converting huffman coded symbols
		 * into decoded symbols.  It contains symbol values sorted by length.
		 *
		 * base[] is the amount to subtract from the value of a huffman symbol
		 * of a given length when using permute[].
		 *
		 * limit[] indicates the largest numerical value a symbol with a given
		 * number of bits can have.  It lets us know when to stop reading.
		 *
		 * To use these, keep reading bits until value <= limit[bitcount] or
		 * youve read over 20 bits (error).  Then the decoded symbol
		 * equals permute[hufcode_value - base[hufcode_bitcount]].
		 */
		hufGroup = bd->groups + (group_data_array * jj);
		require(minLen > 0, "hufGroup minLen can't have negative values\n");
		require(minLen <= MAX_HUFCODE_BITS, "hufGroup minLen can't exceed MAX_HUFCODE_BITS\n");
		hufGroup->minLen = minLen;
		require(maxLen > 0, "hufGroup maxLen can't have negative values\n");
		require(maxLen <= MAX_HUFCODE_BITS, "hufGroup maxLen can't exceed MAX_HUFCODE_BITS\n");
		hufGroup->maxLen = maxLen;
		// Note that minLen cant be smaller than 1, so we adjust the base
		// and limit array pointers so were not always wasting the first
		// entry.  We do this again when using them (during symbol decoding).
		base = hufGroup->base - (int_array * 1);
		require(0 <= base, "can't have a negative hufGroup->base\n");
		limit = hufGroup->limit - (int_array * 1);
		// zero temp[] and limit[], and calculate permute[]
		pp = 0;

		for(ii = minLen; ii <= maxLen; ii += 1)
		{
			require(MAX_HUFCODE_BITS >= ii, "Invalid HUFCODE_BITS length\n");
			temp[ii] = 0;
			limit[ii] = 0;

			for(hh = 0; hh < symCount; hh += 1)
			{
				if(length[hh] == ii)
				{
					require(MAX_SYMBOLS >= pp, "pp exceeded MAX_SYMBOLS\n");
					hufGroup->permute[pp] = hh;
					pp += 1;
				}
			}
		}

		// Count symbols coded for at each bit length
		for(ii = 0; ii < symCount; ii += 1)
		{
			hold = length[ii];
			require(MAX_HUFCODE_BITS >= hold, "Invalid HUFCODE_BITS length\n");
			temp[hold] += 1;
		}

		/* Calculate limit[] (the largest symbol-coding value at each bit
		 * length, which is (previous limit<<1)+symbols at this level), and
		 * base[] (number of symbols to ignore at each bit length, which is
		 * limit minus the cumulative count of symbols coded for already). */
		pp = hh = 0;

		for(ii = minLen; ii < maxLen; ii += 1)
		{
			pp += temp[ii];
			limit[ii] = pp - 1;
			pp = pp << 1;
			hh += temp[ii];
			base[ii + 1] = pp - hh;
		}

		limit[maxLen] = pp + temp[maxLen] - 1;
		limit[maxLen + 1] = INT_MAX;
		base[minLen] = 0;
	}

	free(length);
	free(temp);
	return 0;
}

/* First pass, read blocks symbols into dbuf[dbufCount].
 *
 * This undoes three types of compression: huffman coding, run length encoding,
 * and move to front encoding.  We have to undo all those to know when weve
 * read enough input.
 */

int read_huffman_data(struct bunzip_data *bd, struct bwdata *bw)
{
	struct group_data *hufGroup;
	int ii;
	int jj;
	int kk;
	int runPos;
	int dbufCount;
	int symCount;
	int selector;
	int nextSym;
	int *byteCount;
	int *base;
	int *limit;
	unsigned hh;
	unsigned *dbuf = bw->dbuf;
	unsigned uc;
#if defined(__M2__)
	int int_array = sizeof(int);
	int group_data_array = sizeof(struct group_data);
#else
	int int_array = 1;
	int group_data_array = 1;
#endif
	// Weve finished reading and digesting the block header.  Now read this
	// blocks huffman coded symbols from the file and undo the huffman coding
	// and run length encoding, saving the result into dbuf[dbufCount++] = uc
	// Initialize symbol occurrence counters and symbol mtf table
	byteCount = bw->byteCount;

	for(ii = 0; ii < 256; ii += 1)
	{
		byteCount[ii] = 0;
		bd->mtfSymbol[ii] = ii;
	}

	// Loop through compressed symbols.  This is the first "tight inner loop"
	// that needs to be micro-optimized for speed.  (This one fills out dbuf[]
	// linearly, staying in cache more, so isnt as limited by DRAM access.)
	runPos = 0;
	dbufCount = 0;
	symCount = 0;
	selector = 0;
	// Some unnecessary initializations to shut gcc up.
	base = 0;
	limit = 0;
	hufGroup = 0;
	hh = 0;

	while(TRUE)
	{
		// Have we reached the end of this huffman group?
		if(!(symCount))
		{
			// Determine which huffman coding group to use.
			symCount = GROUP_SIZE - 1;

			if(selector >= bd->nSelectors)
			{
				return RETVAL_DATA_ERROR;
			}

			hufGroup = bd->groups + (group_data_array * bd->selectors[selector]);
			selector += 1;
			base = hufGroup->base - (int_array * 1);
			require(0 <= base, "can't have negative hufGroup->base\n");
			limit = hufGroup->limit - (int_array * 1);
		}
		else
		{
			symCount -= 1;
		}

		// Read next huffman-coded symbol (into jj).
		ii = hufGroup->minLen;
		jj = get_bits(bd, ii);

		while(jj > limit[ii])
		{
			// if (ii > hufGroup->maxLen) return RETVAL_DATA_ERROR;
			ii += 1;

			// Unroll get_bits() to avoid a function call when the datas in
			// the buffer already.
			if(bd->inbufBitCount)
			{
				bd->inbufBitCount -= 1;
				kk = (bd->inbufBits >> bd->inbufBitCount) & 1;
			}
			else
			{
				kk = get_bits(bd, 1);
			}

			jj = (jj << 1) | kk;
		}

		// Huffman decode jj into nextSym (with bounds checking)
		jj -= base[ii];

		if(ii > hufGroup->maxLen || jj >= MAX_SYMBOLS)
		{
			return RETVAL_DATA_ERROR;
		}

		nextSym = hufGroup->permute[jj];

		// If this is a repeated run, loop collecting data
		if(nextSym <= SYMBOL_RUNB)
		{
			// If this is the start of a new run, zero out counter
			if(!runPos)
			{
				runPos = 1;
				hh = 0;
			}

			/* Neat trick that saves 1 symbol: instead of or-ing 0 or 1 at
			   each bit position, add 1 or 2 instead. For example,
			   1011 is 1<<0 + 1<<1 + 2<<2. 1010 is 2<<0 + 2<<1 + 1<<2.
			   You can make any bit pattern that way using 1 less symbol than
			   the basic or 0/1 method (except all bits 0, which would use no
			   symbols, but a run of length 0 doesnt mean anything in this
			   context). Thus space is saved. */
			hh += (runPos << nextSym); // +runPos if RUNA; +2*runPos if RUNB
			runPos = runPos << 1;
			continue;
		}

		/* When we hit the first non-run symbol after a run, we now know
		   how many times to repeat the last literal, so append that many
		   copies to our buffer of decoded symbols (dbuf) now. (The last
		   literal used is the one at the head of the mtfSymbol array.) */
		if(runPos)
		{
			runPos = 0;

			// Check for integer overflow
			if(hh > bd->dbufSize || dbufCount + hh > bd->dbufSize)
			{
				return RETVAL_DATA_ERROR;
			}

			uc = bd->symToByte[bd->mtfSymbol[0]];
			byteCount[uc] += hh;

			while(hh)
			{
				hh -= 1;
				dbuf[dbufCount] = uc;
				dbufCount += 1;
			}
		}

		// Is this the terminating symbol?
		if(nextSym > bd->symTotal)
		{
			break;
		}

		/* At this point, the symbol we just decoded indicates a new literal
		   character. Subtract one to get the position in the MTF array
		   at which this literal is currently to be found. (Note that the
		   result cant be -1 or 0, because 0 and 1 are RUNA and RUNB.
		   Another instance of the first symbol in the mtf array, position 0,
		   would have been handled as part of a run.) */
		if(dbufCount >= bd->dbufSize)
		{
			return RETVAL_DATA_ERROR;
		}

		ii = nextSym - 1;
		uc = bd->mtfSymbol[ii];

		// On my laptop, unrolling this memmove() into a loop shaves 3.5% off
		// the total running time.
		while(ii)
		{
			ii -= 1;
			bd->mtfSymbol[ii + 1] = bd->mtfSymbol[ii];
		}

		bd->mtfSymbol[0] = uc;
		uc = bd->symToByte[uc];
		// We have our literal byte.  Save it into dbuf.
		byteCount[uc] += 1;
		dbuf[dbufCount] = uc;
		dbufCount += 1;
	}

	// Now we know what dbufCount is, do a better sanity check on origPtr.
	if(bw->origPtr >= (bw->writeCount = dbufCount))
	{
		return RETVAL_DATA_ERROR;
	}

	return 0;
}

// Flush output buffer to disk
void flush_bunzip_outbuf(struct bunzip_data *bd, int out_fd)
{
	if(bd->outbufPos)
	{
		if(write(out_fd, bd->outbuf, bd->outbufPos) != bd->outbufPos)
		{
			exit(1);
		}

		bd->outbufPos = 0;
	}
}

void burrows_wheeler_prep(struct bunzip_data *bd, struct bwdata *bw)
{
	int ii;
	int jj;
	int kk;
	unsigned *dbuf = bw->dbuf;
	int *byteCount = bw->byteCount;
	unsigned uc;
	// Turn byteCount into cumulative occurrence counts of 0 to n-1.
	jj = 0;

	for(ii = 0; ii < 256; ii += 1)
	{
		kk = jj + byteCount[ii];
		byteCount[ii] = jj;
		jj = kk;
	}

	// Use occurrence counts to quickly figure out what order dbuf would be in
	// if we sorted it.
	for(ii = 0; ii < bw->writeCount; ii += 1)
	{
		uc = dbuf[ii] & 0xFF;
		dbuf[byteCount[uc]] = dbuf[byteCount[uc]] | (ii << 8);
		byteCount[uc] += 1;
	}

	// blockRandomised support would go here.
	// Using ii as position, jj as previous character, hh as current character,
	// and uc as run count.
	bw->dataCRC = 0xffffffff;

	/* Decode first byte by hand to initialize "previous" byte. Note that it
	   doesnt get output, and if the first three characters are identical
	   it doesnt qualify as a run (hence uc=255, which will either wrap
	   to 1 or get reset). */
	if(bw->writeCount)
	{
		bw->writePos = dbuf[bw->origPtr];
		bw->writeCurrent = bw->writePos;
		bw->writePos = bw->writePos >> 8;
		bw->writeRun = -1;
	}
}

// Decompress a block of text to intermediate buffer
int read_bunzip_data(struct bunzip_data *bd)
{
	int rc = read_block_header(bd, bd->bwdata);

	if(!rc)
	{
		rc = read_huffman_data(bd, bd->bwdata);
	}

	// First thing that can be done by a background thread.
	burrows_wheeler_prep(bd, bd->bwdata);
	return rc;
}

// Undo burrows-wheeler transform on intermediate buffer to produce output.
// If !len, write up to len bytes of data to buf.  Otherwise write to out_fd.
// Returns len ? bytes written : 0.  Notice all errors are negative #s.
//
// Burrows-wheeler transform is described at:
// http://dogma.net/markn/articles/bwt/bwt.htm
// http://marknelson.us/1996/09/01/bwt/

int write_bunzip_data(struct bunzip_data *bd, struct bwdata *bw,
                      int out_fd, char *outbuf, int len)
{
	unsigned *dbuf = bw->dbuf;
	int count;
	int pos;
	int current;
	int run;
	int copies;
	int outbyte;
	int previous;
	int gotcount = 0;
	int i;
	int crc_index;

	while(TRUE)
	{
		// If last read was short due to end of file, return last block now
		if(bw->writeCount < 0)
		{
			return bw->writeCount;
		}

		// If we need to refill dbuf, do it.
		if(!bw->writeCount)
		{
			i = read_bunzip_data(bd);

			if(i)
			{
				if(i == RETVAL_LAST_BLOCK)
				{
					bw->writeCount = i;
					return gotcount;
				}
				else
				{
					return i;
				}
			}
		}

		// loop generating output
		count = bw->writeCount;
		pos = bw->writePos;
		current = bw->writeCurrent;
		run = bw->writeRun;

		while(count)
		{
			// If somebody (like tar) wants a certain number of bytes of
			// data from memory instead of written to a file, humor them.
			if(len && bd->outbufPos >= len)
			{
				goto dataus_interruptus;
			}

			count -= 1;
			// Follow sequence vector to undo Burrows-Wheeler transform.
			previous = current;
			pos = dbuf[pos];
			current = pos & 0xff;
			pos = pos >> 8;

			// Whenever we see 3 consecutive copies of the same byte,
			// the 4th is a repeat count
			if(run == 3)
			{
				run += 1;
				copies = current;
				outbyte = previous;
				current = -1;
			}
			else
			{
				run += 1;
				copies = 1;
				outbyte = current;
			}

			// Output bytes to buffer, flushing to file if necessary
			while(copies)
			{
				copies -= 1;

				if(bd->outbufPos == IOBUF_SIZE)
				{
					flush_bunzip_outbuf(bd, out_fd);
				}

				bd->outbuf[bd->outbufPos] = outbyte;
				bd->outbufPos += 1;
				crc_index = ((bw->dataCRC >> 24) ^ outbyte) & 0xFF;
				bw->dataCRC = (bw->dataCRC << 8) ^ bd->crc32Table[crc_index];
			}

			if(current != previous)
			{
				run = 0;
			}
		}

		// decompression of this block completed successfully
		bw->dataCRC = ~(bw->dataCRC);
#if defined(__M2__)

		// & 0xFFFFFFFF not working
		if(sizeof(unsigned) == 8)
		{
			bw->dataCRC <<= 32;
			bw->dataCRC >>= 32;
		}

#endif
		bd->totalCRC = ((bd->totalCRC << 1) | (bd->totalCRC >> 31)) ^ bw->dataCRC;

		// if this block had a crc error, force file level crc error.
		if(bw->dataCRC != bw->headerCRC)
		{
			bd->totalCRC = bw->headerCRC + 1;
			return RETVAL_LAST_BLOCK;
		}

dataus_interruptus:
		bw->writeCount = count;

		if(len)
		{
			gotcount += bd->outbufPos;
			memcpy(outbuf, bd->outbuf, len);
			// If we got enough data, checkpoint loop state and return
			len -= bd->outbufPos;

			if(len < 1)
			{
				bd->outbufPos -= len;

				if(bd->outbufPos)
				{
					memmove(bd->outbuf, bd->outbuf + len, bd->outbufPos);
				}

				bw->writePos = pos;
				bw->writeCurrent = current;
				bw->writeRun = run;
				return gotcount;
			}
		}
	}
}

// Allocate the structure, read file header. If !len, src_fd contains
// filehandle to read from. Else inbuf contains data.
int start_bunzip(struct bunzip_data **bdp, int src_fd)
{
	struct bunzip_data *bd;
	unsigned i;
	// Figure out how much data to allocate.
	i = sizeof(struct bunzip_data);
	// Allocate bunzip_data. Most fields initialize to zero.
	*bdp = malloc(i);
	bd = *bdp;
	memset(bd, 0, i);
	bd->inbuf = calloc(IOBUF_SIZE, sizeof(char));
	bd->outbuf = calloc(IOBUF_SIZE, sizeof(char));
	bd->selectors = calloc(32768, sizeof(char));
	bd->groups = calloc(MAX_GROUPS, sizeof(struct group_data));

	for(i = 0; i < MAX_GROUPS; i += 1)
	{
		bd->groups[i].limit = calloc(MAX_HUFCODE_BITS + 1, sizeof(int));
		bd->groups[i].base = calloc(MAX_HUFCODE_BITS, sizeof(int));
		bd->groups[i].permute = calloc(MAX_SYMBOLS, sizeof(int));
	}

	bd->symToByte = calloc(256, sizeof(unsigned));
	bd->mtfSymbol = calloc(256, sizeof(unsigned));
	bd->crc32Table = calloc(256, sizeof(unsigned));
	bd->bwdata = calloc(1, sizeof(struct bwdata));
	bd->bwdata->byteCount = calloc(256, sizeof(int));
	unsigned *crc32Table;
	bd->in_fd = src_fd;
	crc_init(bd->crc32Table, 0);
	// Ensure that file starts with "BZh".
	char *header = "BZh";

	for(i = 0; i < 3; i += 1) if(get_bits(bd, 8) != header[i])
		{
			return RETVAL_NOT_BZIP_DATA;
		}

	// Next byte ascii 1-9, indicates block size in units of 100k of
	// uncompressed data. Allocate intermediate buffer for block.
	i = get_bits(bd, 8);

	if(i < 49 || i > 57)
	{
		return RETVAL_NOT_BZIP_DATA;
	}

	bd->dbufSize = 100000 * (i - 48);
	bd->bwdata[0].dbuf = malloc(bd->dbufSize * sizeof(int));
	return 0;
}

// Example usage: decompress src_fd to dst_fd. (Stops at end of bzip data,
// not end of file.)
int bunzipStream(int src_fd, int dst_fd)
{
	struct bunzip_data *bd;
	int i;
	int j;

	if(!(i = start_bunzip(&bd, src_fd)))
	{
		i = write_bunzip_data(bd, bd->bwdata, dst_fd, 0, 0);

		if(i == RETVAL_LAST_BLOCK)
		{
			if(bd->bwdata[0].headerCRC == bd->totalCRC)
			{
				i = 0;
			}
			else
			{
				i = RETVAL_DATA_ERROR;
			}
		}
	}

	flush_bunzip_outbuf(bd, dst_fd);
	free(bd->bwdata[0].dbuf);
	free(bd->inbuf);
	free(bd->outbuf);
	free(bd->selectors);

	for(j = 0; j < MAX_GROUPS; j += 1)
	{
		free(bd->groups[j].limit);
		free(bd->groups[j].base);
		free(bd->groups[j].permute);
	}

	free(bd->groups);
	free(bd->symToByte);
	free(bd->mtfSymbol);
	free(bd->crc32Table);
	free(bd->bwdata->byteCount);
	free(bd->bwdata);
	free(bd);
	return -i;
}

void do_bunzip2(int in_fd, int out_fd)
{
	int err = bunzipStream(in_fd, out_fd);

	if(err)
	{
		exit(1);
	}
}

int main(int argc, char **argv)
{
	char *name = NULL;
	char *dest = NULL;
	FUZZING = FALSE;

	/* process arguments */
	int i = 1;

	while(i < argc)
	{
		if(NULL == argv[i])
		{
			i += 1;
		}
		else if(match(argv[i], "-f") || match(argv[i], "--file"))
		{
			name = argv[i + 1];
			require(NULL != name, "the --file option requires a filename to be given\n");
			i += 2;
		}
		else if(match(argv[i], "-o") || match(argv[i], "--output"))
		{
			dest = argv[i + 1];
			require(NULL != dest, "the --output option requires a filename to be given\n");
			i += 2;
		}
		else if(match(argv[i], "--fuzzing-mode"))
		{
			FUZZING = TRUE;
			i += 1;
		}
		else if(match(argv[i], "-h") || match(argv[i], "--help"))
		{
			fputs("Usage: ", stderr);
			fputs(argv[0], stderr);
			fputs(" --file $input.bz2", stderr);
			fputs(" --output $output\n", stderr);
			fputs("--help to get this message\n", stderr);
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

	/* Deal with no input */
	if(NULL == name)
	{
		fputs("an input file (--file $name) must be provided\n", stderr);
		exit(EXIT_FAILURE);
	}

	int in_fd = open(name, 0, 0);

	if(in_fd < 0)
	{
		fputs("Unable to open input file\n", stderr);
		exit(EXIT_FAILURE);
	}

	/* If an output name isn't provided */
	if(NULL == dest)
	{
		int length = strlen(name);
		require(length > 4, "file name length not sufficient, please provide output name with --output $filename\n");
		/* Assume they want the output file name to be the input file name minus the .bz2 */
		dest = calloc(length, sizeof(char));
		require(NULL != dest, "Failed to allocate new output file name\n");
		/* do name.bz2 => name */
		strcpy(dest, name);
		dest[length-3] = 0;
	}

	int out_fd;
	if(FUZZING)
	{
		/* Dump to /dev/null the garbage data produced during fuzzing */
		out_fd = open("/dev/null", O_WRONLY|O_CREAT|O_TRUNC, 0600);
	}
	else
	{
		out_fd = open(dest, O_WRONLY|O_CREAT|O_TRUNC, 0600);
	}

	if(out_fd < 0)
	{
		fputs("Unable to open output file for writing\n", stderr);
		exit(EXIT_FAILURE);
	}

	do_bunzip2(in_fd, out_fd);
	close(in_fd);
	close(out_fd);
	exit(0);
}
