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
#include <stdlib.h>
#include <string.h>
#include <getopt.h>

//CONSTANT max_string 4096
#define max_string 4096
//CONSTANT PROCESSED 1
#define PROCESSED 1
//CONSTANT STR 2
#define STR 2
//CONSTANT NEWLINE 3
#define NEWLINE 3

//CONSTANT TRUE 1
#define TRUE 1
//CONSTANT FALSE 0
#define FALSE 0

// CONSTANT KNIGHT 0
#define KNIGHT 0
// CONSTANT X86 1
#define X86 1
// CONSTANT AMD64 2
#define AMD64 2
// CONSTANT ARMV7L 40
#define ARMV7L 40
// CONSTANT AARM64 80
#define AARM64 80

// CONSTANT HEX 16
#define HEX 16
// CONSTANT OCTAL 8
#define OCTAL 8
// CONSTANT BINARY 2
#define BINARY 2


/* Imported functions */
int strtoint(char *a);
char* int2str(int x, int base, int signed_p);
int in_set(int c, char* s);
int match(char* a, char* b);
void require(int bool, char* error);

struct blob
{
	struct blob* next;
	int type;
	char* Text;
	char* Expression;
	struct blob* hash_next;
};

struct Token
{
	struct Token* next;
	struct blob* contents;
	char* filename;
	int linenumber;
};

/* Globals */
FILE* source_file;
FILE* destination_file;
int BigEndian;
int BigBitEndian;
int ByteMode;
int Architecture;
int linenumber;
struct Token* token_list;
struct blob* blob_list;
struct blob* define_blob;
struct blob* newline_blob;
int blob_count;
char* SCRATCH;
struct blob** hash_table;

void line_error(char* filename, int linenumber)
{
	fputs(filename, stderr);
	fputs(":", stderr);
	fputs(int2str(linenumber, 10, TRUE), stderr);
	fputs(" :", stderr);
}

void ClearScratch()
{
	int i = 0;
	int c = SCRATCH[i];
	while(0 != c)
	{
		SCRATCH[i] = 0;
		i = i + 1;
		c = SCRATCH[i];
	}
}

int GetHash(char* s)
{
	int i = 5381;
	while(0 != s[0])
	{
		i = (i << 5) + i + s[0];
		s = s + 1;
	}
	return i & 0xFFFF;
}

struct blob* FindBlob()
{
	int hash = GetHash(SCRATCH);
	struct blob* i = hash_table[hash];
	while(NULL != i)
	{
		if(match(SCRATCH, i->Text)) return i;
		i = i->hash_next;
	}

	return NULL;
}

void AddHash(struct blob* a, char* s)
{
	int i = GetHash(s);
	a->hash_next = hash_table[i];
	hash_table[i] = a;
}

void NewBlob(int size)
{
	blob_count = blob_count + 1;
	struct blob* a = calloc(1, sizeof(struct blob));
	require(NULL != a, "Exhusted available memory\n");
	a->Text = calloc(size + 1, sizeof(char));
	require(NULL != a->Text, "Exhusted available memory\n");

	int i = 0;
	while(i <= size)
	{
		a->Text[i] = SCRATCH[i];
		i = i + 1;
	}
	a->next = blob_list;
	blob_list = a;
	AddHash(a, SCRATCH);
}

struct Token* newToken(char* filename, int linenumber)
{
	struct Token* p;

	p = calloc (1, sizeof (struct Token));
	require(NULL != p, "Exhusted available memory\n");

	p->filename = filename;
	p->linenumber = linenumber;

	return p;
}

struct Token* reverse_list(struct Token* head)
{
	struct Token* root = NULL;
	struct Token* next;
	while(NULL != head)
	{
		next = head->next;
		head->next = root;
		root = head;
		head = next;
	}
	return root;
}

void purge_lineComment()
{
	int c = fgetc(source_file);
	while(!in_set(c, "\n\r"))
	{
		if(EOF == c) break;
		c = fgetc(source_file);
	}
}

struct Token* append_newline(struct Token* head, char* filename)
{
	linenumber = linenumber + 1;
	if(NULL == head) return NULL;
	if(NEWLINE == head->contents->type)
	{/* Don't waste whitespace*/
		return head;
	}

