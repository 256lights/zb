/* Copyright (C) 2016 Jeremiah Orians
 * This file is part of stage0.
 *
 * stage0 is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * stage0 is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with stage0.  If not, see <http://www.gnu.org/licenses/>.
 */

#include "lisp.h"

void writeobj(FILE *output_file, struct cell* op)
{
	if(!echo) return;

	if(INT == op->type)
	{
		fputs(int2str(op->value, 10, TRUE), output_file);
	}
	else if(CONS == op->type)
	{
		fputc('(', output_file);
		do
		{
			writeobj(output_file, op->car);
			if(nil == op->cdr)
			{
				fputc(')', output_file);
				break;
			}
			op = op->cdr;
			if(op->type != CONS)
			{
				fputs(" . ", output_file);
				writeobj(output_file, op);
				fputc(')', output_file);
				break;
			}
			fputc(' ', output_file);
		} while(TRUE);
	}
	else if(SYM == op->type)
	{
		fputs(op->string, output_file);
	}
	else if(PRIMOP == op->type)
	{
		fputs("#<PRIMOP>", output_file);
	}
	else if(PROC == op->type)
	{
		fputs("#<PROC>", output_file);
	}
	else if(CHAR == op->type)
	{
		fputc(op->value, output_file);
	}
	else if(STRING == op->type)
	{
		fputs(op->string, output_file);
	}
	else
	{
		fputs("Type ", stderr);
		fputs(int2str(op->type, 10, TRUE), stderr);
		fputs(" is unknown\nPrint aborting hard\n", stderr);
		exit(EXIT_FAILURE);
	}
}
