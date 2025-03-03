/* Copyright (C) 2016 Jeremiah Orians
 * Copyright (C) 2021 Andrius Štikonas <andrius@stikonas.eu>
 * This file is part of M2-Planet.
 *
 * M2-Planet is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * M2-Planet is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with M2-Planet.  If not, see <http://www.gnu.org/licenses/>.
 */

#include "cc.h"
char* env_lookup(char* variable);
char* int2str(int x, int base, int signed_p);

struct visited
{
	struct visited* prev;
	char* name;
};

/* Globals */
FILE* input;
struct token_list* token;
int line;
char* file;
struct visited* vision;

int previously_seen(char* s)
{
	struct visited* v = vision;
	while(NULL != v)
	{
		if(match(v->name, s)) return TRUE;
		v = v->prev;
	}
	return FALSE;
}

void just_seen(char* s)
{
	struct visited* hold = calloc(1, sizeof(struct visited));
	hold->prev = vision;
	hold->name = s;
	vision = hold;
}

int grab_byte()
{
	int c = fgetc(input);
	if(10 == c) line = line + 1;
	return c;
}

void push_byte(int c)
{
	hold_string[string_index] = c;
	string_index = string_index + 1;
	require(MAX_STRING > string_index, "Token exceeded MAX_STRING char limit\nuse --max-string number to increase\n");
}

int consume_byte(int c)
{
	push_byte(c);
	return grab_byte();
}

int preserve_string(int c)
{
	int frequent = c;
	int escape = FALSE;
	do
	{
		if(!escape && '\\' == c ) escape = TRUE;
		else escape = FALSE;
		c = consume_byte(c);
		require(EOF != c, "Unterminated string\n");
	} while(escape || (c != frequent));
	c = consume_byte(frequent);
	return c;
}


void copy_string(char* target, char* source, int max)
{
	int i = 0;
	while(0 != source[i])
	{
		target[i] = source[i];
		i = i + 1;
		if(i == max) break;
	}
}


int preserve_keyword(int c, char* S)
{
	while(in_set(c, S))
	{
		c = consume_byte(c);
	}
	return c;
}

void clear_string(char* s)
{
	int i = 0;
	while(0 != s[i])
	{
		s[i] = 0;
		i = i + 1;
		require(i < MAX_STRING, "string exceeded max string size while clearing string\n");
	}
}

void reset_hold_string()
{
	clear_string(hold_string);
	string_index = 0;
}


/* note if this is the first token in the list, head needs fixing up */
struct token_list* eat_token(struct token_list* token)
{
	if(NULL != token->prev)
	{
		token->prev->next = token->next;
	}

	/* update backlinks */
	if(NULL != token->next)
	{
		token->next->prev = token->prev;
	}

	return token->next;
}


void new_token(char* s, int size)
{
	struct token_list* current = calloc(1, sizeof(struct token_list));
	require(NULL != current, "Exhausted memory while getting token\n");

	/* More efficiently allocate memory for string */
	current->s = calloc(size, sizeof(char));
	require(NULL != current->s, "Exhausted memory while trying to copy a token\n");
	copy_string(current->s, s, MAX_STRING);

	current->prev = token;
	current->next = token;
	current->linenumber = line;
	current->filename = file;
	token = current;
}

