/* Copyright (C) 2019 pts@fazekas.hu
 * Copyright (C) 2024 Jeremiah Orians
 * Copyright (C) 2024 Gábor Stefanik
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
 *
 * Built upon the great work in:
 * muxzcat.c: tiny .xz and .lzma decompression filter
 * by pts@fazekas.hu at Wed Jan 30 15:15:23 CET 2019
 * from https://github.com/pts/muxzcat
 * For .xz it supports only LZMA2 (no other filters such as BCJ).
 * For .lzma it doesn't work with files with 5 <= lc + lp <= 12.
 * It doesn't verify checksums (e.g. CRC-32 and CRC-64).
 * It extracts the first stream only, and it ignores the index.
 *
 * LZMA algorithm implementation based on
 * https://github.com/pts/pts-tiny-7z-sfx/commit/b9a101b076672879f861d472665afaa6caa6fec1
 * , which is based on 7z922.tar.bz2.
 */

#include <stdio.h>
#include <string.h>  /* memcpy(), memmove() */
#include <unistd.h>  /* read(), write() */
#include <stdint.h>
#include <stdlib.h>  /* realloc() */
#include "M2libc/bootstrappable.h"

/* Constants needed */
#define SZ_OK 0
#define SZ_ERROR_DATA 1
#define SZ_ERROR_MEM 2  /* Out of memory. */
#define SZ_ERROR_CRC 3
#define SZ_ERROR_UNSUPPORTED 4
#define SZ_ERROR_PARAM 5
#define SZ_ERROR_INPUT_EOF 6
/*#define SZ_ERROR_OUTPUT_EOF 7*/
#define SZ_ERROR_READ 8
#define SZ_ERROR_WRITE 9
#define SZ_ERROR_FINISHED_WITH_MARK 15            /* LzmaDec_DecodeToDic stream was finished with end mark. */
#define SZ_ERROR_NOT_FINISHED 16                  /* LzmaDec_DecodeToDic stream was not finished, i.e. dicfLimit reached while there is input to decompress */
#define SZ_ERROR_NEEDS_MORE_INPUT 17              /* LzmaDec_DecodeToDic, you must provide more input bytes */
/*#define SZ_MAYBE_FINISHED_WITHOUT_MARK SZ_OK*/  /* LzmaDec_DecodeToDic, there is probability that stream was finished without end mark */
#define SZ_ERROR_CHUNK_NOT_CONSUMED 18
#define SZ_ERROR_NEEDS_MORE_INPUT_PARTIAL 17      /* LzmaDec_DecodeToDic, more input needed, but existing input was partially processed */
#define LZMA_REQUIRED_INPUT_MAX 20
#define LZMA_BASE_SIZE 1846
#define LZMA_LIT_SIZE 768
#define LZMA2_LCLP_MAX 4
#define MAX_DIC_SIZE 1610612736  /* ~1.61 GB. 2 GiB is user virtual memory limit for many 32-bit systems. */
#define MAX_DIC_SIZE_PROP 37
#define MAX_MATCH_SIZE 273
#define kNumTopBits 24
#define kTopValue (1 << kNumTopBits)
#define kNumBitModelTotalBits 11
#define kBitModelTotal (1 << kNumBitModelTotalBits)
#define kNumMoveBits 5
#define RC_INIT_SIZE 5
#define kNumPosBitsMax 4
#define kNumPosStatesMax (1 << kNumPosBitsMax)
#define kLenNumLowBits 3
#define kLenNumLowSymbols (1 << kLenNumLowBits)
#define kLenNumMidBits 3
#define kLenNumMidSymbols (1 << kLenNumMidBits)
#define kLenNumHighBits 8
#define kLenNumHighSymbols (1 << kLenNumHighBits)
#define LenChoice 0
#define LenChoice2 (LenChoice + 1)
#define LenLow (LenChoice2 + 1)
#define LenMid (LenLow + (kNumPosStatesMax << kLenNumLowBits))
#define LenHigh (LenMid + (kNumPosStatesMax << kLenNumMidBits))
#define kNumLenProbs (LenHigh + kLenNumHighSymbols)
#define kNumStates 12
#define kNumLitStates 7
#define kStartPosModelIndex 4
#define kEndPosModelIndex 14
#define kNumFullDistances (1 << (kEndPosModelIndex >> 1))
#define kNumPosSlotBits 6
#define kNumLenToPosStates 4
#define kNumAlignBits 4
#define kAlignTableSize (1 << kNumAlignBits)
#define kMatchMinLen 2
#define kMatchSpecLenStart (kMatchMinLen + kLenNumLowSymbols + kLenNumMidSymbols + kLenNumHighSymbols)
#define IsMatch 0
#define IsRep (IsMatch + (kNumStates << kNumPosBitsMax))
#define IsRepG0 (IsRep + kNumStates)
#define IsRepG1 (IsRepG0 + kNumStates)
#define IsRepG2 (IsRepG1 + kNumStates)
#define IsRep0Long (IsRepG2 + kNumStates)
#define PosSlot (IsRep0Long + (kNumStates << kNumPosBitsMax))
#define SpecPos (PosSlot + (kNumLenToPosStates << kNumPosSlotBits))
#define Align (SpecPos + kNumFullDistances - kEndPosModelIndex)
#define LenCoder (Align + kAlignTableSize)
#define RepLenCoder (LenCoder + kNumLenProbs)
#define Literal (RepLenCoder + kNumLenProbs)
#define LZMA_DIC_MIN (1 << 12)
#define SZ_ERROR_BAD_MAGIC 51
#define SZ_ERROR_BAD_STREAM_FLAGS 52  /* SZ_ERROR_BAD_MAGIC is reported instead. */
#define SZ_ERROR_UNSUPPORTED_FILTER_COUNT 53
#define SZ_ERROR_BAD_BLOCK_FLAGS 54
#define SZ_ERROR_UNSUPPORTED_FILTER_ID 55
#define SZ_ERROR_UNSUPPORTED_FILTER_PROPERTIES_SIZE 56
#define SZ_ERROR_BAD_PADDING 57
#define SZ_ERROR_BLOCK_HEADER_TOO_LONG 58
#define SZ_ERROR_BAD_CHUNK_CONTROL_BYTE 59
#define SZ_ERROR_BAD_CHECKSUM_TYPE 60
#define SZ_ERROR_BAD_DICTIONARY_SIZE 61
#define SZ_ERROR_UNSUPPORTED_DICTIONARY_SIZE 62
#define SZ_ERROR_FEED_CHUNK 63
/*#define SZ_ERROR_NOT_FINISHED_WITH_MARK 64*/
#define SZ_ERROR_BAD_DICPOS 65
#define SZ_ERROR_MISSING_INITPROP 67
#define SZ_ERROR_BAD_LCLPPB_PROP 68
#define FILTER_ID_LZMA2 0x21
// 65536 + 12 * 1 byte (sizeof(uint8_t)
#define sizeof_readBuf 65548
#define sizeof_writeBuf 0x1000000
#define MAX_DICF_SIZE (MAX_DIC_SIZE + MAX_MATCH_SIZE + sizeof_writeBuf)  /* Maximum number of bytes in global.dicf. */
#define DUMMY_ERROR 0 /* unexpected end of input stream */
#define DUMMY_LIT 1
#define DUMMY_MATCH 2
#define DUMMY_REP 3
/* (LZMA_BASE_SIZE + (LZMA_LIT_SIZE << LZMA2_LCLP_MAX)) */
#define probs_size 14134
#define BIT31 (1<<31)
#define BITS32 (0x7FFFFFFF | BIT31)
#define HIGHBITS (0xFFFFFFFF - BITS32)

FILE* destination;
FILE* source;
uint32_t pos;

/* For LZMA streams, lc <= 8, lp <= 4, lc + lp <= 8 + 4 == 12.
 * For LZMA2 streams, lc + lp <= 4.
 * Minimum value: 1846.
 * Maximum value for LZMA streams: 1846 + (768 << (8 + 4)) == 3147574.
 * Maximum value for LZMA2 streams: 1846 + (768 << 4) == 14134.
 * Memory usage of prob: sizeof(uint32_t) * value == (2 or 4) * value bytes.
 */

