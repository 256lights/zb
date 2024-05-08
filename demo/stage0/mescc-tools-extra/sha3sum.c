/*
 * Based on https://github.com/mjosaarinen/tiny_sha3/
 * Copyright (c) 2015 Markku-Juhani O. Saarinen <mjos@iki.fi>
 * Copyright (c) 2021 Michael Schierl <schierlm@gmx.de>
 *
 * This file is part of mescc-tools
 *
 * mescc-tools is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * mescc-tools is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with mescc-tools.  If not, see <http://www.gnu.org/licenses/>.
 */

#include <stdio.h>
#include <stdlib.h>
#include <stddef.h>
#include <stdint.h>

#include "M2libc/bootstrappable.h"

#define KECCAKF_ROUNDS 24

#if defined(__M2__)
#define uint32_t unsigned
#define uint8_t char
#endif

struct keccakf_const {
	uint32_t rndc1[24];
	uint32_t rndc2[24];
	int rotc[24];
	int piln[24];
};

/* init constants */
void keccakf_init(struct keccakf_const *kc)
{
	kc->rndc1[0] =  0x00000001; kc->rndc2[0] =  0x00000000; kc->rotc[0]  = 1;  kc->piln[0] = 10;
	kc->rndc1[1] =  0x00008082; kc->rndc2[1] =  0x00000000; kc->rotc[1]  = 3;  kc->piln[1] = 7;
	kc->rndc1[2] =  0x0000808a; kc->rndc2[2] =  0x80000000; kc->rotc[2]  = 6;  kc->piln[2] = 11;
	kc->rndc1[3] =  0x80008000; kc->rndc2[3] =  0x80000000; kc->rotc[3]  = 10; kc->piln[3] = 17;
	kc->rndc1[4] =  0x0000808b; kc->rndc2[4] =  0x00000000; kc->rotc[4]  = 15; kc->piln[4] = 18;
	kc->rndc1[5] =  0x80000001; kc->rndc2[5] =  0x00000000; kc->rotc[5]  = 21; kc->piln[5] = 3;
	kc->rndc1[6] =  0x80008081; kc->rndc2[6] =  0x80000000; kc->rotc[6]  = 28; kc->piln[6] = 5;
	kc->rndc1[7] =  0x00008009; kc->rndc2[7] =  0x80000000; kc->rotc[7]  = 36; kc->piln[7] = 16;
	kc->rndc1[8] =  0x0000008a; kc->rndc2[8] =  0x00000000; kc->rotc[8]  = 45; kc->piln[8] = 8;
	kc->rndc1[9] =  0x00000088; kc->rndc2[9] =  0x00000000; kc->rotc[9]  = 55; kc->piln[9] = 21;
	kc->rndc1[10] = 0x80008009; kc->rndc2[10] = 0x00000000; kc->rotc[10] = 2;  kc->piln[10] = 24;
	kc->rndc1[11] = 0x8000000a; kc->rndc2[11] = 0x00000000; kc->rotc[11] = 14; kc->piln[11] = 4;
	kc->rndc1[12] = 0x8000808b; kc->rndc2[12] = 0x00000000; kc->rotc[12] = 27; kc->piln[12] = 15;
	kc->rndc1[13] = 0x0000008b; kc->rndc2[13] = 0x80000000; kc->rotc[13] = 41; kc->piln[13] = 23;
	kc->rndc1[14] = 0x00008089; kc->rndc2[14] = 0x80000000; kc->rotc[14] = 56; kc->piln[14] = 19;
	kc->rndc1[15] = 0x00008003; kc->rndc2[15] = 0x80000000; kc->rotc[15] = 8;  kc->piln[15] = 13;
	kc->rndc1[16] = 0x00008002; kc->rndc2[16] = 0x80000000; kc->rotc[16] = 25; kc->piln[16] = 12;
	kc->rndc1[17] = 0x00000080; kc->rndc2[17] = 0x80000000; kc->rotc[17] = 43; kc->piln[17] = 2;
	kc->rndc1[18] = 0x0000800a; kc->rndc2[18] = 0x00000000; kc->rotc[18] = 62; kc->piln[18] = 20;
	kc->rndc1[19] = 0x8000000a; kc->rndc2[19] = 0x80000000; kc->rotc[19] = 18; kc->piln[19] = 14;
	kc->rndc1[20] = 0x80008081; kc->rndc2[20] = 0x80000000; kc->rotc[20] = 39; kc->piln[20] = 22;
	kc->rndc1[21] = 0x00008080; kc->rndc2[21] = 0x80000000; kc->rotc[21] = 61; kc->piln[21] = 9;
	kc->rndc1[22] = 0x80000001; kc->rndc2[22] = 0x00000000; kc->rotc[22] = 20; kc->piln[22] = 6;
	kc->rndc1[23] = 0x80008008; kc->rndc2[23] = 0x80000000; kc->rotc[23] = 44; kc->piln[23] = 1;
}

