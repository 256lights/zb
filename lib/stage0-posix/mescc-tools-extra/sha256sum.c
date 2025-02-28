/* Copyright (C) 2021 Bastian Bittorf <bb@npl.de>
 * Copyright (C) 2021 Alain Mosnier <alain@wanamoon.net>
 * Copyright (C) 2017-2021 Jan Venekamp
 * Copyright (C) 2021 Jeremiah Orians
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

#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include "M2libc/bootstrappable.h"

#define CHUNK_SIZE 64
#define TOTAL_LEN_LEN 8

int mask;

/*
 * Initialize array of round constants:
 * (first 32 bits of the fractional parts of the cube roots of the first 64 primes 2..311):
 */
unsigned* init_k()
{
	unsigned* k = calloc(65, sizeof(unsigned));
	k[0] = 0x428a2f98;
	k[1] = 0x71374491;
	k[2] = 0xb5c0fbcf;
	k[3] = 0xe9b5dba5;
	k[4] = 0x3956c25b;
	k[5] = 0x59f111f1;
	k[6] = 0x923f82a4;
	k[7] = 0xab1c5ed5;
	k[8] = 0xd807aa98;
	k[9] = 0x12835b01;
	k[10] = 0x243185be;
	k[11] = 0x550c7dc3;
	k[12] = 0x72be5d74;
	k[13] = 0x80deb1fe;
	k[14] = 0x9bdc06a7;
	k[15] = 0xc19bf174;
	k[16] = 0xe49b69c1;
	k[17] = 0xefbe4786;
	k[18] = 0x0fc19dc6;
	k[19] = 0x240ca1cc;
	k[20] = 0x2de92c6f;
	k[21] = 0x4a7484aa;
	k[22] = 0x5cb0a9dc;
	k[23] = 0x76f988da;
	k[24] = 0x983e5152;
	k[25] = 0xa831c66d;
	k[26] = 0xb00327c8;
	k[27] = 0xbf597fc7;
	k[28] = 0xc6e00bf3;
	k[29] = 0xd5a79147;
	k[30] = 0x06ca6351;
	k[31] = 0x14292967;
	k[32] = 0x27b70a85;
	k[33] = 0x2e1b2138;
	k[34] = 0x4d2c6dfc;
	k[35] = 0x53380d13;
	k[36] = 0x650a7354;
	k[37] = 0x766a0abb;
	k[38] = 0x81c2c92e;
	k[39] = 0x92722c85;
	k[40] = 0xa2bfe8a1;
	k[41] = 0xa81a664b;
	k[42] = 0xc24b8b70;
	k[43] = 0xc76c51a3;
	k[44] = 0xd192e819;
	k[45] = 0xd6990624;
	k[46] = 0xf40e3585;
	k[47] = 0x106aa070;
	k[48] = 0x19a4c116;
	k[49] = 0x1e376c08;
	k[50] = 0x2748774c;
	k[51] = 0x34b0bcb5;
	k[52] = 0x391c0cb3;
	k[53] = 0x4ed8aa4a;
	k[54] = 0x5b9cca4f;
	k[55] = 0x682e6ff3;
	k[56] = 0x748f82ee;
	k[57] = 0x78a5636f;
	k[58] = 0x84c87814;
	k[59] = 0x8cc70208;
	k[60] = 0x90befffa;
	k[61] = 0xa4506ceb;
	k[62] = 0xbef9a3f7;
	k[63] = 0xc67178f2;
	return k;
}

unsigned* init_h()
{
	unsigned* h = calloc(9, sizeof(unsigned));
	h[0] = 0x6a09e667;
	h[1] = 0xbb67ae85;
	h[2] = 0x3c6ef372;
	h[3] = 0xa54ff53a;
	h[4] = 0x510e527f;
	h[5] = 0x9b05688c;
	h[6] = 0x1f83d9ab;
	h[7] = 0x5be0cd19;
	return h;
}

struct buffer_state
{
	char* p;
	size_t len;
	size_t total_len;
	int single_one_delivered; /* bool */
	int total_len_delivered; /* bool */
};

unsigned right_rot(unsigned value, unsigned count)
{
	/*
	 * Defined behaviour in standard C for all count where 0 < count < 32,
	 * which is what we need here.
	 */

	value &= mask;
	int hold1 = (value >> count) & mask;
	int hold2 = (value << (32 - count)) & mask;
	int hold = (hold1 | hold2) & mask;
	return hold;
}