struct CLzmaDec
{
	/* lc, lp and pb would fit into a byte, but i386 code is shorter as uint32_t.
	 *
	 * Constraints:
	 *
	 * * (0 <= lc <= 8) by LZMA.
	 * * 0 <= lc <= 4 by LZMA2 and muxzcat-LZMA and muzxcat-LZMA2.
	 * * 0 <= lp <= 4.
	 * * 0 <= pb <= 4.
	 * * (0 <= lc + lp == 8 + 4 <= 12) by LZMA.
	 * * 0 <= lc + lp <= 4 by LZMA2 and muxzcat-LZMA and muxzcat-LZMA2.
	 */
	uint32_t lc;
	uint32_t lp;
	uint32_t pb; /* Configured in prop byte. */
	/* Maximum lookback delta.
	 * More optimized implementations (but not this version of muxzcat) need
	 * that many bytes of storage for the dictionary. muxzcat uses more,
	 * because it keeps the entire decompression output in memory, for
	 * the simplicity of the implementation.
	 * Configured in dicSizeProp byte. Maximum LZMA and LZMA2 supports is 0xffffffff,
	 * maximum we support is MAX_DIC_SIZE == 1610612736.
	 */
	uint32_t dicSize;
	uint8_t *buf;
	uint32_t range;
	uint32_t code;
	uint32_t dicfPos;  /* The next decompression output byte will be written to dicf + dicfPos. */
	uint32_t dicfLimit;  /* It's OK to write this many decompression output bytes to dic. GrowDic(dicfPos + len) must be called before writing len bytes at dicfPos. */
	uint32_t writtenPos;  /* Decompression output bytes dicf[:writtenPos] are already written to the output file. writtenPos <= dicfPos. */
	uint32_t discardedSize;  /* Number of decompression output bytes discarded. */
	uint32_t writeRemaining;  /* Maximum number of remaining bytes to write, or ~0 for unlimited. */
	uint32_t allocCapacity;  /* Number of bytes allocated in dic. */
	uint32_t processedPos;  /* Decompression output byte count since the last call to LzmaDec_InitDicAndState(TRUE, ...); */
	uint32_t checkDicSize;
	uint32_t state;
	uint32_t reps[4];
	uint32_t remainLen;
	uint32_t tempBufSize;
	uint32_t probs[probs_size];
	int needFlush;
	int needInitLzma;
	int needInitDic;
	int needInitState;
	int needInitProp;
	uint8_t tempBuf[LZMA_REQUIRED_INPUT_MAX];
	/* Contains the decompresison output, and used as the lookback dictionary.
	 * allocCapacity bytes are allocated, it's OK to grow it up to dicfLimit.
	 */
	uint8_t *dicf;
	uint8_t* readBuf;
	uint8_t* readCur;
	uint8_t* readEnd;
};

/* globals needed */
struct CLzmaDec* global;
int FUZZING;

/* Writes uncompressed data (global.dicf[global.writtenPos : global.dicfPos] to stdout. */
void Flush()
{
	/* print the bytes in the buffer until done */
	uint8_t* p = global->dicf + global->writtenPos;
	uint8_t* q = global->dicf + global->dicfPos;

	while(p < q)
	{
		fputc(0xFF & p[0], destination);
		p = p + 1;
	}

	global->writtenPos = global->dicfPos;
}

void FlushDiscardOldFromStartOfDic()
{
	if(global->dicfPos > global->dicSize)
	{
		uint32_t delta = global->dicfPos - global->dicSize;

		if(delta + MAX_MATCH_SIZE >= sizeof_writeBuf)
		{
			Flush();
			global->dicf = memmove(global->dicf, global->dicf + delta, global->dicSize);
			global->dicfPos = global->dicfPos - delta;
			global->dicfLimit = global->dicfLimit - delta;
			global->writtenPos = global->writtenPos - delta;
			global->discardedSize = global->discardedSize + delta;
		}
	}

}

void GrowCapacity(uint32_t newCapacity)
{
	if(newCapacity > global->allocCapacity)
	{
		/* make sure we don't alloc too much */
		require(newCapacity <= MAX_DICF_SIZE, "GrowCapacity exceeds MAX_DICF_SIZE");

		/* Get our new block */
		uint8_t* dicf = calloc(newCapacity, sizeof(uint8_t));
		require(NULL != dicf, "GrowCapacity memory allocation failed");

		/* copy our old block into it  and get rid of the old block */
		if (NULL != global->dicf) {
			memcpy(dicf, global->dicf, global->allocCapacity);
			free(global->dicf);
		}

		/* now track that new state */
		global->dicf = dicf;
		global->allocCapacity = newCapacity;
	}

	/* else no need to grow */
}

void FlushDiscardGrowDic(uint32_t dicfPosDelta)
{
	uint32_t minCapacity = global->dicfPos + dicfPosDelta;
	uint32_t newCapacity;

	if(minCapacity > global->allocCapacity)
	{
		FlushDiscardOldFromStartOfDic();
		minCapacity = global->dicfPos + dicfPosDelta;

		if(minCapacity > global->allocCapacity)
		{
			/* start by assuming 64KB */
			newCapacity = (1 << 16);

			while(newCapacity + MAX_MATCH_SIZE < minCapacity)
			{
				/* No overflow. */
				if(newCapacity > global->dicSize)
				{
					newCapacity = global->dicSize;
					if(newCapacity + MAX_MATCH_SIZE < minCapacity)
					{
						newCapacity = minCapacity - MAX_MATCH_SIZE;
					}
					break;
				}
				newCapacity = newCapacity << 1;
			}

			GrowCapacity(newCapacity + MAX_MATCH_SIZE);
		}
	}
}