	struct Token* lf = newToken(filename, linenumber);
	lf->contents = newline_blob;
	lf->next = head;
	return lf;
}


struct Token* store_atom(struct Token* head, char c, char* filename)
{
	ClearScratch();
	int ch = c;
	int i = 0;
	do
	{
		SCRATCH[i] = ch;
		ch = fgetc(source_file);
		i = i + 1;
		require(i < max_string, "storing atom of size larger than max_string\n");
		if(EOF == ch) break;
	} while (!in_set(ch, "\t\n "));

	head->contents = FindBlob();
	if(NULL == head->contents)
	{
		NewBlob(i);
		head->contents = blob_list;
	}

	if('\n' == ch)
	{
		return append_newline(head, filename);
	}
	return head;
}

struct blob* store_string(char c, char* filename)
{
	ClearScratch();

	int ch = c;
	int i = 0;
	do
	{
		SCRATCH[i] = ch;
		i = i + 1;
		if('\n' == ch) linenumber = linenumber + 1;
		ch = fgetc(source_file);
		require(EOF != ch, "Unmatched \"!\n");

		if(max_string == i)
		{
			line_error(filename, linenumber);
			fputs("String: ", stderr);
			fputs(SCRATCH, stderr);
			fputs(" exceeds max string size\n", stderr);
			exit(EXIT_FAILURE);
		}
	} while(ch != c);

	struct blob* a = FindBlob();
	if(NULL == a)
	{
		NewBlob(i);
		a = blob_list;
		a->type = STR;
	}

	return a;
}

struct Token* Tokenize_Line(struct Token* head, char* filename)
{
	int c;
	struct Token* p;
	linenumber = 1;

	do
	{
restart:
		c = fgetc(source_file);

		if(in_set(c, ";#"))
		{
			purge_lineComment();
			head = append_newline(head, filename);
			goto restart;
		}

		if(in_set(c, "\t "))
		{
			goto restart;
		}

		if('\n' == c)
		{
			head = append_newline(head, filename);
			goto restart;
		}

		if(EOF == c)
		{
			head = append_newline(head, filename);
			goto done;
		}

		p = newToken(filename, linenumber);
		p->next = head;
		if(in_set(c, "'\""))
		{
			p->contents = store_string(c, filename);
		}
		else
		{
			p = store_atom(p, c, filename);
		}

		head = p;
	} while(TRUE);
done:
	return head;
}

void line_macro(struct Token* p)
{
	struct Token* i;
	for(i = p; NULL != i; i = i->next)
	{
		if(define_blob == i->contents)
		{
			require(NULL != i->next, "Macro name must exist\n");
			require(NULL != i->next->next, "Macro value must exist\n");
			if(PROCESSED == i->next->contents->type)
			{
				line_error(i->filename, i->linenumber);
				fputs("Multiple definitions for macro ", stderr);
				fputs(i->next->contents->Text, stderr);
				fputs("\n", stderr);
				exit(EXIT_FAILURE);
			}

			i->contents = newline_blob;

			if (STR == i->next->next->contents->type)
			{
				i->contents->Expression = i->next->next->contents->Text + 1;
			}
			else
			{
				i->next->contents->Expression = i->next->next->contents->Text;
			}
			i->next = i->next->next->next;
		}
	}
}


int string_length(char* a)
{
	int i = 0;
	while(0 != a[i]) i = i + 1;
	return i;
}