void init_buf_state(struct buffer_state * state, char* input, size_t len)
{
	state->p = input;
	state->len = len;
	state->total_len = len;
	state->single_one_delivered = 0;
	state->total_len_delivered = 0;
}

/* Return value: bool */
int calc_chunk(char* chunk, struct buffer_state * state)
{
	size_t space_in_chunk;

	if(state->total_len_delivered)
	{
		return 0;
	}

	if(state->len >= CHUNK_SIZE)
	{
		memcpy(chunk, state->p, CHUNK_SIZE);
		state->p += CHUNK_SIZE;
		state->len -= CHUNK_SIZE;
		return 1;
	}

	memcpy(chunk, state->p, state->len);
	chunk += state->len;
	space_in_chunk = CHUNK_SIZE - state->len;
	state->p += state->len;
	state->len = 0;

	/* If we are here, space_in_chunk is one at minimum. */
	if(!state->single_one_delivered)
	{
		chunk[0] = 0x80;
		chunk += 1;
		space_in_chunk -= 1;
		state->single_one_delivered = 1;
	}

	/*
	 * Now:
	 * - either there is enough space left for the total length, and we can conclude,
	 * - or there is too little space left, and we have to pad the rest of this chunk with zeroes.
	 * In the latter case, we will conclude at the next invocation of this function.
	 */
	if(space_in_chunk >= TOTAL_LEN_LEN)
	{
		size_t left = space_in_chunk - TOTAL_LEN_LEN;
		size_t len = state->total_len;
		int i;
		memset(chunk, 0x00, left);
		chunk += left;
		/* Storing of len * 8 as a big endian 64-bit without overflow. */
		chunk[7] = (len << 3);
		len >>= 5;

		for(i = 6; i >= 0; i -= 1)
		{
			chunk[i] = len;
			len >>= 8;
		}

		state->total_len_delivered = 1;
	}
	else
	{
		memset(chunk, 0x00, space_in_chunk);
	}

	return 1;
}

/*
 * Limitations:
 * - Since input is a pointer in RAM, the data to hash should be in RAM, which could be a problem
 *   for large data sizes.
 * - SHA algorithms theoretically operate on bit strings. However, this implementation has no support
 *   for bit string lengths that are not multiples of eight, and it really operates on arrays of bytes.
 *   In particular, the len parameter is a number of bytes.
 */