void LzmaDec_DecodeReal(uint32_t limit, uint8_t *bufLimit)
{
	uint32_t *probs = global->probs;
	uint32_t state = global->state;
	uint32_t rep0 = global->reps[0];
	uint32_t rep1 = global->reps[1];
	uint32_t rep2 = global->reps[2];
	uint32_t rep3 = global->reps[3];
	uint32_t pbMask = (1 << (global->pb)) - 1;
	uint32_t lpMask = (1 << (global->lp)) - 1;
	uint32_t lc = global->lc;
	uint8_t* dicl = global->dicf;
	uint32_t diclLimit = global->dicfLimit;
	uint32_t diclPos = global->dicfPos;
	uint32_t processedPos = global->processedPos;
	uint32_t checkDicSize = global->checkDicSize;
	uint32_t len = 0;
	uint8_t* buf = global->buf;
	uint32_t range = global->range;
	uint32_t code = global->code;

	uint32_t* prob;
	uint32_t bound;
	uint32_t ttt;
	uint32_t posState;
	uint32_t symbol;
	uint32_t matchByte;
	uint32_t offs;
	uint32_t bit;
	uint32_t* probLit;
	uint32_t distance;
	uint32_t limita;
	uint32_t *probLen;
	uint32_t offset;
	uint32_t posSlot;
	uint32_t numDirectBits;
	uint32_t mask;
	uint32_t i;
	uint32_t n;
	uint32_t t;
	uint32_t rem;
	uint32_t curLen;
	uint32_t pos;
	uint8_t* p;

	do
	{
		posState = processedPos & pbMask;
		p = probs;
		prob = p + 4 * (IsMatch + (state << kNumPosBitsMax) + posState);
		ttt = prob[0];

		if(range < kTopValue)
		{
			range = range << 8;
			code = (code << 8) | (0xFF & buf[0]);
			buf = buf + 1;
		}

		bound = (range >> kNumBitModelTotalBits) * ttt;

		if(code < bound)
		{
			range = bound;
			prob[0] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[0]);
			p = probs;
			prob = p + 4 * Literal;

			if(checkDicSize != 0 || processedPos != 0)
			{
				if(diclPos == 0)
				{
					p = prob;
					prob = p + 4 * (LZMA_LIT_SIZE * (((processedPos & lpMask) << lc) + (0xFF & dicl[(diclLimit) - 1])) >> (8 - lc));
				}
				else
				{
					p = prob;
					prob = p + 4 *(LZMA_LIT_SIZE * ((((processedPos & lpMask) << lc) + (0xFF & dicl[diclPos - 1])) >> (8 - lc)));
				}
			}

			if(state < kNumLitStates)
			{
				if(state < 4) state = 0;
				else state = state - 3;
				symbol = 1;

				do
				{
					ttt = prob[symbol];

					if(range < kTopValue)
					{
						range = range << 8;
						code = (code << 8) | (0xFF & buf[0]);
						buf = buf + 1;
					}

					bound = (range >> kNumBitModelTotalBits) * ttt;

					if(code < bound)
					{
						range = bound;
						prob[symbol] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[symbol]);
						symbol = (symbol + symbol);
					}
					else
					{
						range = range - bound;
						code = code - bound;
						prob[symbol] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[symbol]);
						symbol = (symbol + symbol) + 1;
					}
				} while(symbol < 0x100);
			}
			else
			{
				if(diclPos < rep0) matchByte = 0xFF & dicl[(diclPos - rep0) + diclLimit];
				else matchByte = 0xFF & dicl[(diclPos - rep0)];

				offs = 0x100;

				if(state < 10) state = state - 3;
				else state = state - 6;

				symbol = 1;

				do
				{
					matchByte = matchByte << 1;
					bit = (matchByte & offs);
					p = prob;
					probLit = p + 4 * (offs + bit + symbol);
					ttt = probLit[0];

					if(range < kTopValue)
					{
						range = range << 8;
						code = (code << 8) | (0xFF & buf[0]);
						buf = buf + 1;
					}

					bound = (range >> kNumBitModelTotalBits) * ttt;

					if(code < bound)
					{
						range = bound;
						probLit[0] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & probLit[0]);
						symbol = (symbol + symbol);
						offs = offs & ~bit;
					}
					else
					{
						range = range - bound;
						code = code - bound;
						probLit[0] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & probLit[0]);
						symbol = (symbol + symbol) + 1;
						offs = offs & bit;
					}
				} while(symbol < 0x100);
			}

			if(diclPos >= global->allocCapacity)
			{
				global->dicfPos = diclPos;
				FlushDiscardGrowDic(1);
				dicl = global->dicf;
				diclLimit = global->dicfLimit;
				diclPos = global->dicfPos;
			}

			dicl[diclPos] = (0xFF & symbol) | ((~0xFF) & dicl[diclPos]);
			diclPos = diclPos + 1;
			processedPos = processedPos + 1;
			continue;
		}
		else
		{
			range = range - bound;
			code = code - bound;
			prob[0] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[0]);
			p = probs;
			prob = p + 4 * (IsRep + state);
			ttt = prob[0];

			if(range < kTopValue)
			{
				range = range << 8;
				code = (code << 8) | (0xFF & buf[0]);
				buf = buf + 1;
			}

			bound = (range >> kNumBitModelTotalBits) * ttt;

			if(code < bound)
			{
				range = bound;
				prob[0] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[0]);
				state = state + kNumStates;
				p = probs;
				prob = p + 4 * LenCoder;
			}
			else
			{
				range = range - bound;
				code = code - bound;
				prob[0] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[0]);

				require((checkDicSize != 0) || (processedPos != 0), "checkDicsize == 0 && processPos == 0");

				p = probs;
				prob = p + 4 * (IsRepG0 + state);
				ttt = prob[0];

				if(range < kTopValue)
				{
					range = range << 8;
					code = (code << 8) | (0xFF & buf[0]);
					buf = buf + 1;
				}

				bound = (range >> kNumBitModelTotalBits) * ttt;

				if(code < bound)
				{
					range = bound;
					prob[0] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[0]);
					p = probs;
					prob = p + 4 * (IsRep0Long + (state << kNumPosBitsMax) + posState);
					ttt = prob[0];

					if(range < kTopValue)
					{
						range = range << 8;
						code = (code << 8) | (0xFF & buf[0]);
						buf = buf + 1;
					}

					bound = (range >> kNumBitModelTotalBits) * ttt;

					if(code < bound)
					{
						range = bound;
						prob[0] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[0]);

						if(diclPos >= global->allocCapacity)
						{
							global->dicfPos = diclPos;
							FlushDiscardGrowDic(1);
							dicl = global->dicf;
							diclLimit = global->dicfLimit;
							diclPos = global->dicfPos;
						}

						if(diclPos < rep0) dicl[diclPos] = (0xFF & dicl[(diclPos - rep0) + diclLimit]) | ((~0xFF) & dicl[diclPos]);
						else dicl[diclPos] = (0xFF & dicl[(diclPos - rep0)]) | ((~0xFF) & dicl[diclPos]);

						diclPos = diclPos + 1;
						processedPos = processedPos + 1;

						if(state < kNumLitStates) state = 9;
						else state = 11;

						continue;
					}

					range = range - bound;
					code = code - bound;
					prob[0] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[0]);
				}
				else
				{
					range = range - bound;
					code = code - bound;
					prob[0] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[0]);
					p = probs;
					prob = p + 4 * (IsRepG1 + state);
					ttt = prob[0];

					if(range < kTopValue)
					{
						range = range << 8;
						code = (code << 8) | (0xFF & buf[0]);
						buf = buf + 1;
					}

					bound = (range >> kNumBitModelTotalBits) * ttt;

					if(code < bound)
					{
						range = bound;
						prob[0] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[0]);
						distance = rep1;
					}
					else
					{
						range = range - bound;
						code = code - bound;
						prob[0] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[0]);
						p = probs;
						prob = p + 4 * (IsRepG2 + state);
						ttt = prob[0];

						if(range < kTopValue)
						{
							range = range << 8;
							code = (code << 8) | (0xFF & buf[0]);
							buf = buf + 1;
						}

						bound = (range >> kNumBitModelTotalBits) * ttt;

						if(code < bound)
						{
							range = bound;
							prob[0] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[0]);
							distance = rep2;
						}
						else
						{
							range = range - bound;
							code = code - bound;
							prob[0] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[0]);
							distance = rep3;
							rep3 = rep2;
						}

						rep2 = rep1;
					}

					rep1 = rep0;
					rep0 = distance;
				}

				if(state < kNumLitStates) state = 8;
				else state = 11;

				p = probs;
				prob = p + 4 * RepLenCoder;
			}

			p = prob;
			probLen = p + 4 * LenChoice;
			ttt = probLen[0];

			if(range < kTopValue)
			{
				range <<= 8;
				code = (code << 8) | (0xFF & buf[0]);
				buf = buf + 1;
			}

			bound = (range >> kNumBitModelTotalBits) * ttt;

			if(code < bound)
			{
				range = bound;
				probLen[0] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & probLen[0]);
				p = prob;
				probLen = p + 4 * (LenLow + (posState << kLenNumLowBits));
				offset = 0;
				limita = (1 << kLenNumLowBits);
			}
			else
			{
				range = range - bound;
				code = code - bound;
				probLen[0] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & probLen[0]);
				p = prob;
				probLen = p + 4 * LenChoice2;
				ttt = probLen[0];

				if(range < kTopValue)
				{
					range = range << 8;
					code = (code << 8) | (0xFF & buf[0]);
					buf = buf + 1;
				}

				bound = (range >> kNumBitModelTotalBits) * ttt;

				if(code < bound)
				{
					range = bound;
					probLen[0] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & probLen[0]);
					p = prob;
					probLen = p + 4 * (LenMid + (posState << kLenNumMidBits));
					offset = kLenNumLowSymbols;
					limita = (1 << kLenNumMidBits);
				}
				else
				{
					range = range - bound;
					code = code - bound;
					probLen[0] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & probLen[0]);
					p = prob;
					probLen = p + 4 * LenHigh;
					offset = kLenNumLowSymbols + kLenNumMidSymbols;
					limita = (1 << kLenNumHighBits);
				}
			}

			len = 1;

			do
			{
				ttt = probLen[len];

				if(range < kTopValue)
				{
					range = range << 8;
					code = (code << 8) | (0xFF & buf[0]);
					buf = buf + 1;
				}

				bound = (range >> kNumBitModelTotalBits) * ttt;

				if(code < bound)
				{
					range = bound;
					probLen[len] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & probLen[len]);
					len = (len + len);
				}
				else
				{
					range = range - bound;
					code = code - bound;
					probLen[len] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & probLen[len]);
					len = (len + len) + 1;
				}
			} while(len < limita);

			len = len - limita + offset;

			if(state >= kNumStates)
			{
				if(len < kNumLenToPosStates) { p = probs; prob = p + 4 * (PosSlot + (len << kNumPosSlotBits));  }
				else { p = probs; prob = p + 4 * (PosSlot + ((kNumLenToPosStates - 1) << kNumPosSlotBits));  }

				distance = 1;

				do
				{
					ttt = prob[distance];

					if(range < kTopValue)
					{
						range = range << 8;
						code = (code << 8) | (0xFF & buf[0]);
						buf = buf + 1;
					}

					bound = (range >> kNumBitModelTotalBits) * ttt;
					if(code < bound)
					{
						range = bound;
						prob[distance] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[distance]);
						distance = (distance + distance);
					}
					else
					{
						range = range - bound;
						code = code - bound;
						prob[distance] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[distance]);
						distance = (distance + distance) + 1;
					}
				} while(distance < (1 << 6));

				distance = distance - (1 << 6);

				if(distance >= kStartPosModelIndex)
				{
					posSlot = distance;
					numDirectBits = (distance >> 1) - 1;
					distance = (2 | (distance & 1));

					if(posSlot < kEndPosModelIndex)
					{
						distance = distance << numDirectBits;
						p = probs;
						prob = p + 4 * (SpecPos + distance - posSlot - 1);
						mask = 1;
						i = 1;

						do
						{
							ttt = prob[i];

							if(range < kTopValue)
							{
								range = range << 8;
								code = (code << 8) | (0xFF & buf[0]);
								buf = buf + 1;
							}

							bound = (range >> kNumBitModelTotalBits) * ttt;

							if(code < bound)
							{
								range = bound;
								prob[i] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[i]);
								i = (i + i);
							}
							else
							{
								range = range - bound;
								code = code - bound;
								prob[i] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[i]);
								i = (i + i) + 1;
								distance = distance | mask;
							}

							mask = mask << 1;
							numDirectBits = numDirectBits - 1;
						} while(numDirectBits != 0);
					}
					else
					{
						numDirectBits = numDirectBits - kNumAlignBits;

						do
						{
							if(range < kTopValue)
							{
								range = range << 8;
								code = (code << 8) | (0xFF & buf[0]);
								buf = buf + 1;
							}

							range = range >> 1;
							{
								code = code - range;
								t = (0 - (code >> 31));
								distance = (distance << 1) + (t + 1);
								code = code + (range & t);
							}
							numDirectBits = numDirectBits - 1;
						} while(numDirectBits != 0);

						p = probs;
						prob = p + 4 * Align;
						distance = distance << kNumAlignBits;
						i = 1;
						ttt = prob[i];

						if(range < kTopValue)
						{
							range = range << 8;
							code = (code << 8) | (0xFF & buf[0]);
							buf = buf + 1;
						}

						bound = (range >> kNumBitModelTotalBits) * ttt;

						if(code < bound)
						{
							range = bound;
							prob[i] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[i]);
							i = (i + i);
						}
						else
						{
							range = range - bound;
							code = code - bound;
							prob[i] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[i]);
							i = (i + i) + 1;
							distance = distance | 1;
						}

						ttt = prob[i];

						if(range < kTopValue)
						{
							range = range << 8;
							code = (code << 8) | (0xFF & buf[0]);
							buf = buf + 1;
						}

						bound = (range >> kNumBitModelTotalBits) * ttt;

						if(code < bound)
						{
							range = bound;
							prob[i] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[i]);
							i = (i + i);
						}
						else
						{
							range = range - bound;
							code = code - bound;
							prob[i] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[i]);
							i = (i + i) + 1;
							distance = distance | 2;
						}

						ttt = prob[i];

						if(range < kTopValue)
						{
							range = range << 8;
							code = (code << 8) | (0xFF & buf[0]);
							buf = buf + 1;
						}

						bound = (range >> kNumBitModelTotalBits) * ttt;

						if(code < bound)
						{
							range = bound;
							prob[i] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[i]);
							i = (i + i);
						}
						else
						{
							range = range - bound;
							code = code - bound;
							prob[i] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[i]);
							i = (i + i) + 1;
							distance = distance | 4;
						}

						ttt = prob[i];

						if(range < kTopValue)
						{
							range = range << 8;
							code = (code << 8) | (0xFF & buf[0]);
							buf = buf + 1;
						}

						bound = (range >> kNumBitModelTotalBits) * ttt;

						if(code < bound)
						{
							range = bound;
							prob[i] = (BITS32 & ((ttt + ((kBitModelTotal - ttt) >> kNumMoveBits)))) | (HIGHBITS & prob[i]);
							i = (i + i);
						}
						else
						{
							range = range - bound;
							code = code - bound;
							prob[i] = (BITS32 & ((ttt - (ttt >> kNumMoveBits)))) | (HIGHBITS & prob[i]);
							i = (i + i) + 1;
							distance = distance | 8;
						}

						if(distance == BITS32)
						{
							len = len + kMatchSpecLenStart;
							state = state - kNumStates;
							break;
						}
					}
				}

				rep3 = rep2;
				rep2 = rep1;
				rep1 = rep0;
				rep0 = distance + 1;

				if(checkDicSize == 0) require(distance < processedPos , "distance >= processedPos");
				else require(distance < checkDicSize, "distance >= checkDicSize");

				if(state < kNumStates + kNumLitStates) state = kNumLitStates;
				else state = kNumLitStates + 3;
			}

			len = len + kMatchMinLen;
			require(len <= MAX_MATCH_SIZE, "len greater than MAX_MATCH_SIZE");
			require(limit != diclPos, "limit == diclPos");

			rem = limit - diclPos;
			if(rem < len) curLen = rem;
			else curLen = len;

			if(diclPos < rep0) pos = (diclPos - rep0) + diclLimit;
			else pos = diclPos - rep0;

			processedPos = processedPos + curLen;
			len = len - curLen;

			/* TODO(pts): ASSERT(len == curLen);, simplify buffering code. */
			/* + cannot overflow. */
			if((diclPos + curLen) > global->allocCapacity)
			{
				global->dicfPos = diclPos;
				FlushDiscardGrowDic(curLen);

				pos = pos + global->dicfPos - diclPos;
				dicl = global->dicf;
				diclLimit = global->dicfLimit;
				diclPos = global->dicfPos;
			}

			if((pos + curLen) <= diclLimit)
			{
				require(diclPos > pos, "diclPos > pos");
				require(curLen > 0, "curLen > 0");
				i = 0;
				n = curLen;
				/* overlapping memcpy of sorts */
				while(n > 0)
				{
					dicl[diclPos + i] = (0xFF & dicl[pos + i]) | ((~0xFF) & dicl[diclPos + i]);
					i = i + 1;
					n = n - 1;
				}
				diclPos = diclPos + curLen;
			}
			else
			{
				do
				{
					dicl[diclPos] = (0xFF & dicl[pos]) | ((~0xFF) & dicl[diclPos]);
					diclPos = diclPos + 1;
					pos = pos + 1;

					if(pos == diclLimit)
					{
						pos = 0;
					}
					curLen = curLen - 1;
				} while(curLen != 0);
			}
		}
	} while((diclPos < limit) && (buf < bufLimit));

	if(range < kTopValue)
	{
		range = range << 8;
		code = (code << 8) | (0xFF & buf[0]);
		buf = buf + 1;
	}

	global->buf = buf;
	global->range = range;
	global->code = code;
	global->remainLen = len;
	global->dicfPos = diclPos;
	global->processedPos = processedPos;
	global->reps[0] = rep0;
	global->reps[1] = rep1;
	global->reps[2] = rep2;
	global->reps[3] = rep3;
	global->state = state;
}

