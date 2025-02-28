/* Copyright (C) 2021 Sanne Wouda
 * Copyright (C) 2021 Andrius Štikonas <andrius@stikonas.eu>
 * Copyright (C) 2022 Jan (janneke) Nieuwenhuizen <janneke@gnu.org>
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

void require(int bool, char* error);
int strtoint(char* a);
void line_error_token(struct token_list* list);
struct token_list* eat_token(struct token_list* head);
struct token_list* reverse_list(struct token_list* head);

struct conditional_inclusion
{
	struct conditional_inclusion* prev;
	int include; /* 1 == include, 0 == skip */
	int previous_condition_matched; /* 1 == all subsequent conditions treated as FALSE */
};

struct macro_list
{
	struct macro_list* next;
	char* symbol;
	struct token_list* expansion;
	struct token_list* arguments;
};

struct macro_list* macro_env;
struct conditional_inclusion* conditional_inclusion_top;

/* point where we are currently modifying the global_token list */
struct token_list* macro_token;

void init_macro_env(char* sym, char* value, char* source, int num)
{
	struct macro_list* hold = macro_env;
	macro_env = calloc(1, sizeof(struct macro_list));
	macro_env->symbol = sym;
	macro_env->next = hold;
	macro_env->expansion = calloc(1, sizeof(struct token_list));
	macro_env->expansion->s = value;
	macro_env->expansion->filename = source;
	macro_env->expansion->linenumber = num;
}

void _eat_current_token(int eat_whitespace)
{
	int update_global_token = FALSE;
	if (macro_token == global_token)
		update_global_token = TRUE;

	macro_token = eat_token(macro_token);
	if(eat_whitespace)
	{
		while (macro_token->s[0] == ' ')
		macro_token = eat_token(macro_token);
	}

	if(update_global_token)
		global_token = macro_token;
}

void eat_current_token()
{
	_eat_current_token(TRUE);
}

void eat_current_token_without_space()
{
	_eat_current_token(FALSE);
}

struct token_list* lookup_token(struct token_list* token, struct token_list* arguments)
{
	if(NULL == token)
	{
		fputs("null token received in token\n", stderr);
		exit(EXIT_FAILURE);
	}

	struct token_list* hold = arguments;

	while (NULL != hold)
	{
		if (match(token->s, hold->s))
		{
			/* found! */
			return hold->expansion;
		}

		hold = hold->next;
	}

	/* not found! */
	return NULL;
}

/* returns the first token inserted; inserts *before* point */
struct token_list* insert_tokens(struct token_list* point, struct token_list* token)
{
	struct token_list* copy;
	struct token_list* first = NULL;

	while (NULL != token)
	{
		copy = calloc(1, sizeof(struct token_list));
		copy->s = token->s;
		copy->filename = token->filename;
		copy->linenumber = token->linenumber;

		if(NULL == first)
		{
			first = copy;
		}

		copy->next = point;

		if (NULL != point)
		{
			copy->prev = point->prev;

			if(NULL != point->prev)
			{
				point->prev->next = copy;
			}

			point->prev = copy;
		}

		token = token->next;
	}

	return first;
}

/* returns the first token inserted; inserts *before* point */
struct token_list* copy_list(struct token_list* token)
{
	struct token_list* copy;
	struct token_list* prev = NULL;

	while (NULL != token)
	{
		copy = calloc(1, sizeof(struct token_list));
		copy->s = token->s;

		copy->next = prev;
		copy->prev = prev;
		prev = copy;
		token = token->next;
	}
	copy = reverse_list(copy);

	return copy;
}

struct macro_list* lookup_macro(struct token_list* token)
{
	if(NULL == token)
	{
		line_error_token(macro_token);
		fputs("null token received in lookup_macro\n", stderr);
		exit(EXIT_FAILURE);
	}

	struct macro_list* hold = macro_env;