void hexify_string(struct blob* p)
{
	char* table = "0123456789ABCDEF";
	int i = string_length(p->Text);
	int size;

	if(HEX == ByteMode) size = (((i << 1) + i) + 12);
	else if(OCTAL == ByteMode) size = (i << 2) + 1;
	else if(BINARY == ByteMode) size = (i << 3) + i + 1;
	else size = 1;

	require(1 != size, "hexify_string lacked a valid bytemode\n");
	char* d = calloc(size, sizeof(char));
	require(NULL != d, "Exhusted available memory\n");
	p->Expression = d;
	char* S = p->Text;

	if((KNIGHT == Architecture) && (HEX == ByteMode))
	{
		i = (((((i - 1) >> 2) + 1) << 3) + i);
		while( 0 < i)
		{
			i = i - 1;
			d[i] = '0';
		}
	}

	if(HEX == ByteMode)
	{
		while(0 != S[0])
		{
			S = S + 1;
			d[0] = table[S[0] >> 4];
			d[1] = table[S[0] & 0xF];
			d[2] = ' ';
			d = d + 3;
		}
	}
	else if(OCTAL == ByteMode)
	{
		while(0 != S[0])
		{
			S = S + 1;
			d[0] = table[S[0] >> 6];
			d[1] = table[(S[0] >> 3) & 0x7];
			d[2] = table[S[0] & 0x7];
			d[3] = ' ';
			d = d + 4;
		}
	}
	else if(BINARY == ByteMode)
	{
		while(0 != S[0])
		{
			S = S + 1;
			d[0] = table[S[0] >> 7];
			d[1] = table[(S[0] >> 6) & 0x1];
			d[2] = table[(S[0] >> 5) & 0x1];
			d[3] = table[(S[0] >> 4) & 0x1];
			d[4] = table[(S[0] >> 3) & 0x1];
			d[5] = table[(S[0] >> 2) & 0x1];
			d[6] = table[(S[0] >> 1) & 0x1];
			d[7] = table[S[0] & 0x1];
			d[8] = ' ';
			d = d + 9;
		}
	}
}

void process_string(struct blob* p)
{
	struct blob* i;
	for(i = p; NULL != i; i = i->next)
	{
		if(STR == i->type)
		{
			if('\'' == i->Text[0])
			{
				i->Expression = i->Text + 1;
			}
			else if('"' == i->Text[0])
			{
				hexify_string(i);
			}
		}
	}
}

char* pad_nulls(int size, char* nil)
{
	if(0 == size) return nil;
	require(size > 0, "negative null padding not possible\n");
	if(HEX == ByteMode) size = size * 2;
	else if (OCTAL == ByteMode) size = size * 3;
	else if (BINARY == ByteMode) size = size * 8;

	char* s = calloc(size + 1, sizeof(char));
	require(NULL != s, "Exhusted available memory\n");

	int i = 0;
	while(i < size)
	{
		s[i] = '0';
		i = i + 1;
	}

	return s;
}

void preserve_other(struct blob* p)
{
	struct blob* i;
	char c;
	for(i = p; NULL != i; i = i->next)
	{
		if((NULL == i->Expression) && !(i->type & PROCESSED))
		{
			c = i->Text[0];

			if(in_set(c, "!@$~%&:^"))
			{
				i->Expression = i->Text;
			}
			else if('<' == c)
			{
				i->Expression = pad_nulls(strtoint(i->Text + 1), i->Text);
			}
		}
	}
}

void bound_values(int displacement, int number_of_bytes, int low, int high)
{
	if((high < displacement) || (displacement < low))
	{
		fputs("A displacement of ", stderr);
		fputs(int2str(displacement, 10, TRUE), stderr);
		fputs(" does not fit in ", stderr);
		fputs(int2str(number_of_bytes, 10, TRUE), stderr);
		fputs(" bytes\n", stderr);
		exit(EXIT_FAILURE);
	}
}

void range_check(int displacement, int number_of_bytes)
{
	if(4 == number_of_bytes) return;
	else if(3 == number_of_bytes)
	{
		bound_values(displacement, number_of_bytes, -8388608, 16777216);
		return;
	}
	else if(2 == number_of_bytes)
	{
		bound_values(displacement, number_of_bytes, -32768, 65535);
		return;
	}
	else if(1 == number_of_bytes)
	{
		bound_values(displacement, number_of_bytes, -128, 255);
		return;
	}

	fputs("Received an invalid number of bytes in range_check\n", stderr);
	exit(EXIT_FAILURE);
}