void LzmaDec_WriteRem(uint32_t limit)
{
	uint8_t *dicl;
	uint32_t diclPos;
	uint32_t diclLimit;
	uint32_t len;
	uint32_t rep0;

	if(global->remainLen != 0 && global->remainLen < kMatchSpecLenStart)
	{
		dicl = global->dicf;
		diclPos = global->dicfPos;
		diclLimit = global->dicfLimit;
		len = global->remainLen;
		rep0 = global->reps[0];

		if(limit - diclPos < len)
		{
			len = limit - diclPos;
		}

		if(diclPos + len > global->allocCapacity)
		{
			FlushDiscardGrowDic(len);
			dicl = global->dicf;
			diclLimit = global->dicfLimit;
			diclPos = global->dicfPos;
		}

		if((global->checkDicSize == 0) && ((global->dicSize - global->processedPos) <= len))
		{
			global->checkDicSize = global->dicSize;
		}

		global->processedPos = global->processedPos + len;
		global->remainLen = global->remainLen - len;

		while(len != 0)
		{
			len = len - 1;
			if(diclPos < rep0) dicl[diclPos] = (0xFF & dicl[(diclPos - rep0) + diclLimit]) | ((~0xFF) & dicl[diclPos]);
			else dicl[diclPos] = (0xFF & dicl[diclPos - rep0]) | ((~0xFF) & dicl[diclPos]);
			diclPos = diclPos + 1;
		}

		global->dicfPos = diclPos;
	}
}

void LzmaDec_DecodeReal2(uint32_t limit, uint8_t *bufLimit)
{
	uint32_t limit2;
	uint32_t rem;

	do
	{
		limit2 = limit;

		if(global->checkDicSize == 0)
		{
			rem = global->dicSize - global->processedPos;

			if((limit - global->dicfPos) > rem)
			{
				limit2 = global->dicfPos + rem;
			}
		}

		LzmaDec_DecodeReal(limit2, bufLimit);

		if(global->processedPos >= global->dicSize)
		{
			global->checkDicSize = global->dicSize;
		}

		LzmaDec_WriteRem(limit);
	} while((global->dicfPos < limit) && (global->buf < bufLimit) && (global->remainLen < kMatchSpecLenStart));

	if(global->remainLen > kMatchSpecLenStart)
	{
		global->remainLen = kMatchSpecLenStart;
	}
}