	while (NULL != hold)
	{
		if (match(token->s, hold->symbol))
		{
			/* found! */
			return hold;
		}

		hold = hold->next;
	}

	/* not found! */
	return NULL;
}

void remove_macro(struct token_list* token)
{
	if(NULL == token)
	{
		line_error_token(macro_token);
		fputs("received a null in remove_macro\n", stderr);
		exit(EXIT_FAILURE);
	}

	struct macro_list* hold = macro_env;
	struct macro_list* temp;

	/* Deal with the first element */
	if (match(token->s, hold->symbol)) {
		macro_env = hold->next;
		free(hold);
		return;
	}

	/* Remove element form the middle of linked list */
	while (NULL != hold->next)
	{
		if (match(token->s, hold->next->symbol))
		{
			temp = hold->next;
			hold->next = hold->next->next;
			free(temp);
			return;
		}

		hold = hold->next;
	}

	/* nothing to undefine */
	return;
}

int macro_expression();
int macro_variable()
{
	int value = 0;
	struct macro_list* hold = lookup_macro(macro_token);
	if (NULL != hold)
	{
		if(NULL == hold->expansion)
		{
			line_error_token(macro_token);
			fputs("hold->expansion is a null\n", stderr);
			exit(EXIT_FAILURE);
		}
		value = strtoint(hold->expansion->s);
	}
	eat_current_token();
	return value;
}

int macro_number()
{
	int result = strtoint(macro_token->s);
	eat_current_token();
	return result;
}

