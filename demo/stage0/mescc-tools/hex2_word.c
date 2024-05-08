/* -*- c-file-style: "linux";indent-tabs-mode:t -*- */
/* Copyright (C) 2017 Jeremiah Orians
 * Copyright (C) 2017 Jan Nieuwenhuizen <janneke@gnu.org>
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

#include "hex2_globals.h"

unsigned shiftregister;
unsigned tempword;
int updates;

void outOfRange(char* s, int value)
{
	line_error();
	fputs("error: value ", stderr);
	fputs(int2str(value, 10, TRUE), stderr);
	fputs(" out of range for field type ", stderr);
	fputs(s, stderr);
	fputs("\n", stderr);
	exit(EXIT_FAILURE);
}

void UpdateShiftRegister(char ch, int value)
{
	if ('.' == ch)
	{
		unsigned swap;
		/* Assume the user knows what they are doing */
		if(!BigEndian)
		{
			/* Swap from big-endian to little endian order */
			swap = (((value >> 24) & 0xFF) |
			        ((value << 8) & 0xFF0000) |
			        ((value >> 8) & 0xFF00) |
			        ((value & 0xFF) << 24));
		}
		else
		{
			/* Big endian needs no change */
			swap = value;
		}
		/* we just take the 4 bytes after the . and shove in the shift register */
		swap = swap & ((0xFFFF << 16) | 0xFFFF);
		shiftregister = shiftregister ^ swap;
	}
	else if ('!' == ch)
	{
		/* Corresponds to RISC-V I format */
		/* Will need architecture specific logic if more architectures go this route */
		/* no range check because it needs to work with labels for lui/addi + AUIPC combos */
		/* !label is used in the second instruction of AUIPC combo but we want an offset from */
		/* the first instruction */
		value = value + 4;
		tempword = (value & 0xFFF) << 20;
		/* Update shift register */
		tempword = tempword & ((0xFFFF << 16) | 0xFFFF);
		shiftregister = shiftregister ^ tempword;
	}
	else if ('@' == ch)
	{
		/* Corresponds to RISC-V B format (formerly known as SB) */
		/* Will need architecture specific logic if more architectures go this route */
		if ((value < -0x1000 || value > 0xFFF) || (value & 1)) outOfRange("B", value);

		/* Prepare the immediate's word */
		tempword = ((value & 0x1E) << 7)
			| ((value & 0x7E0) << (31 - 11))
			| ((value & 0x800) >> 4)
			| ((value & 0x1000) << (31 - 12));
		tempword = tempword & ((0xFFFF << 16) | 0xFFFF);
		/* Update shift register */
		shiftregister = shiftregister ^ tempword;
	}
	else if ('$' == ch)
	{
		/* Corresponds with RISC-V J format (formerly known as UJ) */
		/* Will need architecture specific logic if more architectures go this route */
		if ((value < -0x100000 || value > 0xFFFFF) || (value & 1)) outOfRange("J", value);

		tempword = ((value & 0x7FE) << (30 - 10))
			| ((value & 0x800) << (20 - 11))
			| ((value & 0xFF000))
			| ((value & 0x100000) << (31 - 20));
		tempword = tempword & ((0xFFFF << 16) | 0xFFFF);
		shiftregister = shiftregister ^ tempword;
	}
	else if ('~' == ch)
	{
		/* Corresponds with RISC-V U format */
		/* Will need architecture specific logic if more architectures go this route */
		if ((value & 0xFFF) < 0x800) tempword = value & (0xFFFFF << 12);
		else tempword = (value & (0xFFFFF << 12)) + 0x1000;
		tempword = tempword & ((0xFFFF << 16) | 0xFFFF);
		shiftregister = shiftregister ^ tempword;
	}
	else
	{
		line_error();
		fputs("error: UpdateShiftRegister reached impossible case: ch=", stderr);
		fputc(ch, stderr);
		fputs("\n", stderr);
		exit(EXIT_FAILURE);
	}
}

void WordStorePointer(char ch, FILE* source_file)
{
	/* Get string of pointer */
	ip = ip + 4;
	Clear_Scratch(scratch);
	int base_sep_p = consume_token(source_file);

	/* Lookup token */
	int target = GetTarget(scratch);
	int displacement;

	int base = ip;

	/* Change relative base address to :<base> */
	if ('>' == base_sep_p)
	{
		Clear_Scratch(scratch);
		consume_token (source_file);
		base = GetTarget (scratch);

		/* Force universality of behavior */
		displacement = (target - base);
	}
	else
	{
		displacement = Architectural_displacement(target, base);
	}

	/* output calculated difference */
	if('&' == ch) outputPointer(target, 4, TRUE); /* Deal with & */
	else if('%' == ch) outputPointer(displacement, 4, FALSE);  /* Deal with % */
	else
	{
		line_error();
		fputs("error: WordStorePointer reached impossible case: ch=", stderr);
		fputc(ch, stderr);
		fputs("\n", stderr);
		exit(EXIT_FAILURE);
	}
}

unsigned sr_nextb()
{
	unsigned rv = shiftregister & 0xff;
	shiftregister = shiftregister >> 8;
	return rv;
}