int LzmaDec_TryDummy(uint8_t* buf, uint32_t inSize)
{
	uint32_t range = global->range;
	uint32_t code = global->code;
	uint8_t* bufLimit = buf + inSize;
	uint32_t* probs = global->probs;
	uint32_t state = global->state;
	int res;
	uint32_t* prob;
	uint32_t bound;
	uint32_t ttt;
	uint32_t posState;
	uint32_t hold;
	uint32_t symbol;
	uint32_t matchByte;
	uint32_t offs;
	uint32_t bit;
	uint32_t* probLit;
	uint32_t len;
	uint32_t limit;
	uint32_t offset;
	uint32_t* probLen;
	uint32_t posSlot;
	uint32_t numDirectBits;
	uint32_t i;
	uint8_t* p;

	posState = (global->processedPos) & ((1 << global->pb) - 1);
	p = probs;
	prob = p + 4 * (IsMatch + (state << kNumPosBitsMax) + posState);
	ttt = prob[0];

	if(range < kTopValue)
	{
		if(buf >= bufLimit)
		{
			return DUMMY_ERROR;
		}

		range = range << 8;
		code = (code << 8) | (0xFF & buf[0]);
		buf = buf + 1;
	}

	bound = (range >> kNumBitModelTotalBits) * ttt;

	if(code < bound)
	{
		range = bound;
		p = probs;
		prob = p + 4 * Literal;

		if(global->checkDicSize != 0 || global->processedPos != 0)
		{
			hold = (((global->processedPos) & ((1 << (global->lp)) - 1)) << global->lc);
			if(global->dicfPos == 0)
			{
				hold = hold + ((0xFF & global->dicf[global->dicfLimit - 1]) >> (8 - global->lc));
			}
			else
			{
				hold = hold + ((0xFF & global->dicf[global->dicfPos - 1]) >> (8 - global->lc));
			}
			p = prob;
			prob = p + 4 * (LZMA_LIT_SIZE * hold);
		}

		if(state < kNumLitStates)
		{
			symbol = 1;

			do
			{
				ttt = prob[symbol];

				if(range < kTopValue)
				{
					if(buf >= bufLimit)
					{
						return DUMMY_ERROR;
					}

					range = range << 8;
					code = (code << 8) | (0xFF & buf[0]);
					buf = buf + 1;
				}

				bound = (range >> kNumBitModelTotalBits) * ttt;

				if(code < bound)
				{
					range = bound;
					symbol = (symbol + symbol);
				}
				else
				{
					range = range - bound;
					code = code - bound;
					symbol = (symbol + symbol) + 1;
				}
			} while(symbol < 0x100);
		}
		else
		{
			if(global->dicfPos < (global->reps[0] & BITS32))
			{
				hold = global->dicfPos - (global->reps[0] & BITS32) + global->dicfLimit;
			}
			else hold = global->dicfPos - (global->reps[0] & BITS32);
			matchByte = 0xFF & global->dicf[hold];

			offs = 0x100;
			symbol = 1;

			do
			{
				matchByte = matchByte << 1;
				bit = (matchByte & offs);
				p = prob;
				probLit = p + 4 * (offs + bit + symbol);
				ttt = probLit[0];

				if(range < kTopValue)
				{
					if(buf >= bufLimit)
					{
						return DUMMY_ERROR;
					}

					range = range << 8;
					code = (code << 8) | (0xFF & buf[0]);
					buf = buf + 1;
				}

				bound = (range >> kNumBitModelTotalBits) * ttt;

				if(code < bound)
				{
					range = bound;
					symbol = (symbol + symbol);
					offs = offs & ~bit;
				}
				else
				{
					range = range - bound;
					code = code - bound;
					symbol = (symbol + symbol) + 1;
					offs = offs & bit;
				}
			} while(symbol < 0x100);
		}

		res = DUMMY_LIT;
	}
	else
	{
		range = range - bound;
		code = code - bound;
		p = probs;
		prob = p + 4 * (IsRep + state);
		ttt = prob[0];

		if(range < kTopValue)
		{
			if(buf >= bufLimit)
			{
				return DUMMY_ERROR;
			}

			range = range << 8;
			code = (code << 8) | (0xFF & buf[0]);
			buf = buf + 1;
		}

		bound = (range >> kNumBitModelTotalBits) * ttt;

		if(code < bound)
		{
			range = bound;
			state = 0;
			p = probs;
			prob = p + 4 * LenCoder;
			res = DUMMY_MATCH;
		}
		else
		{
			range = range - bound;
			code = code - bound;
			res = DUMMY_REP;
			p = probs;
			prob = p + 4 * (IsRepG0 + state);
			ttt = prob[0];

			if(range < kTopValue)
			{
				if(buf >= bufLimit)
				{
					return DUMMY_ERROR;
				}

				range = range << 8;
				code = (code << 8) | (0xFF & buf[0]);
				buf = buf + 1;
			}

			bound = (range >> kNumBitModelTotalBits) * ttt;

			if(code < bound)
			{
				range = bound;
				p = probs;
				prob = p + 4 * (IsRep0Long + (state << kNumPosBitsMax) + posState);
				ttt = prob[0];

				if(range < kTopValue)
				{
					if(buf >= bufLimit)
					{
						return DUMMY_ERROR;
					}

					range = range << 8;
					code = (code << 8) | (0xFF & buf[0]);
					buf = buf + 1;
				}

				bound = (range >> kNumBitModelTotalBits) * ttt;

				if(code < bound)
				{
					range = bound;

					if(range < kTopValue)
					{
						if(buf >= bufLimit)
						{
							return DUMMY_ERROR;
						}

						range = range << 8;
						code = (code << 8) | (0xFF & buf[0]);
						buf = buf + 1;
					}

					return DUMMY_REP;
				}
				else
				{
					range = range - bound;
					code = code - bound;
				}
			}
			else
			{
				range = range - bound;
				code = code - bound;
				p = probs;
				prob = p + 4 * (IsRepG1 + state);
				ttt = prob[0];

				if(range < kTopValue)
				{
					if(buf >= bufLimit)
					{
						return DUMMY_ERROR;
					}

					range = range << 8;
					code = (code << 8) | (0xFF & buf[0]);
					buf = buf + 1;
				}

				bound = (range >> kNumBitModelTotalBits) * ttt;

				if(code < bound)
				{
					range = bound;
				}
				else
				{
					range = range - bound;
					code = code - bound;
					p = probs;
					prob = p + 4 * (IsRepG2 + state);
					ttt = prob[0];

					if(range < kTopValue)
					{
						if(buf >= bufLimit)
						{
							return DUMMY_ERROR;
						}

						range = range << 8;
						code = (code << 8) | (0xFF & buf[0]);
						buf = buf + 1;
					}

					bound = (range >> kNumBitModelTotalBits) * ttt;

					if(code < bound)
					{
						range = bound;
					}
					else
					{
						range = range - bound;
						code = code - bound;
					}
				}
			}

			state = kNumStates;
			p = probs;
			prob = p + 4 * RepLenCoder;
		}

		p = prob;
		probLen = p + 4 * LenChoice;
		ttt = probLen[0];

		if(range < kTopValue)
		{
			if(buf >= bufLimit)
			{
				return DUMMY_ERROR;
			}

			range = range << 8;
			code = (code << 8) | (0xFF & buf[0]);
			buf = buf + 1;
		}

		bound = (range >> kNumBitModelTotalBits) * ttt;

		if(code < bound)
		{
			range = bound;
			p = prob;
			probLen = p + 4 * (LenLow + (posState << kLenNumLowBits));
			offset = 0;
			limit = 1 << kLenNumLowBits;
		}
		else
		{
			range = range - bound;
			code = code - bound;
			p = prob;
			probLen = p + 4 * LenChoice2;
			ttt = probLen[0];

			if(range < kTopValue)
			{
				if(buf >= bufLimit)
				{
					return DUMMY_ERROR;
				}

				range = range << 8;
				code = (code << 8) | (0xFF & buf[0]);
				buf = buf + 1;
			}

			bound = (range >> kNumBitModelTotalBits) * ttt;

			if(code < bound)
			{
				range = bound;
				p = prob;
				probLen = p + 4 * (LenMid + (posState << kLenNumMidBits));
				offset = kLenNumLowSymbols;
				limit = 1 << kLenNumMidBits;
			}
			else
			{
				range = range - bound;
				code = code - bound;
				probLen = p + 4 * LenHigh;
				offset = kLenNumLowSymbols + kLenNumMidSymbols;
				limit = 1 << kLenNumHighBits;
			}
		}

		len = 1;

		do
		{
			ttt = probLen[len];

			if(range < kTopValue)
			{
				if(buf >= bufLimit)
				{
					return DUMMY_ERROR;
				}

				range = range << 8;
				code = (code << 8) | (0xFF & buf[0]);
				buf = buf + 1;
			}

			bound = (range >> kNumBitModelTotalBits) * ttt;

			if(code < bound)
			{
				range = bound;
				len = (len + len);
			}
			else
			{
				range = range - bound;
				code = code - bound;
				len = (len + len) + 1;
			}
		} while(len < limit);

		len = len - limit + offset;

		if(state < 4)
		{
			if(len < kNumLenToPosStates) hold = len << kNumPosSlotBits;
			else hold = (kNumLenToPosStates - 1) << kNumPosSlotBits;

			p = probs;
			prob = p + 4 * (PosSlot + hold);
			posSlot = 1;

			do
			{
				ttt = prob[posSlot];

				if(range < kTopValue)
				{
					if(buf >= bufLimit)
					{
						return DUMMY_ERROR;
					}

					range = range << 8;
					code = (code << 8) | (0xFF & buf[0]);
					buf = buf + 1;
				}

				bound = (range >> kNumBitModelTotalBits) * ttt;

				if(code < bound)
				{
					range = bound;
					posSlot = (posSlot + posSlot);
				}
				else
				{
					range = range - bound;
					code = code - bound;
					posSlot = (posSlot + posSlot) + 1;
				}
			} while(posSlot < (1 << kNumPosSlotBits));

			posSlot = posSlot - (1 << kNumPosSlotBits);

			if(posSlot >= kStartPosModelIndex)
			{
				numDirectBits = ((posSlot >> 1) - 1);

				if(posSlot < kEndPosModelIndex)
				{
					p = probs;
					prob = p + 4 * (SpecPos + ((2 | (posSlot & 1)) << numDirectBits) - posSlot - 1);
				}
				else
				{
					numDirectBits = numDirectBits - kNumAlignBits;

					do
					{
						if(range < kTopValue)
						{
							if(buf >= bufLimit)
							{
								return DUMMY_ERROR;
							}

							range = range << 8;
							code = (code << 8) | (0xFF & buf[0]);
							buf = buf + 1;
						}

						range = range >> 1;
						code = code - (range & ((((code - range) >> 31) & 1) - 1));
						numDirectBits = numDirectBits - 1;
					} while(numDirectBits != 0);

					p = probs;
					prob = p + 4 * Align;
					numDirectBits = kNumAlignBits;
				}

				i = 1;

				do
				{
					ttt = prob[i];

					if(range < kTopValue)
					{
						if(buf >= bufLimit)
						{
							return DUMMY_ERROR;
						}

						range = range << 8;
						code = (code << 8) | (0xFF & buf[0]);
						buf = buf + 1;
					}

					bound = (range >> kNumBitModelTotalBits) * ttt;

					if(code < bound)
					{
						range = bound;
						i = (i + i);
					}
					else
					{
						range = range - bound;
						code = code - bound;
						i = (i + i) + 1;
					}
					numDirectBits = numDirectBits - 1;
				} while(numDirectBits != 0);
			}
		}
	}

	if(range < kTopValue)
	{
		if(buf >= bufLimit)
		{
			return DUMMY_ERROR;
		}

		/* is this even needed? */
		range = range << 8;
		code = (code << 8) | (0xFF & buf[0]);
		buf = buf + 1;
	}

	return res;
}