int macro_primary_expr()
{
	int defined_has_paren = FALSE;
	int hold;
	require(NULL != macro_token, "got an EOF terminated macro primary expression\n");

	if('-' == macro_token->s[0])
	{
		eat_current_token();
		return -macro_primary_expr();
	}
	else if('!' == macro_token->s[0])
	{
		eat_current_token();
		return !macro_primary_expr();
	}
	else if('(' == macro_token->s[0])
	{
		eat_current_token();
		hold = macro_expression();
		require(')' == macro_token->s[0], "missing ) in macro expression\n");
		eat_current_token();
		return hold;
	}
	else if(match("defined", macro_token->s))
	{
		eat_current_token();

		require(NULL != macro_token, "got an EOF terminated macro defined expression\n");

		if('(' == macro_token->s[0])
		{
			defined_has_paren = TRUE;
			eat_current_token();
		}

		if (NULL != lookup_macro(macro_token))
		{
			hold = TRUE;
		}
		else
		{
			hold = FALSE;
		}
		eat_current_token();

		if(TRUE == defined_has_paren)
		{
			if(NULL == macro_token)
			{
				line_error_token(macro_token);
				fputs("unterminated define ( statement\n", stderr);
				exit(EXIT_FAILURE);
			}
			require(')' == macro_token->s[0], "missing close parenthesis for defined()\n");
			eat_current_token();
		}

		return hold;
	}
	else if(in_set(macro_token->s[0], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_"))
	{
		return macro_variable();
	}
	else if(in_set(macro_token->s[0], "0123456789"))
	{
		return macro_number();
	}
	else
	{
		return 0;    /* FIXME: error handling */
	}
}

int macro_additive_expr()
{
	int lhs = macro_primary_expr();
	int hold;

	require(NULL != macro_token, "got an EOF terminated macro additive expression\n");
	if(match("+", macro_token->s))
	{
		eat_current_token();
		return lhs + macro_additive_expr();
	}
	else if(match("-", macro_token->s))
	{
		eat_current_token();
		return lhs - macro_additive_expr();
	}
	else if(match("*", macro_token->s))
	{
		eat_current_token();
		return lhs * macro_additive_expr();
	}
	else if(match("/", macro_token->s))
	{
		eat_current_token();
		hold = macro_additive_expr();
		require(0 != hold, "divide by zero not valid even in C macros\n");
		return lhs / hold;
	}
	else if(match("%", macro_token->s))
	{
		eat_current_token();
		hold = macro_additive_expr();
		require(0 != hold, "modulus by zero not valid even in C macros\n");
		return lhs % hold;
	}
	else if(match(">>", macro_token->s))
	{
		eat_current_token();
		return lhs >> macro_additive_expr();
	}
	else if(match("<<", macro_token->s))
	{
		eat_current_token();
		return lhs << macro_additive_expr();
	}
	else
	{
		return lhs;
	}
}

int macro_relational_expr()
{
	int lhs = macro_additive_expr();

	if(match("<", macro_token->s))
	{
		eat_current_token();
		return lhs < macro_relational_expr();
	}
	else if(match("<=", macro_token->s))
	{
		eat_current_token();
		return lhs <= macro_relational_expr();
	}
	else if(match(">=", macro_token->s))
	{
		eat_current_token();
		return lhs >= macro_relational_expr();
	}
	else if(match(">", macro_token->s))
	{
		eat_current_token();
		return lhs > macro_relational_expr();
	}
	else if(match("==", macro_token->s))
	{
		eat_current_token();
		return lhs == macro_relational_expr();
	}
	else if(match("!=", macro_token->s))
	{
		eat_current_token();
		return lhs != macro_relational_expr();
	}
	else
	{
		return lhs;
	}
}

int macro_bitwise_expr()
{
	int rhs;
	int lhs = macro_relational_expr();

	if(match("&", macro_token->s))
	{
		eat_current_token();
		return lhs & macro_bitwise_expr();
	}
	else if(match("&&", macro_token->s))
	{
		eat_current_token();
		rhs = macro_bitwise_expr();
		return lhs && rhs;
	}
	else if(match("|", macro_token->s))
	{
		eat_current_token();
		rhs = macro_bitwise_expr();
		return lhs | rhs;
	}
	else if(match("||", macro_token->s))
	{
		eat_current_token();
		rhs = macro_bitwise_expr();
		return lhs || rhs;
	}
	else if(match("^", macro_token->s))
	{
		eat_current_token();
		rhs = macro_bitwise_expr();
		return lhs ^ rhs;
	}
	else
	{
		return lhs;
	}
}

int macro_expression()
{
	return macro_bitwise_expr();
}

void handle_define()
{
	struct macro_list* hold;
	struct token_list* arg;
	struct token_list* expansion_end = NULL;

	/* don't use #define statements from non-included blocks */
	int conditional_define = TRUE;
	if(NULL != conditional_inclusion_top)
	{
		if(FALSE == conditional_inclusion_top->include)
		{
			conditional_define = FALSE;
		}
	}

	eat_current_token();

	require(NULL != macro_token, "got an EOF terminated #define\n");
	require('\n' != macro_token->s[0], "unexpected newline after #define\n");

	/* insert new macro */
	hold = calloc(1, sizeof(struct macro_list));
	hold->symbol = macro_token->s;
	hold->next = macro_env;
	/* provided it isn't in a non-included block */
	if(conditional_define) macro_env = hold;

	/* discard the macro name */
	eat_current_token_without_space();

	/* Handle macro arguments */
	if(macro_token->s[0] == '(')
	{
		/* discard ( */
		eat_current_token();
		require(NULL != macro_token, "got an EOF terminated #define\n");
		if(macro_token->s[0] != ')')
		{
			arg = calloc(1, sizeof(struct token_list));
			arg->s = macro_token->s;
			hold->arguments = arg;
			eat_current_token();
			require(NULL != macro_token, "incomplete macro call\n");
			while(macro_token->s[0] == ',')
			{
				eat_current_token();
				require(NULL != macro_token, "incomplete macro call, got an EOF instead of an argument\n");
				arg = calloc(1, sizeof(struct token_list));
				arg->s = macro_token->s;
				arg->next = hold->arguments;
				hold->arguments = arg;
				eat_current_token();
				require(NULL != macro_token, "incomplete macro call\n");
			}
		}
		eat_current_token();

		/* Reverse argument list */
		hold->arguments = reverse_list(hold->arguments);

		require(NULL != macro_token, "got an EOF terminated #define\n");
	}
	else if(macro_token->s[0] == ' ')
	{
		eat_current_token();
	}

	while (TRUE)
	{
		require(NULL != macro_token, "got an EOF terminated #define\n");

		if ('\n' == macro_token->s[0])
		{
			if(NULL == expansion_end)
			{
				hold->expansion = NULL;
				expansion_end = macro_token;
				return;
			}
			expansion_end->next = NULL;
			return;
		}
		else if(('/' == macro_token->s[0]) && ('*' == macro_token->s[1]))
		{
			eat_current_token();
			continue;
		}
		else if(('/' == macro_token->s[0]) && ('/' == macro_token->s[1]))
		{
			macro_token->s = "\n";
			if(NULL == expansion_end)
			{
				hold->expansion = NULL;
				expansion_end = macro_token;
				return;
			}
			expansion_end->next = NULL;
			return;
		}

		if(NULL == hold)
		{
			eat_current_token();
			continue;
		}

		expansion_end = macro_token;

		/* in the first iteration, we set the first token of the expansion, if
		   it exists */
		if (NULL == hold->expansion)
		{
			hold->expansion = macro_token;
		}

		/* throw away if not used */
		if(!conditional_define && (NULL != hold))
		{
			free(hold);
			hold = NULL;
		}

		eat_current_token();
	}
}

void handle_undef()
{
	eat_current_token();
	remove_macro(macro_token);
	eat_current_token();
}

void handle_error(int warning_p)
{
	/* don't use #error statements from non-included blocks */
	int conditional_error = TRUE;
	if(NULL != conditional_inclusion_top)
	{
		if(FALSE == conditional_inclusion_top->include)
		{
			conditional_error = FALSE;
		}
	}
	eat_current_token();
	/* provided it isn't in a non-included block */
	if(conditional_error)
	{
		line_error_token(macro_token);
		if(warning_p) fputs(" warning: #warning ", stderr);
		else fputs(" error: #error ", stderr);
		while (TRUE)
		{
			if ('\n' == macro_token->s[0]) break;
			fputs(macro_token->s, stderr);
			macro_token = macro_token->next;
		}
		fputs("\n", stderr);
		if(!warning_p) exit(EXIT_FAILURE);
	}
	while (TRUE)
	{
		/* discard the error */
		if ('\n' == macro_token->s[0])
		{
			return;
		}
		eat_current_token();
	}
}

void macro_directive()
{
	struct conditional_inclusion *t;
	int result;

	/* FIXME: whitespace is allowed between "#"" and "if" */
	if(match("#if", macro_token->s))
	{
		eat_current_token();
		/* evaluate constant integer expression */
		result = macro_expression();
		/* push conditional inclusion */
		t = calloc(1, sizeof(struct conditional_inclusion));
		t->prev = conditional_inclusion_top;
		conditional_inclusion_top = t;
		t->include = TRUE;

		if(FALSE == result)
		{
			t->include = FALSE;
		}

		t->previous_condition_matched = t->include;
	}
	else if(match("#ifdef", macro_token->s))
	{
		eat_current_token();
		require(NULL != macro_token, "got an EOF terminated macro defined expression\n");
		if (NULL != lookup_macro(macro_token))
		{
			result = TRUE;
		}
		else
		{
			result = FALSE;
		}
		eat_current_token();

		/* push conditional inclusion */
		t = calloc(1, sizeof(struct conditional_inclusion));
		t->prev = conditional_inclusion_top;
		conditional_inclusion_top = t;
		t->include = TRUE;

		if(FALSE == result)
		{
			t->include = FALSE;
		}

		t->previous_condition_matched = t->include;
	}
	else if(match("#ifndef", macro_token->s))
	{
		eat_current_token();
		require(NULL != macro_token, "got an EOF terminated macro defined expression\n");
		if (NULL != lookup_macro(macro_token))
		{
			result = FALSE;
		}
		else
		{
			result = TRUE;
		}
		eat_current_token();

		/* push conditional inclusion */
		t = calloc(1, sizeof(struct conditional_inclusion));
		t->prev = conditional_inclusion_top;
		conditional_inclusion_top = t;
		t->include = TRUE;

		if(FALSE == result)
		{
			t->include = FALSE;
		}

		t->previous_condition_matched = t->include;
	}
	else if(match("#elif", macro_token->s))
	{
		eat_current_token();
		result = macro_expression();
		require(NULL != conditional_inclusion_top, "#elif without leading #if\n");
		conditional_inclusion_top->include = result && !conditional_inclusion_top->previous_condition_matched;
		conditional_inclusion_top->previous_condition_matched =
		    conditional_inclusion_top->previous_condition_matched || conditional_inclusion_top->include;
	}
	else if(match("#else", macro_token->s))
	{
		eat_current_token();
		require(NULL != conditional_inclusion_top, "#else without leading #if\n");
		conditional_inclusion_top->include = !conditional_inclusion_top->previous_condition_matched;
	}
	else if(match("#endif", macro_token->s))
	{
		if(NULL == conditional_inclusion_top)
		{
			line_error_token(macro_token);
			fputs("unexpected #endif\n", stderr);
			exit(EXIT_FAILURE);
		}

		eat_current_token();
		/* pop conditional inclusion */
		t = conditional_inclusion_top;
		conditional_inclusion_top = conditional_inclusion_top->prev;
		free(t);
	}
	else if(match("#define", macro_token->s))
	{
		handle_define();
	}
	else if(match("#undef", macro_token->s))
	{
		handle_undef();
	}
	else if(match("#error", macro_token->s))
	{
		handle_error(FALSE);
	}
	else if(match("#warning", macro_token->s))
	{
		handle_error(TRUE);
	}
	else if(match("#FILENAME", macro_token->s))
	{
		while(TRUE)
		{
			if(NULL == macro_token)
			{
				return;
			}

			if('\n' == macro_token->s[0])
			{
				return;
			}

			eat_current_token();
		}
	}
	else
	{
		/* Put a big fat warning but see if we can just ignore */
		fputs(">>WARNING<<\n>>WARNING<<\n", stderr);
		line_error_token(macro_token);
		fputs("feature: ", stderr);
		fputs(macro_token->s, stderr);
		fputs(" unsupported in M2-Planet\nIgnoring line, may result in bugs\n>>WARNING<<\n>>WARNING<<\n\n", stderr);

		/* unhandled macro directive; let's eat until a newline; om nom nom */
		while(TRUE)
		{
			if(NULL == macro_token)
			{
				return;
			}

			if('\n' == macro_token->s[0])
			{
				return;
			}

			eat_current_token();
		}
	}
}

struct token_list* expand_macro_functions(struct token_list* expansion, struct token_list* arguments)
{
	struct token_list* expanded_token;
	struct token_list* head;
	struct token_list* hold; /* Same as head unless head == NULL */
	head = copy_list(expansion);
	while(NULL != head)
	{
		expanded_token = lookup_token(head, arguments);
		hold = head;
		if(NULL != expanded_token)
		{
			insert_tokens(head, expanded_token);
			hold = head->prev;
			head = eat_token(head);
		}
		else
		{
			head = head->next;
		}
	}
	while(NULL != hold->prev) hold = hold->prev;
	return hold;
}

void eat_until_endif()
{
	/* This #if block is nested inside of an #if block that needs to be dropped, lose EVERYTHING */
	do
	{
		if(match("#if", macro_token->s) || match("#ifdef", macro_token->s) || match("#ifndef", macro_token->s))
		{
			eat_current_token();
			eat_until_endif();
		}

		eat_current_token();
		require(NULL != macro_token, "Unterminated #if block\n");
	} while(!match("#endif", macro_token->s));
}

void eat_block()
{
	/* This conditional #if block is wrong, drop everything until the #elif/#else/#endif */
	do
	{
		if(match("#if", macro_token->s) || match("#ifdef", macro_token->s) || match("#ifndef", macro_token->s))
		{
			eat_current_token();
			eat_until_endif();
		}

		eat_current_token();
		require(NULL != macro_token, "Unterminated #if block\n");
	} while(!match("#elif", macro_token->s) && !match("#else", macro_token->s) && !match("#endif", macro_token->s));
}

struct token_list* maybe_expand(struct token_list* token)
{
	if(NULL == token)
	{
		line_error_token(macro_token);
		fputs("maybe_expand passed a null token\n", stderr);
		exit(EXIT_FAILURE);
	}

	struct macro_list* hold = lookup_macro(token);
	struct token_list* hold2;
	struct token_list* hold3;
	struct token_list* hold4;
	if(NULL == token->next)
	{
		line_error_token(macro_token);
		fputs("we can't expand a null token: ", stderr);
		fputs(token->s, stderr);
		fputc('\n', stderr);
		exit(EXIT_FAILURE);
	}

	if (NULL == hold)
	{
		return token->next;
	}

	if(match("__M2__", token->s)) return token->next;

	token = eat_token(token);

	if (NULL == hold->expansion)
	{
		return token->next;
	}

	/* Match macro arguments with stored names */
	hold3 = hold->arguments;
	if(NULL != hold3)
	{
		if(token->s[0] == ' ')
		{
			token = eat_token(token);
		}
		require('(' == token->s[0], "missing open parenthesis for macro function\n");
		token = eat_token(token);
		require(NULL != token, "got an EOF terminated macro function\n");
		do
		{
			hold2 = calloc(1, sizeof(struct token_list));
			hold2->s = token->s;
			hold2->next = hold->arguments->expansion;
			hold->arguments->expansion = hold2;
			token = eat_token(token);
			require(NULL != token, "incomplete macro call\n");
			if(token->s[0] == ',')
			{
				hold->arguments->expansion = reverse_list(hold->arguments->expansion);
				hold->arguments = hold->arguments->next;
				require(NULL != hold->arguments, "too many arguments in macro call\n");
				token = eat_token(token);
				require(NULL != token, "incomplete macro call\n");
			}
		} while(token->s[0] != ')');
		hold->arguments->expansion = reverse_list(hold->arguments->expansion);
		hold->arguments = hold3;
		token = eat_token(token);
	}
	hold4 = expand_macro_functions(hold->expansion, hold->arguments);
	hold4 = insert_tokens(token, hold4);

	return hold4;
}

void preprocess()
{
	int start_of_line = TRUE;
	macro_token = global_token;

	while(NULL != macro_token)
	{
		if(start_of_line && '#' == macro_token->s[0])
		{
			macro_directive();

			if(macro_token)
			{
				if('\n' != macro_token->s[0])
				{
					line_error_token(macro_token);
					fputs("newline expected at end of macro directive\n", stderr);
					fputs("found: '", stderr);
					fputs(macro_token->s, stderr);
					fputs("'\n", stderr);
					exit(EXIT_FAILURE);
				}
			}
		}
		else if('\n' == macro_token->s[0])
		{
			start_of_line = TRUE;
			macro_token = macro_token->next;
		}
		else
		{
			start_of_line = FALSE;
			if(NULL == conditional_inclusion_top)
			{
				macro_token = maybe_expand(macro_token);
			}
			else if(!conditional_inclusion_top->include)
			{
				/* rewrite the token stream to exclude the current token */
				eat_block();
				start_of_line = TRUE;
			}
			else
			{
				macro_token = maybe_expand(macro_token);
			}
		}
	}
}
