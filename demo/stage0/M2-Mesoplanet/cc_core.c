/* Copyright (C) 2016 Jeremiah Orians
 * Copyright (C) 2018 Jan (janneke) Nieuwenhuizen <janneke@gnu.org>
 * Copyright (C) 2020 deesix <deesix@tuta.io>
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
#include "gcc_req.h"
#include <stdint.h>

/* Imported functions */
char* int2str(int x, int base, int signed_p);

void line_error_token(struct token_list *token)
{
	if(NULL == token)
	{
		fputs("EOF reached inside of line_error\n", stderr);
		fputs("problem at end of file\n", stderr);
		return;
	}
	fputs(token->filename, stderr);
	fputs(":", stderr);
	fputs(int2str(token->linenumber, 10, TRUE), stderr);
	fputs(":", stderr);
}

void line_error()
{
	line_error_token(global_token);
}

void require_match(char* message, char* required)
{
	require(NULL != global_token, "EOF reached inside of require match\n");
	if(!match(global_token->s, required))
	{
		line_error();
		fputs(message, stderr);
		exit(EXIT_FAILURE);
	}
	global_token = global_token->next;
	require(NULL != global_token, "EOF after require match occurred\n");
}

void output_tokens(struct token_list *i, FILE* out)
{
	while(NULL != i)
	{
		fputs(i->s, out);
		i = i->next;
	}
}