void LzmaDec_InitRc(uint8_t* data)
{
	global->code = ((0xFF & data[1]) << 24) | ((0xFF & data[2]) << 16) | ((0xFF & data[3]) << 8) | (0xFF & data[4]);
	global->range = BITS32;
	global->needFlush = FALSE;
}

void LzmaDec_InitDicAndState(int initDic, int initState)
{
	global->needFlush = TRUE;
	global->remainLen = 0;
	global->tempBufSize = 0;

	if(initDic)
	{
		global->processedPos = 0;
		global->checkDicSize = 0;
		global->needInitLzma = TRUE;
	}

	if(initState)
	{
		global->needInitLzma = TRUE;
	}
}

void LzmaDec_InitStateReal()
{
	uint32_t numProbs = Literal + (LZMA_LIT_SIZE << (global->lc + global->lp));
	uint32_t i;
	uint32_t* probs = global->probs;

	for(i = 0; i < numProbs; i = i + 1)
	{
		probs[i] = (BITS32 & (kBitModelTotal >> 1)) | (HIGHBITS & probs[i]);
	}

	global->reps[0] = 1; global->reps[1] = 1; global->reps[2] = 1; global->reps[3] = 1;
	global->state = 0;
	global->needInitLzma = FALSE;
}

uint32_t LzmaDec_DecodeToDic(uint8_t* src, uint32_t srcLen)
{
	uint32_t srcLen0 = srcLen;
	uint32_t inSize = srcLen;
	int checkEndMarkNow;
	uint32_t processed;
	uint8_t *bufLimit;
	uint32_t dummyRes;
	uint32_t rem;
	uint32_t lookAhead;

	srcLen = 0;
	LzmaDec_WriteRem(global->dicfLimit);

	while(global->remainLen != kMatchSpecLenStart)
	{
		if(global->needFlush)
		{
			while(inSize > 0 && global->tempBufSize < RC_INIT_SIZE)
			{
				global->tempBuf[global->tempBufSize] = 0xFF & src[0];
				global->tempBufSize = global->tempBufSize + 1;
				src = src + 1;
				srcLen = srcLen + 1;
				inSize = inSize - 1;
			}

			if(global->tempBufSize < RC_INIT_SIZE)
			{
				if(srcLen != srcLen0) return SZ_ERROR_NEEDS_MORE_INPUT_PARTIAL;
				return SZ_ERROR_NEEDS_MORE_INPUT;
			}

			if((0xFF & global->tempBuf[0]) != 0) return SZ_ERROR_DATA;

			LzmaDec_InitRc(global->tempBuf);
			global->tempBufSize = 0;
		}

		checkEndMarkNow = FALSE;

		if(global->dicfPos >= global->dicfLimit)
		{
			if((global->remainLen == 0) && (global->code == 0))
			{
				if(srcLen != srcLen0) return SZ_ERROR_CHUNK_NOT_CONSUMED;
				return SZ_OK /* MAYBE_FINISHED_WITHOUT_MARK */;
			}

			if(global->remainLen != 0) return SZ_ERROR_NOT_FINISHED;
			checkEndMarkNow = TRUE;
		}

		if(global->needInitLzma) LzmaDec_InitStateReal();

		if(global->tempBufSize == 0)
		{

			if(inSize < LZMA_REQUIRED_INPUT_MAX || checkEndMarkNow)
			{
				dummyRes = LzmaDec_TryDummy(src, inSize);

				if(dummyRes == DUMMY_ERROR)
				{
					memcpy(global->tempBuf, src, inSize);
					global->tempBufSize = inSize;
					srcLen += inSize;
					if(srcLen != srcLen0) return SZ_ERROR_NEEDS_MORE_INPUT_PARTIAL;
					return SZ_ERROR_NEEDS_MORE_INPUT;
				}

				if(checkEndMarkNow && dummyRes != DUMMY_MATCH) return SZ_ERROR_NOT_FINISHED;
				bufLimit = src;
			}
			else
			{
				bufLimit = src + inSize - LZMA_REQUIRED_INPUT_MAX;
			}

			global->buf = src;
			LzmaDec_DecodeReal2(global->dicfLimit, bufLimit);
			processed = (global->buf - src);
			srcLen = srcLen + processed;
			src = src + processed;
			inSize = inSize - processed;
		}
		else
		{
			rem = global->tempBufSize;
			lookAhead = 0;

			while((rem < LZMA_REQUIRED_INPUT_MAX) && (lookAhead < inSize))
			{
				global->tempBuf[rem] = 0xFF & src[lookAhead];
				rem = rem + 1;
				lookAhead = lookAhead + 1;
			}

			global->tempBufSize = rem;

			if(rem < LZMA_REQUIRED_INPUT_MAX || checkEndMarkNow)
			{
				dummyRes = LzmaDec_TryDummy(global->tempBuf, rem);

				if(dummyRes == DUMMY_ERROR)
				{
					srcLen = srcLen + lookAhead;
					if(srcLen != srcLen0) return SZ_ERROR_NEEDS_MORE_INPUT_PARTIAL;
					return SZ_ERROR_NEEDS_MORE_INPUT;
				}

				if(checkEndMarkNow && dummyRes != DUMMY_MATCH) return SZ_ERROR_NOT_FINISHED;
			}

			global->buf = global->tempBuf;
			LzmaDec_DecodeReal2(global->dicfLimit, global->buf);
			lookAhead = lookAhead - (rem - (global->buf - global->tempBuf));
			srcLen = srcLen + lookAhead;
			src = src + lookAhead;
			inSize = inSize - lookAhead;
			global->tempBufSize = 0;
		}
	}

	if(global->code != 0) return SZ_ERROR_DATA;
	return SZ_ERROR_FINISHED_WITH_MARK;
}



/* Tries to preread r bytes to the read buffer. Returns the number of bytes
 * available in the read buffer. If smaller than r, that indicates EOF.
 *
 * Doesn't try to preread more than absolutely necessary, to avoid copies in
 * the future.
 *
 * Works only if r <= sizeof(readBuf).
 */