void reverseBitOrder(char* c)
{
	if(NULL == c) return;
	if(0 == c[1]) return;
	int hold = c[0];

	if(HEX == ByteMode)
	{
		c[0] = c[1];
		c[1] = hold;
		reverseBitOrder(c+2);
	}
	else if(OCTAL == ByteMode)
	{
		c[0] = c[2];
		c[2] = hold;
		reverseBitOrder(c+3);
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
		reverseBitOrder(c+8);
	}
}

void LittleEndian(char* start)
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

	if(BigBitEndian) reverseBitOrder(c);
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

char* express_number(int value, char c)
{
	char* ch = calloc(42, sizeof(char));
	require(NULL != ch, "Exhusted available memory\n");
	int size;
	int number_of_bytes;
	int shift;
	if('!' == c)
	{
		number_of_bytes = 1;
		value = value & 0xFF;
	}
	else if('@' == c)
	{
		number_of_bytes = 2;
		value = value & 0xFFFF;
	}
	else if('~' == c)
	{
		number_of_bytes = 3;
		value = value & 0xFFFFFF;
	}
	else if('%' == c)
	{
		number_of_bytes = 4;
		value = value & 0xFFFFFFFF;
	}
	else
	{
		fputs("Given symbol ", stderr);
		fputc(c, stderr);
		fputs(" to express immediate value ", stderr);
		fputs(int2str(value, 10, TRUE), stderr);
		fputc('\n', stderr);
		exit(EXIT_FAILURE);
	}

	range_check(value, number_of_bytes);

	if(HEX == ByteMode)
	{
		size = number_of_bytes * 2;
		shift = 4;
	}
	else if(OCTAL == ByteMode)
	{
		size = number_of_bytes * 3;
		shift = 3;
	}
	else if(BINARY == ByteMode)
	{
		size = number_of_bytes * 8;
		shift = 1;
	}
	else
	{
		fputs("Got invalid ByteMode in express_number\n", stderr);
		exit(EXIT_FAILURE);
	}

	stringify(ch, size, ByteMode, value, shift);

	if(!BigEndian) LittleEndian(ch);
	else if(!BigBitEndian) reverseBitOrder(ch);
	return ch;
}

void eval_immediates(struct blob* p)
{
	struct blob* i;
	int value;
	for(i = p; NULL != i; i = i->next)
	{
		if(PROCESSED == i->type) continue;
		else if(NEWLINE == i->type) continue;
		else if('<' == i->Text[0]) continue;
		else if(NULL == i->Expression)
		{
			if((X86 == Architecture) || (AMD64 == Architecture) || (ARMV7L == Architecture) || (AARM64 == Architecture))
			{
				if(in_set(i->Text[0], "%~@!"))
				{
					value = strtoint(i->Text + 1);

					if(('0' == i->Text[1]) || (0 != value))
					{
						i->Expression = express_number(value, i->Text[0]);
					}
				}
			}
			else if(KNIGHT == Architecture)
			{
				value = strtoint(i->Text);
				if(('0' == i->Text[0]) || (0 != value))
				{
					i->Expression = express_number(value, '@');
				}
			}
			else
			{
				fputs("Unknown architecture received in eval_immediates\n", stderr);
				exit(EXIT_FAILURE);
			}
		}
	}
}

void print_hex(struct Token* p)
{
	struct Token* i;
	for(i = p; NULL != i; i = i->next)
	{
		if(NEWLINE == i->contents->type)
		{
			if(NULL == i->next) fputc('\n', destination_file);
			else if(NEWLINE != i->next->contents->type) fputc('\n', destination_file);
		}
		else if(NULL != i->contents->Expression)
		{
			fputs(i->contents->Expression, destination_file);
			if(NEWLINE != i->next->contents->type) fputc(' ', destination_file);
		}
		else
		{
			line_error(i->filename, i->linenumber);
			fputs("Received invalid other; ", stderr);
			fputs(i->contents->Text, stderr);
			fputs("\n", stderr);
			exit(EXIT_FAILURE);
		}
	}
}