void DoByte(char c, FILE* source_file, int write, int update)
{
	if(HEX == ByteMode)
	{
		if(0 <= hex(c, source_file))
		{
			if(toggle)
			{
				if(write) fputc(((hold * 16)) + hex(c, source_file) ^ sr_nextb(), output);
				ip = ip + 1;
				if(update)
				{
					hold = (hold * 16) + hex(c, source_file);
					tempword = (tempword << 8) ^ hold;
					updates = updates + 1;
				}
				hold = 0;
			}
			else
			{
				hold = hex(c, source_file);
			}
			toggle = !toggle;
		}
	}
	else if(OCTAL ==ByteMode)
	{
		if(0 <= octal(c, source_file))
		{
			if(2 == toggle)
			{
				if(write) fputc(((hold * 8)) + octal(c, source_file) ^ sr_nextb(), output);
				ip = ip + 1;
				if(update)
				{
					hold = ((hold * 8) + octal(c, source_file));
					tempword = (tempword << 8) ^ hold;
					updates = updates + 1;
				}
				hold = 0;
				toggle = 0;
			}
			else if(1 == toggle)
			{
				hold = ((hold * 8) + octal(c, source_file));
				toggle = 2;
			}
			else
			{
				hold = octal(c, source_file);
				toggle = 1;
			}
		}
	}
	else if(BINARY == ByteMode)
	{
		if(0 <= binary(c, source_file))
		{
			if(7 == toggle)
			{
				if(write) fputc((hold * 2) + binary(c, source_file) ^ sr_nextb(), output);
				ip = ip + 1;
				if(update)
				{
					hold = ((hold * 2) + binary(c, source_file));
					tempword = (tempword << 8) ^ hold;
					updates = updates + 1;
				}
				hold = 0;
				toggle = 0;
			}
			else
			{
				hold = ((hold * 2) + binary(c, source_file));
				toggle = toggle + 1;
			}
		}
	}
}

void WordFirstPass(struct input_files* input)
{
	if(NULL == input) return;
	WordFirstPass(input->next);
	filename = input->filename;
	linenumber = 1;
	FILE* source_file = fopen(filename, "r");

	if(NULL == source_file)
	{
		fputs("The file: ", stderr);
		fputs(input->filename, stderr);
		fputs(" can not be opened!\n", stderr);
		exit(EXIT_FAILURE);
	}

	toggle = FALSE;
	int c;
	for(c = fgetc(source_file); EOF != c; c = fgetc(source_file))
	{
		/* Check for and deal with label */
		if(':' == c)
		{
			c = storeLabel(source_file, ip);
		}

		/* check for and deal with relative/absolute pointers to labels */
		if('.' == c)
		{
			/* Read architecture specific number of bytes for what is defined as a word */
			/* 4bytes in RISC-V's case */
			updates = 0;
			tempword = 0;
			while (updates < 4)
			{
				c = fgetc(source_file);
				DoByte(c, source_file, FALSE, TRUE);
			}
			ip = ip - 4;
		}
		else if(in_set(c, "!@$~"))
		{
			/* Don't update IP */
			c = Throwaway_token(source_file);
		}
		else if(in_set(c, "%&"))
		{
			ip = ip + 4;
			c = Throwaway_token(source_file);
			if ('>' == c)
			{ /* deal with label>base */
				c = Throwaway_token(source_file);
			}
		}
		else if('<' == c)
		{
			pad_to_align(FALSE);
		}
		else if('^' == c)
		{
			/* Just ignore */
			continue;
		}
		else DoByte(c, source_file, FALSE, FALSE);
	}
	fclose(source_file);
}

void WordSecondPass(struct input_files* input)
{
	shiftregister = 0;
	tempword = 0;

	if(NULL == input) return;
	WordSecondPass(input->next);
	filename = input->filename;
	linenumber = 1;
	FILE* source_file = fopen(filename, "r");

	/* Something that should never happen */
	if(NULL == source_file)
	{
		fputs("The file: ", stderr);
		fputs(input->filename, stderr);
		fputs(" can not be opened!\nWTF-pass2\n", stderr);
		exit(EXIT_FAILURE);
	}

	toggle = FALSE;
	hold = 0;

	int c;
	for(c = fgetc(source_file); EOF != c; c = fgetc(source_file))
	{
		if(':' == c) c = Throwaway_token(source_file); /* Deal with : */
		else if('.' == c)
		{
			/* Read architecture specific number of bytes for what is defined as a word */
			/* 4bytes in RISC-V's case */
			updates = 0;
			tempword = 0;
			while (updates < 4)
			{
				c = fgetc(source_file);
				DoByte(c, source_file, FALSE, TRUE);
			}
			UpdateShiftRegister('.', tempword);
			ip = ip - 4;
		}
		else if(in_set(c, "%&")) WordStorePointer(c, source_file);  /* Deal with % and & */
		else if(in_set(c, "!@$~"))
		{
			Clear_Scratch(scratch);
			consume_token(source_file);
			UpdateShiftRegister(c, Architectural_displacement(GetTarget(scratch), ip)); /* Play with shift register */
		}
		else if('<' == c) pad_to_align(TRUE);
		else if('^' == c) ALIGNED = TRUE;
		else DoByte(c, source_file, TRUE, FALSE);
	}

	fclose(source_file);
}