uint32_t Preread(uint32_t r)
{
	uint32_t hold;
	uint32_t p = global->readEnd - global->readCur;
	require(r <= sizeof_readBuf, "r <= sizeof_readBuf");

	if(p < r)     /* Not enough pending available. */
	{
		if(global->readBuf + sizeof_readBuf - global->readCur + 0 < r)
		{
			/* If no room for r bytes to the end, discard bytes from the beginning. */
			global->readBuf = memmove(global->readBuf, global->readCur, p);
			global->readEnd = global->readBuf + p;
			global->readCur = global->readBuf;
		}

		while(p < r)
		{
			/* our single spot for reading input */
			hold = fgetc(source);
			pos = pos + 1;
			/* EOF or error on input. */
			if(EOF == hold) break;

			/* otherwise just add it */
			global->readEnd[0] = (0xFF & hold) | ((~0xFF) & global->readEnd[0]);
			global->readEnd = global->readEnd + 1;
			p = p + 1;
		}
	}

	return p;
}

void IgnoreVarint()
{
	while((0xFF & global->readCur[0]) >= 0x80)
	{
		global->readCur = global->readCur + 1;
	}
	global->readCur = global->readCur + 1;
}

uint32_t IgnoreZeroBytes(uint32_t c)
{
	while(c > 0)
	{
		if((0xFF & global->readCur[0]) != 0)
		{
			global->readCur = global->readCur + 1;
			return SZ_ERROR_BAD_PADDING;
		}
		global->readCur = global->readCur + 1;
		c = c - 1;
	}

	return SZ_OK;
}

uint32_t GetLE4(uint8_t *p)
{
	return (0xFF & p[0]) | (0xFF & p[1]) << 8 | (0xFF & p[2]) << 16 | (0xFF & p[3]) << 24;
}

/* Expects global->dicSize be set already. Can be called before or after InitProp. */
void InitDecode()
{
	/* global->lc = global->pb = global->lp = 0; */  /* needinitprop will initialize it */
	global->dicfLimit = 0;  /* We'll increment it later. */
	global->needInitDic = TRUE;
	global->needInitState = TRUE;
	global->needInitProp = TRUE;
	global->writtenPos = 0;
	global->writeRemaining = BITS32;
	global->discardedSize = 0;
	global->dicfPos = 0;
	LzmaDec_InitDicAndState(TRUE, TRUE);
}

uint32_t InitProp(uint8_t b)
{
	uint32_t lc;
	uint32_t lp;

	if(b >= (9 * 5 * 5))
	{
		return SZ_ERROR_BAD_LCLPPB_PROP;
	}

	lc = b % 9;
	b = b / 9;
	global->pb = b / 5;
	lp = b % 5;

	if(lc + lp > LZMA2_LCLP_MAX)
	{
		return SZ_ERROR_BAD_LCLPPB_PROP;
	}

	global->lc = lc;
	global->lp = lp;
	global->needInitProp = FALSE;
	return SZ_OK;
}


/* Reads .xz or .lzma data from source, writes uncompressed bytes to destination,
 * uses CLzmaDec.dic. It verifies some aspects of the file format (so it
 * can't be tricked to an infinite loop etc.), it doesn't verify checksums
 * (e.g. CRC32).
 */
