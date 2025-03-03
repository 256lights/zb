/* -*- c-file-style: "linux";indent-tabs-mode:t -*- */
/* Copyright (C) 2016 Jeremiah Orians
 * Copyright (C) 2017 Jan Nieuwenhuizen <janneke@gnu.org>
 * This file is part of mescc-tools.
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

// CONSTANT HEX 16
#define HEX 16
// CONSTANT OCTAL 8
#define OCTAL 8
// CONSTANT BINARY 2
#define BINARY 2

/***********************************************************
 * Needed for current implementation of little endian      *
 * Can be used to support little bit endian instruction    *
 * sets if we ever find one that might be useful           *
 * But I seriously doubt it                                *
 ***********************************************************/
void reverseBitOrder(char* c, int ByteMode)
{
	if(NULL == c) return;
	if(0 == c[1]) return;
	int hold = c[0];

	if(HEX == ByteMode)
	{
		c[0] = c[1];
		c[1] = hold;
		reverseBitOrder(c+2, ByteMode);
	}
	else if(OCTAL == ByteMode)
	{
		c[0] = c[2];
		c[2] = hold;
		reverseBitOrder(c+3, ByteMode);
	}
	else if(BINARY == ByteMode)
	{
		c[0] = c[7];
		c[7] = hold;
		hold = c[1];
		c[1] = c[6];
		c[6] = hold;
		hold = c[2];
		c[2] = c[5];
		c[5] = hold;
		hold = c[3];
		c[3] = c[4];
		c[4] = hold;
		reverseBitOrder(c+8, ByteMode);
	}
}

void LittleEndian(char* start, int ByteMode)
{
	char* end = start;
	char* c = start;
	while(0 != end[0]) end = end + 1;
	int hold;
	for(end = end - 1; start < end; start = start + 1)
	{
		hold = start[0];
		start[0] = end[0];
		end[0] = hold;
		end = end - 1;
	}

	/* The above makes a reversed bit order */
	reverseBitOrder(c, ByteMode);
}

int hex2char(int c)
{
	if((c >= 0) && (c <= 9)) return (c + 48);
	else if((c >= 10) && (c <= 15)) return (c + 55);
	else return -1;
}

int stringify(char* s, int digits, int divisor, int value, int shift)
{
	int i = value;
	if(digits > 1)
	{
		i = stringify(s+1, (digits - 1), divisor, value, shift);
	}
	s[0] = hex2char(i & (divisor - 1));
	return (i >> shift);
}