void calc_sha_256(char* hash, char* input, size_t len)
{
	/*
	 * Note 1: All integers (expect indexes) are 32-bit unsigned integers and addition is calculated modulo 2^32.
	 * Note 2: For each round, there is one round constant k[i] and one entry in the message schedule array w[i], 0 = i = 63
	 * Note 3: The compression function uses 8 working variables, a through h
	 * Note 4: Big-endian convention is used when expressing the constants in this pseudocode,
	 *     and when parsing message block data from bytes to words, for example,
	 *     the first word of the input message "abc" after padding is 0x61626380
	 */
	/*
	 * Initialize hash values:
	 * (first 32 bits of the fractional parts of the square roots of the first 8 primes 2..19):
	 */
	unsigned* k = init_k();
	unsigned* h = init_h();
	unsigned i;
	unsigned j;
	unsigned hold1;
	unsigned hold2;
	/* 512-bit chunks is what we will operate on. */
	char* chunk = calloc(65, sizeof(char));
	struct buffer_state* state = calloc(1, sizeof(struct buffer_state));
	init_buf_state(state, input, len);
	unsigned* ah = calloc(9, sizeof(unsigned));
	char *p;
	unsigned* w = calloc(17, sizeof(unsigned));
	unsigned s0;
	unsigned s1;
	unsigned ch;
	unsigned temp1;
	unsigned temp2;
	unsigned maj;

	while(calc_chunk(chunk, state))
	{
		p = chunk;

		/* Initialize working variables to current hash value: */
		for(i = 0; i < 8; i += 1)
		{
			ah[i] = h[i];
		}

		/* Compression function main loop: */
		for(i = 0; i < 4; i += 1)
		{
			/*
			 * The w-array is really w[64], but since we only need
			 * 16 of them at a time, we save stack by calculating
			 * 16 at a time.
			 *
			 * This optimization was not there initially and the
			 * rest of the comments about w[64] are kept in their
			 * initial state.
			 */
			/*
			 * create a 64-entry message schedule array w[0..63] of 32-bit words
			 * (The initial values in w[0..63] don't matter, so many implementations zero them here)
			 * copy chunk into first 16 words w[0..15] of the message schedule array
			 */

			for(j = 0; j < 16; j += 1)
			{
				if(i == 0)
				{
					w[j] = ((p[0] & 0xFF) << 24) | ((p[1] & 0xFF) << 16) | ((p[2] & 0xFF) << 8) | (p[3] & 0xFF);
					p += 4;
				}
				else
				{
					/* Extend the first 16 words into the remaining 48 words w[16..63] of the message schedule array: */
					hold1 = (j + 1) & 0xf;
					hold2 = w[hold1];
					s0 = right_rot(hold2, 7) ^ right_rot(hold2, 18) ^ ((hold2 & mask) >> 3);

					hold1 = (j + 14) & 0xf;
					hold2 = w[hold1];
					s1 = right_rot(hold2, 17) ^ right_rot(hold2, 19) ^ ((hold2 & mask) >> 10);

					w[j] += s0 + w[(j + 9) & 0xf] + s1;
				}

				s1 = right_rot(ah[4], 6) ^ right_rot(ah[4], 11) ^ right_rot(ah[4], 25);
				ch = (ah[4] & ah[5]) ^ (~ah[4] & ah[6]);
				temp1 = ah[7] + s1 + ch + k[i << 4 | j] + w[j];
				s0 = right_rot(ah[0], 2) ^ right_rot(ah[0], 13) ^ right_rot(ah[0], 22);
				maj = (ah[0] & ah[1]) ^ (ah[0] & ah[2]) ^ (ah[1] & ah[2]);
				temp2 = s0 + maj;
				ah[7] = ah[6];
				ah[6] = ah[5];
				ah[5] = ah[4];
				ah[4] = ah[3] + temp1;
				ah[3] = ah[2];
				ah[2] = ah[1];
				ah[1] = ah[0];
				ah[0] = temp1 + temp2;
			}
		}

		/* Add the compressed chunk to the current hash value: */
		for(i = 0; i < 8; i +=  1)
		{
			h[i] += ah[i];
		}
	}

	/* Produce the final hash value (big-endian): */
	i = 0;
	for(j = 0; i < 8; i += 1)
	{
		hash[j] = ((h[i] >> 24) & 0xFF);
		j += 1;
		hash[j] = ((h[i] >> 16) & 0xFF);
		j += 1;
		hash[j] = ((h[i] >> 8) & 0xFF);
		j += 1;
		hash[j] = (h[i] & 0xFF);
		j += 1;
	}
}

struct list
{
	int found;
	char* name;
	FILE* f;
	size_t size;
	char* buffer;
	char* hash;
	struct list* next;
};

void bad_checkfile(char* filename)
{
	fputs(filename, stdout);
	puts(": no properly formatted SHA256 checksum lines found");
}

int hex2int(char c, char* filename)
{
	if((c >= '0') && (c <= '9')) return (c - 48);
	else if((c >= 'a') && (c <= 'f')) return (c - 87);
	else if ((c >= 'F') && (c <= 'F')) return (c - 55);
	bad_checkfile(filename);
	exit(EXIT_FAILURE);
}

char* hash_to_string(char* a)
{
	char* table = "0123456789abcdef";
	char* r = calloc(66, sizeof(char));
	int i;
	int j = 0;
	int c;
	for(i = 0; i < 32; i += 1)
	{
		c = a[i] & 0xFF;
		r[j] = table[(c >> 4)];
		j += 1;
		r[j] = table[(c & 0xF)];
		j += 1;
	}
	return r;
}