uint32_t DecompressXzOrLzma()
{
	uint8_t checksumSize;
	/* Block header flags */
	uint32_t bhf;
	uint32_t result;
	/* uncompressed chunk size*/
	uint32_t us;

	/* needed by lzma */
	uint32_t srcLen;
	uint32_t res;

	/* needed by xz */
	uint8_t blockSizePad;
	uint32_t bhs;
	uint32_t bhs2;
	uint8_t dicSizeProp;
	uint8_t* readAtBlock;
	uint8_t control;
	uint8_t numRecords;
	/* compressed chunk size */
	uint32_t cs;
	int initDic;
	uint8_t mode;
	int initState;
	int isProp;

	/* 12 for the stream header + 12 for the first block header + 6 for the
	 * first chunk header. empty.xz is 32 bytes.
	 */
	if(Preread(12 + 12 + 6) < 12 + 12 + 6)
	{
		return SZ_ERROR_INPUT_EOF;
	}

	/* readbuf[7] is actually stream flags, should also be 0. */
	if(0 != memcmp(global->readCur, "\xFD""7zXZ\0", 7))
	{
		/* sanity check for lzma */
		require((0xFF & global->readCur[0]) <= 225, "lzma check 1 failed");
		require((0xFF & global->readCur[13]) == 0, "lzma check 2 failed");
		require((((bhf = GetLE4(global->readCur + 9)) == 0) || (bhf == BITS32)), "lzma check 3 failed");
		require((global->dicSize = GetLE4(global->readCur + 1)) >= LZMA_DIC_MIN, "lzma check 4 failed");

		/* Based on https://svn.python.org/projects/external/xz-5.0.3/doc/lzma-file-format.txt */
		/* TODO(pts): Support 8-byte uncompressed size. */
		if(bhf == 0) us = GetLE4(global->readCur + 5);
		else us = bhf;

		if(global->dicSize > MAX_DIC_SIZE) return SZ_ERROR_UNSUPPORTED_DICTIONARY_SIZE;

		InitDecode();
		global->allocCapacity = 0;
		global->dicf = NULL;
		/* LZMA2 restricts lc + lp <= 4. LZMA requires lc + lp <= 12.
		 * We apply the LZMA2 restriction here (to save memory in
		 * CLzmaDec.probs), thus we are not able to extract some legitimate
		 * .lzma files.
		 */
		result = (InitProp(0xFF & global->readCur[0]));
		if(result != SZ_OK) return result;

		global->readCur = global->readCur + 13;  /* Start decompressing the 0 byte. */
		global->dicfLimit = global->writeRemaining;
		global->writeRemaining = us;

		if(us <= global->dicSize) GrowCapacity(us);

		while((global->discardedSize + global->dicfPos) != us)
		{

			if((srcLen = Preread(sizeof_readBuf)) == 0)
			{
				if(us != BITS32) return SZ_ERROR_INPUT_EOF;
				break;
			}

			res = LzmaDec_DecodeToDic(global->readCur, srcLen);
			global->readCur = global->readCur + srcLen;

			if(res == SZ_ERROR_FINISHED_WITH_MARK) break;

			if(res != SZ_ERROR_NEEDS_MORE_INPUT && res != SZ_OK) return res;
		}

		Flush();
		return SZ_OK;
	}

	global->allocCapacity = 0;
	global->dicf = NULL;

	while(TRUE)
	{
		/* Based on https://tukaani.org/xz/xz-file-format-1.0.4.txt */
		switch(0xFF & global->readCur[7])
		{
			/* None */
			case 0: checksumSize = 1;
					break;
			/* CRC32 */
			case 1: checksumSize = 4;
					break;
			/* CRC64, typical xz output. */
			case 4: checksumSize = 8;
					break;
			default: return SZ_ERROR_BAD_CHECKSUM_TYPE;
		}

		/* Also ignore the CRC32 after checksumSize. */
		global->readCur = global->readCur + 12;

		while(TRUE)
		{
			/* We need it modulo 4, so a uint8_t is enough. */
			blockSizePad = 3;
			require(global->readEnd - global->readCur >= 12, "readEnd - readCur >= 12");  /* At least 12 bytes preread. */

			bhs = 0xFF & global->readCur[0];
			/* Last block, index follows. */
			if(bhs == 0)
			{
				global->readCur = global->readCur + 1;
				/* This is actually a varint, but it's shorter to read it as a byte. */
				numRecords = 0xFF & global->readCur[0];
				global->readCur = global->readCur + 1;
				while(0 != numRecords) {
					/* a varint is at most 9 bytes long, but may be shorter */
					Preread(9);
					IgnoreVarint();
					Preread(9);
					IgnoreVarint();
					numRecords = numRecords - 1;
				}
				/* Synchronize to 4-byte boundary */
				if (0 != ((pos - (global->readEnd - global->readCur)) & 3)) {
					Preread(4 - ((pos - (global->readEnd - global->readCur)) & 3));
					global->readCur = global->readCur + (4 - ((pos - (global->readEnd - global->readCur)) & 3));
				}
				/* Consume crc32 of index + stream footer */
				Preread(16);
				global->readCur = global->readCur + 16;
				break;
			}
			global->readCur = global->readCur + 1;

			/* Block header size includes the bhs field above and the CRC32 below. */
			bhs = (bhs + 1) << 2;

			/* Typically the Preread(12 + 12 + 6) above covers it. */
			if(Preread(bhs) < bhs)
			{
				return SZ_ERROR_INPUT_EOF;
			}

			readAtBlock = global->readCur;
			bhf = 0xFF & global->readCur[0];
			global->readCur = global->readCur + 1;

			if((bhf & 2) != 0) return SZ_ERROR_UNSUPPORTED_FILTER_COUNT;
			if((bhf & 20) != 0) return SZ_ERROR_BAD_BLOCK_FLAGS;
			/* Compressed size present. */
			/* Usually not present, just ignore it. */
			if((bhf & 64) != 0) IgnoreVarint();
			/* Uncompressed size present. */
			/* Usually not present, just ignore it. */
			if((bhf & 128) != 0) IgnoreVarint();

			/* This is actually a varint, but it's shorter to read it as a byte. */
			if((0xFF & global->readCur[0]) != FILTER_ID_LZMA2) return SZ_ERROR_UNSUPPORTED_FILTER_ID;
			global->readCur = global->readCur + 1;

			/* This is actually a varint, but it's shorter to read it as a byte. */
			if((0xFF & global->readCur[0]) != 1) return SZ_ERROR_UNSUPPORTED_FILTER_PROPERTIES_SIZE;
			global->readCur = global->readCur + 1;

			dicSizeProp = 0xFF & global->readCur[0];
			global->readCur = global->readCur + 1;

			/* Typical large dictionary sizes:
			 * 35: 805306368 bytes == 768 MiB
			 * 36: 1073741824 bytes == 1 GiB
			 * 37: 1610612736 bytes, largest supported by .xz
			 * 38: 2147483648 bytes == 2 GiB
			 * 39: 3221225472 bytes == 3 GiB
			 * 40: 4294967295 bytes, largest supported by .7z
			 */
			if(dicSizeProp > 40) return SZ_ERROR_BAD_DICTIONARY_SIZE;

			/* LZMA2 and .xz support it, we don't (for simpler memory management on
			 * 32-bit systems).
			 */
			if(dicSizeProp > MAX_DIC_SIZE_PROP) return SZ_ERROR_UNSUPPORTED_DICTIONARY_SIZE;

			/* Works if dicSizeProp <= 39. */
			global->dicSize = ((2 | ((dicSizeProp) & 1)) << ((dicSizeProp) / 2 + 11));
			/* TODO(pts): Free dic after use, also after realloc error. */
			require(global->dicSize >= LZMA_DIC_MIN, "global->dicSize >= LZMA_DIC_MIN");
			GrowCapacity(global->dicSize + MAX_MATCH_SIZE + sizeof_writeBuf);
			bhs2 = global->readCur - readAtBlock + 5;

			if(bhs2 > bhs) return SZ_ERROR_BLOCK_HEADER_TOO_LONG;

			result = IgnoreZeroBytes(bhs - bhs2);
			if(result != 0) return result;

			/* Ignore CRC32. */
			global->readCur = global->readCur + 4;
			/* Typically it's offset 24, xz creates it by default, minimal. */

			/* Finally Parse LZMA2 stream. */
			InitDecode();

			while(TRUE)
			{
				require(global->dicfPos == global->dicfLimit, "global->dicfPos == global->dicfLimit");

				/* Actually 2 bytes is enough to get to the index if everything is
				 * aligned and there is no block checksum.
				 */
				if(Preread(6) < 6) return SZ_ERROR_INPUT_EOF;
				control = 0xFF & global->readCur[0];

				if(control == 0)
				{
					global->readCur = global->readCur + 1;
					break;
				}
				else if(((control - 3) & 0xFF) < 0x7D) return SZ_ERROR_BAD_CHUNK_CONTROL_BYTE;

				us = ((0xFF & global->readCur[1]) << 8) + (0xFF & global->readCur[2]) + 1;

				/* Uncompressed chunk. */
				if(control < 3)
				{
					/* assume it was already setup */
					initDic = FALSE;
					cs = us;
					global->readCur = global->readCur + 3;
					blockSizePad = blockSizePad - 3;

					/* now test that assumption */
					if(control == 1)
					{
						global->needInitProp = global->needInitState;
						global->needInitState = TRUE;
						global->needInitDic = FALSE;
					}
					else if(global->needInitDic) return SZ_ERROR_DATA;

					LzmaDec_InitDicAndState(initDic, FALSE);
				}
				else
				{
					/* LZMA chunk. */
					mode = (((control) >> 5) & 3);
					if(mode == 3) initDic = TRUE;
					else initDic = FALSE;

					if(mode > 0) initState = TRUE;
					else initState = FALSE;

					if((control & 64) != 0) isProp = TRUE;
					else isProp = FALSE;

					us = us + ((control & 31) << 16);
					cs = ((0xFF & global->readCur[3]) << 8) + (0xFF & global->readCur[4]) + 1;

					if(isProp)
					{
						result = InitProp(0xFF & global->readCur[5]);
						if(result != 0) return result;

						global->readCur = global->readCur + 1;
						blockSizePad = blockSizePad - 1;
					}
					else if(global->needInitProp) return SZ_ERROR_MISSING_INITPROP;

					global->readCur = global->readCur + 5;
					blockSizePad = blockSizePad - 5;

					if((!initDic && global->needInitDic) || (!initState && global->needInitState))
					{
						return SZ_ERROR_DATA;
					}

					LzmaDec_InitDicAndState(initDic, initState);
					global->needInitDic = FALSE;
					global->needInitState = FALSE;
				}

				require(us <= (1 << 24), "us <= (1 << 24)");
				require(cs <= (1 << 16), "cs <= (1 << 16)");
				require(global->dicfPos == global->dicfLimit, "global->dicfPos == global->dicfLimit");
				FlushDiscardOldFromStartOfDic();
				global->dicfLimit = global->dicfLimit + us;

				if(global->dicfLimit < us) return SZ_ERROR_MEM;

				/* Read 6 extra bytes to optimize away a read(...) system call in
				 * the Prefetch(6) call in the next chunk header.
				 */
				if(Preread(cs + 6) < cs) return SZ_ERROR_INPUT_EOF;

				/* Uncompressed chunk, at most 64 KiB. */
				if(control < 3)
				{
					require((global->dicfPos + us) == global->dicfLimit, "global->dicfPos + us == global->dicfLimit");
					FlushDiscardGrowDic(us);
					memcpy(global->dicf + global->dicfPos, global->readCur, us);
					global->dicfPos = global->dicfPos + us;

					if((global->checkDicSize == 0) && ((global->dicSize - global->processedPos) <= us))
					{
						global->checkDicSize = global->dicSize;
					}

					global->processedPos = global->processedPos + us;
				}
				else
				{
					/* Compressed chunk. */
					/* This call doesn't change global->dicfLimit. */
					result = LzmaDec_DecodeToDic(global->readCur, cs);

					if(result != 0) return result;
				}

				if(global->dicfPos != global->dicfLimit) return SZ_ERROR_BAD_DICPOS;

				global->readCur = global->readCur + cs;
				blockSizePad = blockSizePad - cs;
				/* We can't discard decompressbuf[:global->dicfLimit] now,
				 * because we need it a dictionary in which subsequent calls to
				 * Lzma2Dec_DecodeToDic will look up backreferences.
				 */
			}

			Flush();
			/* End of LZMA2 stream. */

			/* End of block. */
			/* 7 for padding4 and CRC32 + 12 for the next block header + 6 for the next
			 * chunk header.
			 */
			if(Preread(7 + 12 + 6) < 7 + 12 + 6) return SZ_ERROR_INPUT_EOF;
			/* Ignore block padding. */
			result = (IgnoreZeroBytes(blockSizePad & 3));
			if(result != 0) return result;

			global->readCur = global->readCur + checksumSize;  /* Ignore CRC32, CRC64 etc. */
		}

		/* Look for another concatenated stream */

		/* 12 for the stream header + 12 for the first block header + 6 for the
		 * first chunk header. empty.xz is 32 bytes.
		 */
		if(Preread(12 + 12 + 6) < 12 + 12 + 6)
		{
			break;
		}

		if(0 != memcmp(global->readCur, "\xFD""7zXZ\0", 7)) {
			break;
		}
	}

	/* The .xz input file continues with the index, which we ignore from here. */
	return SZ_OK;
}

int main(int argc, char **argv)
{
	uint32_t res;
	char* name;
	char* dest;
	FUZZING = FALSE;
	name = NULL;
	dest = NULL;
	pos = 0;

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
			fputs(" [--file $input.xz or --file $input.lzma] (or it'll read from stdin)\n", stderr);
			fputs(" [--output $output] (or it'll write to stdout)\n", stderr);
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

	if(NULL != name) source = fopen(name, "r");
	else source = stdin;
	if(NULL != dest) destination = fopen(dest, "w");
	else destination = stdout;

	if(FUZZING) destination = fopen("/dev/null", "w");
	global = calloc(1, sizeof(struct CLzmaDec));
	global->readBuf = calloc(sizeof_readBuf, sizeof(uint8_t));
	global->readCur = global->readBuf;
	global->readEnd = global->readBuf;
	global->allocCapacity = 0;
	global->dicSize = 0;
	res = DecompressXzOrLzma();
	free(global->dicf);  /* Pacify valgrind(1). */
	free(global->readBuf);
	free(global);
	return res;
}