/* rotate val1 | (val2 << 32) left by howfar bits and return the least significant uint32_t */
uint32_t keccak_rotl64_0(uint32_t val1, uint32_t val2, int howfar)
{
	if (howfar < 32) {
		return (val1 << howfar) | (val2 >> (32 - howfar));
	} else {
		return (val2 << (howfar - 32)) | (val1 >> (64 - howfar));
	}
}

/* rotate val1 | (val2 << 32) left by howfar bits and return the most significant uint32_t */
uint32_t keccak_rotl64_1(uint32_t val1, uint32_t val2, int howfar)
{
	if (howfar < 32) {
		return (val1 >> (32 - howfar)) | (val2 << howfar);
	} else {
		return (val1 << (howfar - 32)) | (val2 >> (64 - howfar));
	}
}

uint32_t cast_uint32_t(uint8_t v)
{
	uint32_t r = v & 0xff;
	return r;
}

uint8_t cast_uint8_t(uint32_t v)
{
	uint8_t r = v & 0xff;
	return r;
}

uint8_t* cast_uint8_t_p(uint32_t* v)
{
#if defined(__M2__)
	uint8_t* r = v;
	return r;
#else
	return (uint8_t*) v;
#endif
}

/* Compression function */
void sha3_keccakf(struct keccakf_const* kc, uint32_t* st, uint32_t* bc)
{
	/* variables */
	int i;
	int j;
	int r;
	uint32_t hold;
	uint32_t t0;
	uint32_t t1;
	uint8_t* v;

	/* endianess conversion. this is redundant on little-endian targets */
	for (i = 0; i < 50; i = i + 1) {
		hold = st[i];
		v = cast_uint8_t_p(&hold);
		st[i] = cast_uint32_t(v[0])	 | (cast_uint32_t(v[1]) << 8) |
			(cast_uint32_t(v[2]) << 16) | (cast_uint32_t(v[3]) << 24);
	}

	/* actual iteration */
	for (r = 0; r < KECCAKF_ROUNDS; r = r + 1) {
		/* Theta */
		for (i = 0; i < 10; i = i + 1)
			bc[i] = st[i] ^ st[i + 10] ^ st[i + 20] ^ st[i + 30] ^ st[i + 40];

		for (i = 0; i < 5; i = i + 1) {
			t0 = bc[(((i + 4) % 5) * 2)] ^ keccak_rotl64_0(bc[(((i + 1) % 5) * 2)], bc[((((i + 1) % 5) * 2) + 1)], 1);
			t1 = bc[((((i + 4) % 5) * 2) + 1)] ^ keccak_rotl64_1(bc[(((i + 1) % 5) * 2)], bc[((((i + 1) % 5) * 2) + 1)], 1);
			for (j = 0; j < 25; j = j + 5) {
				st[((j + i) * 2)] = st[((j + i) * 2)] ^ t0;
				st[(((j + i) * 2) + 1)] = st[(((j + i) * 2) + 1)] ^ t1;
			}
		}

		/* Rho Pi */
		t0 = st[2];
		t1 = st[3];
		for (i = 0; i < 24; i = i + 1) {
			j = kc->piln[i];
			bc[0] = st[j*2];
			bc[1] = st[j*2+1];
			st[j*2] = keccak_rotl64_0(t0, t1, kc->rotc[i]);
			st[j*2+1] = keccak_rotl64_1(t0, t1, kc->rotc[i]);
			t0 = bc[0];
			t1 = bc[1];
		}

		/* Chi */
		for (j = 0; j < 25; j = j + 5) {
			for (i = 0; i < 10; i = i + 1)
				bc[i] = st[j * 2 + i];
			for (i = 0; i < 10; i = i + 1)
				st[j*2 + i] = st[j*2 + i] ^ ((~bc[(i + 2) % 10]) & bc[(i + 4) % 10]);
		}

		/* Iota */
		st[0] = st[0] ^ kc->rndc1[r];
		st[1] = st[1] ^ kc->rndc2[r];
	}

	/* endianess conversion. this is redundant on little-endian targets */
	for (i = 0; i < 50; i = i + 1) {
		hold = st[i];
		v = cast_uint8_t_p(&hold);
		t1 = st[i];
		v[0] = t1 & 0xFF;
		v[1] = (t1 >> 8) & 0xFF;
		v[2] = (t1 >> 16) & 0xFF;
		v[3] = (t1 >> 24) & 0xFF;
		st[i] = hold;
	}
}