/* Standard C main program */
int main(int argc, char **argv)
{
	BigEndian = TRUE;
	Architecture = KNIGHT;
	destination_file = stdout;
	BigBitEndian = TRUE;
	ByteMode = HEX;
	char* filename;
	char* arch;
	blob_count = 2;
	hash_table = calloc(65537, sizeof(struct blob*));

	/* Create newline blob */
	newline_blob = calloc(1, sizeof(struct blob));
	newline_blob->Text = "\n";
	newline_blob->Expression = "\n";
	newline_blob->type = NEWLINE;
	AddHash(newline_blob, "\n");

	/* Start the blob list with DEFINE and newline */
	blob_list = calloc(1, sizeof(struct blob));
	blob_list->Text = "DEFINE";
	define_blob = blob_list;
	blob_list->next = newline_blob;
	AddHash(define_blob, "DEFINE");

	/* Initialize scratch */
	SCRATCH = calloc(max_string + 1, sizeof(char));

	int option_index = 1;
	while(option_index <= argc)
	{
		if(NULL == argv[option_index])
		{
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--BigEndian") || match(argv[option_index], "--big-endian"))
		{
			BigEndian = TRUE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "--LittleEndian") || match(argv[option_index], "--little-endian"))
		{
			BigEndian = FALSE;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-A") || match(argv[option_index], "--architecture"))
		{
			arch = argv[option_index + 1];
			if(match("knight-native", arch) || match("knight-posix", arch)) Architecture = KNIGHT;
			else if(match("x86", arch)) Architecture = X86;
			else if(match("amd64", arch)) Architecture = AMD64;
			else if(match("armv7l", arch)) Architecture = ARMV7L;
			else if(match("aarch64", arch)) Architecture = AARM64;
			else
			{
				fputs("Unknown architecture: ", stderr);
				fputs(arch, stderr);
				fputs(" know values are: knight-native, knight-posix, x86, amd64, armv7l and aarch64", stderr);
				exit(EXIT_FAILURE);
			}
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-b") || match(argv[option_index], "--binary"))
		{
			ByteMode = BINARY;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-h") || match(argv[option_index], "--help"))
		{
			fputs("Usage: ", stderr);
			fputs(argv[0], stderr);
			fputs(" --file FILENAME1 {-f FILENAME2} (--big-endian|--little-endian) ", stderr);
			fputs("[--architecture name]\nArchitectures: knight-native, knight-posix, x86, amd64 and armv7\n", stderr);
			fputs("To leverage octal or binary output: --octal, --binary\n", stderr);
			exit(EXIT_SUCCESS);
		}
		else if(match(argv[option_index], "-f") || match(argv[option_index], "--file"))
		{
			filename = argv[option_index + 1];
			source_file = fopen(filename, "r");

			if(NULL == source_file)
			{
				fputs("The file: ", stderr);
				fputs(argv[option_index + 1], stderr);
				fputs(" can not be opened!\n", stderr);
				exit(EXIT_FAILURE);
			}

			token_list = Tokenize_Line(token_list, filename);

			fclose(source_file);

			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-o") || match(argv[option_index], "--output"))
		{
			destination_file = fopen(argv[option_index + 1], "w");

			if(NULL == destination_file)
			{
				fputs("The file: ", stderr);
				fputs(argv[option_index + 1], stderr);
				fputs(" can not be opened!\n", stderr);
				exit(EXIT_FAILURE);
			}
			option_index = option_index + 2;
		}
		else if(match(argv[option_index], "-O") || match(argv[option_index], "--octal"))
		{
			ByteMode = OCTAL;
			option_index = option_index + 1;
		}
		else if(match(argv[option_index], "-V") || match(argv[option_index], "--version"))
		{
			fputs("M1 1.0.0\n", stdout);
			exit(EXIT_SUCCESS);
		}
		else
		{
			fputs("Unknown option\n", stderr);
			exit(EXIT_FAILURE);
		}
	}

	if(NULL == token_list)
	{
		fputs("Either no input files were given or they were empty\n", stderr);
		exit(EXIT_FAILURE);
	}

	token_list = reverse_list(token_list);
	line_macro(token_list);
	process_string(blob_list);
	eval_immediates(blob_list);
	preserve_other(blob_list);
	print_hex(token_list);

	if (destination_file != stdout)
	{
		fclose(destination_file);
	}

	return EXIT_SUCCESS;
}