int check_file(char* b, char* filename)
{
	int r = TRUE;
	size_t i;
	int hold1;
	int hold2;
	FILE* f;
	char* name = calloc(4097, sizeof(char));
	char* hash = calloc(33, sizeof(char));
	char* hash2 = calloc(33, sizeof(char));
	size_t size;
	char* buffer;
go_again:
	for(i = 0; i < 32; i += 1)
	{
		hold1 = hex2int(b[0], filename);
		hold2 = hex2int(b[1], filename);
		hash[i] = (hold1 << 4) + hold2;
		b += 2;
	}

	if((' ' != b[0]) || (' ' != b[1]))
	{
		bad_checkfile(filename);
		exit(EXIT_FAILURE);
	}

	b += 2;
	for(i = 0; i < 4096; i += 1)
	{
		if('\n' == b[0])
		{
			name[i] = 0;
			b += 1;
			break;
		}
		name[i] = b[0];
		b += 1;
	}

	f = fopen(name, "r");
	if(NULL == f)
	{
		fputs(name, stdout);
		puts(": No such file or directory");
		exit(EXIT_FAILURE);
	}
	else
	{
		fseek(f, 0, SEEK_END);
		size = ftell(f);
		rewind(f);
		buffer = calloc(size + 1, sizeof(char));
		fread(buffer, sizeof(char), size, f);
		calc_sha_256(hash2, buffer, size);
		if(match(hash_to_string(hash), hash_to_string(hash2)))
		{
			fputs(name, stdout);
			puts(": OK");
		}
		else
		{
			fputs(name, stdout);
			fputs(": FAILED\nWanted:   ", stdout);
			fputs(hash_to_string(hash), stdout);
			fputs("\nReceived: ", stdout);
			puts(hash_to_string(hash2));
			r = FALSE;
		}
	}

	if(0 == b[0]) return r;
	goto go_again;
}

/* reverse the linked list */
void reverse(struct list** head)
{
	struct list* prev = NULL;
	struct list* current = *head;
	struct list* next = NULL;
	while (current != NULL)
	{
		next = current->next;
		current->next = prev;
		prev = current;
		current = next;
	}
	*head = prev;
}

int main(int argc, char **argv)
{
	struct list* l = NULL;
	struct list* t = NULL;
	size_t read;
	int check = FALSE;
	int r = TRUE;
	char* output_file = "";
	FILE* output = stdout;
	mask = (0x7FFFFFFF << 1) | 0x1;

	int i = 1;
	while(i <= argc)
	{
		if(NULL == argv[i])
		{
			i += 1;
		}
		else if(match(argv[i], "-c") || match(argv[i], "--check"))
		{
			check = TRUE;
			i += 1;
		}
		else if (match(argv[i], "-o") || match(argv[i], "--output"))
		{
			output_file = argv[i + 1];
			i += 2;
			if (output != stdout) {
				fclose(output);
			}
			output = fopen(output_file, "w");
			require(output != NULL, "Output file cannot be opened!\n");
		}
		else if(match(argv[i], "-h") || match(argv[i], "--help"))
		{
			puts("Usage: sha256sum <file> [--check]");
			exit(EXIT_SUCCESS);
		}
		else
		{
			t = calloc(1, sizeof(struct list));
			t->hash = calloc(33, sizeof(char));
			t->name = argv[i];
			t->f = fopen(t->name, "r");
			if(NULL != t->f)
			{
				t->found = TRUE;
				fseek(t->f, 0, SEEK_END);
				t->size = ftell(t->f);
				rewind(t->f);
				t->buffer = calloc(t->size + 1, sizeof(char));
				read = fread(t->buffer, sizeof(char), t->size, t->f);
			}
			t->next = l;
			l = t;
			i += 1;
		}
	}
	reverse(&l);

	if(check)
	{
		while(NULL != l)
		{
			if(l->found)
			{
				if(!check_file(l->buffer, l->name)) r = FALSE;
			}
			else
			{
				fputs(l->name, stdout);
				puts(": No such file or directory");
				exit(EXIT_FAILURE);
			}
			l = l->next;
		}
	}
	else
	{
		while(NULL != l)
		{
			if(l->found)
			{
				calc_sha_256(l->hash, l->buffer, l->size);
				fputs(hash_to_string(l->hash), output);
				fputs("  ", output);
				fputs(l->name, output);
				fputc('\n', output);
			}
			else
			{
				fputs(l->name, output);
				fputs(": No such file or directory\n", output);
				exit(EXIT_FAILURE);
			}
			l = l->next;
		}
	}
	if (output != stdout) {
		fclose(output);
	}

	if(r) return 0;
	else return 1;
}