/* main function */
int main(int argc, char **argv)
{
	int algorithm = 256;
	char* verify_hash = NULL;
	char* output_file = "";
	FILE* output = stdout;
	int option_index = 1;
	uint32_t* state = calloc(50, sizeof(uint32_t));
	uint8_t* st8 = cast_uint8_t_p(state);
	struct keccakf_const* kc = calloc(1, sizeof(struct keccakf_const));
	uint32_t* bc = calloc(10, sizeof(uint32_t));
	char* filename;
	FILE* ff;
	char* mdhex = calloc(1, 512 / 4 + 1);
	char* hextable = "0123456789abcdef";
	int c;
	int rsiz;
	int pt;
	int i;
	uint8_t v;

	keccakf_init(kc);

	while(option_index <= argc) {
		if (NULL == argv[option_index]) {
			option_index = option_index + 1;
		} else if (match(argv[option_index], "-a") || match(argv[option_index], "--algorithm")) {
			algorithm = strtoint(argv[option_index + 1]);
			option_index = option_index + 2;
			require(algorithm == 224 || algorithm == 256 || algorithm == 384 || algorithm == 512, "invalid bit length\n");
		} else if (match(argv[option_index], "-o") || match(argv[option_index], "--output")) {
			output_file = argv[option_index + 1];
			option_index = option_index + 2;
			if (output != stdout) {
				fclose(output);
			}
			output = fopen(output_file, "w");
			require(output != NULL, "Output file cannot be opened!\n");
		} else if (match(argv[option_index], "--verify")) {
			verify_hash = argv[option_index + 1];
			option_index = option_index + 2;
		} else if (match(argv[option_index], "-h") || match(argv[option_index], "--help")) {
			fputs("Usage: ", stderr);
			fputs(argv[0], stderr);
			fputs(" [--verify <hash>] [-a 224|256|384|512] [-o <outfile>] <file> ...\n", stderr);
			return 0;
		} else if (match(argv[option_index], "-V") || match(argv[option_index], "--version")) {
			fputs("sha3sum 1.3.0\n", stdout);
			return 0;
		} else {
			for (i = 0; i < 50; i = i + 1) {
				state[i] = 0;
			}
			rsiz = 200 - (algorithm / 4);
			filename = argv[option_index];
			option_index = option_index + 1;
			ff = fopen(filename, "rb");
			require(ff != NULL, "Input file cannot be opened!\n");
			pt = 0;
			while((c = fgetc(ff)) != EOF) {
				st8[pt] = ((st8[pt] & 0xff) ^ c) & 0xff;
				pt = pt + 1;
				if (pt >= rsiz) {
					sha3_keccakf(kc, state, bc);
					pt = 0;
				}
			}
			fclose(ff);
			st8[pt] = ((st8[pt] & 0xff)^ 0x06) & 0xff;
			st8[rsiz - 1] = ((st8[rsiz - 1]& 0xff) ^ 0x80) & 0xff;
			sha3_keccakf(kc, state, bc);
			for(i = 0; i < algorithm >> 3; i = i + 1) {
				v = st8[i] & 0xff;
				mdhex[i * 2] = hextable[v >> 4];
				mdhex[i * 2 + 1] = hextable[v & 0x0F];
			}
			mdhex[algorithm>>2] = '\0';
			fputs(mdhex, output);
			fputs("  ", output);
			fputs(filename, output);
			fputs("\n", output);
			if (verify_hash != NULL) {
				require(match(verify_hash, mdhex), "hashes do not match!\n");
			}
		}
	}
	if (output != stdout) {
		fclose(output);
	}
	return 0;
}