int get_token(int c)
{
	reset_hold_string();

	if(c == EOF)
	{
		return c;
	}
	else if((32 == c) || (9 == c) || (c == '\n'))
	{
		c = consume_byte(c);
	}
	else if('#' == c)
	{
		c = consume_byte(c);
		c = preserve_keyword(c, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_");
	}
	else if(in_set(c, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"))
	{
		c = preserve_keyword(c, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_:");
	}
	else if(in_set(c, "<=>|&!^%"))
	{
		c = preserve_keyword(c, "<=>|&!^%");
	}
	else if(in_set(c, "'\""))
	{
		c = preserve_string(c);
	}
	else if(c == '/')
	{
		c = consume_byte(c);
		if(c == '*')
		{
			c = consume_byte(c);
			while(c != '/')
			{
				while(c != '*')
				{
					c = consume_byte(c);
					require(EOF != c, "Hit EOF inside of block comment\n");
				}
				c = consume_byte(c);
				require(EOF != c, "Hit EOF inside of block comment\n");
			}
			c = consume_byte(c);
		}
		else if(c == '/')
		{
			while(c != '\n')
			{
				c = consume_byte(c);
				require(EOF != c, "Hit EOF inside of line comment\n");
			}
			c = consume_byte(c);
		}
		else if(c == '=')
		{
			c = consume_byte(c);
		}
	}
	else if(c == '*')
	{
		c = consume_byte(c);
		if(c == '=')
		{
			c = consume_byte(c);
		}
	}
	else if(c == '+')
	{
		c = consume_byte(c);
		if(c == '=')
		{
			c = consume_byte(c);
		}
		if(c == '+')
		{
			c = consume_byte(c);
		}
	}
	else if(c == '-')
	{
		c = consume_byte(c);
		if(c == '=')
		{
			c = consume_byte(c);
		}
		if(c == '>')
		{
			c = consume_byte(c);
		}
		if(c == '-')
		{
			c = consume_byte(c);
		}
	}
	else
	{
		c = consume_byte(c);
	}

	return c;
}

struct token_list* reverse_list(struct token_list* head)
{
	struct token_list* root = NULL;
	struct token_list* next;
	while(NULL != head)
	{
		next = head->next;
		head->next = root;
		root = head;
		head = next;
	}
	return root;
}

int read_include(int c)
{
	reset_hold_string();
	int done = FALSE;
	int ch;

	while(!done)
	{
		if(c == EOF)
		{
			fputs("we don't support EOF as a filename in #include statements\n", stderr);
			exit(EXIT_FAILURE);
		}
		else if((32 == c) || (9 == c) || (c == '\n'))
		{
			c = grab_byte();
		}
		else if(('"' == c) || ('<' == c))
		{
			if('<' == c) c = '>';
			ch = c;
			do
			{
				c = consume_byte(c);
				require(EOF != c, "Unterminated filename in #include\n");
			} while(c != ch);
			if('>' == ch) hold_string[0] = '<';
			done = TRUE;
		}
	}

	return c;
}

void insert_file_header(char* name, int line)
{
	char* hold_line = int2str(line, 10, FALSE);
	reset_hold_string();
	strcat(hold_string, "// #FILENAME ");
	strcat(hold_string, name);
	strcat(hold_string, " ");
	strcat(hold_string, hold_line);
	new_token(hold_string, strlen(hold_string)+2);
	new_token("\n", 3);
}

struct token_list* read_all_tokens(FILE* a, struct token_list* current, char* filename, int include);
int include_file(int ch, int include_file)
{
	/* The old state to restore to */
	char* hold_filename = file;
	FILE* hold_input = input;
	int hold_number;

	/* The new file to load */
	char* new_filename;
	FILE* new_file;

	require(EOF != ch, "#include failed to receive filename\n");
	/* Remove the #include */
	token = token->next;

	/* Get new filename */
	read_include(ch);
	/* with just a little extra to put in the matching at the end */
	new_token(hold_string, string_index + 3);

	ch = '\n';
	new_filename = token->s;
	/* Remove name from stream */
	token = token->next;

	/* Try to open the file */
	if('<' == new_filename[0])
	{
		if(match("stdio.h", new_filename + 1)) STDIO_USED = TRUE;
		reset_hold_string();
		strcat(hold_string, M2LIBC_PATH);
		strcat(hold_string, "/");
		strcat(hold_string, new_filename + 1);
		strcat(new_filename, ">");
		if(match("Linux", OperatingSystem))
		{
			if(NULL == strstr(hold_string, "uefi"))
			{
				new_file = fopen(hold_string, "r");
			}
			else
			{
				puts("skipping:");
				puts(hold_string);
				return ch;
			}
		}
		else if(match("UEFI", OperatingSystem))
		{
			if(NULL == strstr(hold_string, "linux"))
			{
				new_file = fopen(hold_string, "r");
			}
			else
			{
				puts("skipping:");
				puts(hold_string);
				return ch;
			}
		}
		else
		{
			puts("unknown host");
			exit(EXIT_FAILURE);
		}
	}
	else
	{
		if(match("M2libc/bootstrappable.h", new_filename+1))
		{
			reset_hold_string();
			strcat(hold_string, M2LIBC_PATH);
			strcat(hold_string, "/bootstrappable.h");
			new_file = fopen(hold_string, "r");
		}
		else new_file = fopen(new_filename+1, "r");

		strcat(new_filename, "\"");
	}

	/* prevent multiple visits */
	if(previously_seen(new_filename)) return ch;
	just_seen(new_filename);

	/* special case this compatibility crap */
	if(match("\"../gcc_req.h\"", new_filename) || match("\"gcc_req.h\"", new_filename)) return ch;

	if(include_file)
	{
		fputs("reading file: ", stderr);
		fputs(new_filename, stderr);
		fputc('\n', stderr);
	}

	/* catch garbage input */
	if(NULL == new_file)
	{
		fputs("unable to read file: ", stderr);
		fputs(new_filename, stderr);
		fputs("\nAborting hard!\n", stderr);
		exit(EXIT_FAILURE);
	}

	/* protect our current line number */
	hold_number = line + 1;

	/* Read the new file */
	if(include_file) read_all_tokens(new_file, token, new_filename, include_file);

	/* put back old file info */
	insert_file_header(hold_filename, hold_number);

	/* resume reading old file */
	input = hold_input;
	line = hold_number;
	file = hold_filename;
	return ch;
}

struct token_list* read_all_tokens(FILE* a, struct token_list* current, char* filename, int include)
{
	token = current;
	insert_file_header(filename, 1);
	input  = a;
	line = 1;
	file = filename;
	int ch = grab_byte();
	while(EOF != ch)
	{
		ch = get_token(ch);
		new_token(hold_string, string_index + 2);
		if(match("#include", token->s)) ch = include_file(ch, include);
	}

	return token;
}
