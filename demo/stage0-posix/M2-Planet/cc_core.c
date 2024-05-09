/* Copyright (C) 2016 Jeremiah Orians
 * Copyright (C) 2018 Jan (janneke) Nieuwenhuizen <janneke@gnu.org>
 * Copyright (C) 2020 deesix <deesix@tuta.io>
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
#include "gcc_req.h"
#include <stdint.h>

/* Global lists */
struct token_list* global_symbol_list;
struct token_list* global_function_list;
struct token_list* global_constant_list;

/* Core lists for this file */
struct token_list* function;

/* What we are currently working on */
struct type* current_target;
char* break_target_head;
char* break_target_func;
char* break_target_num;
char* continue_target_head;
struct token_list* break_frame;
int current_count;
int Address_of;

/* Imported functions */
char* int2str(int x, int base, int signed_p);
int strtoint(char *a);
char* parse_string(char* string);
int escape_lookup(char* c);
void require(int bool, char* error);
struct token_list* reverse_list(struct token_list* head);
struct type* mirror_type(struct type* source, char* name);
struct type* add_primitive(struct type* a);

struct token_list* emit(char *s, struct token_list* head)
{
	struct token_list* t = calloc(1, sizeof(struct token_list));
	require(NULL != t, "Exhausted memory while generating token to emit\n");
	t->next = head;
	t->s = s;
	return t;
}

void emit_out(char* s)
{
	output_list = emit(s, output_list);
}

struct token_list* uniqueID(char* s, struct token_list* l, char* num)
{
	l = emit("\n", emit(num, emit("_", emit(s, l))));
	return l;
}

void uniqueID_out(char* s, char* num)
{
	output_list = uniqueID(s, output_list, num);
}

struct token_list* sym_declare(char *s, struct type* t, struct token_list* list)
{
	struct token_list* a = calloc(1, sizeof(struct token_list));
	require(NULL != a, "Exhausted memory while attempting to declare a symbol\n");
	a->next = list;
	a->s = s;
	a->type = t;
	return a;
}

struct token_list* sym_lookup(char *s, struct token_list* symbol_list)
{
	struct token_list* i;
	for(i = symbol_list; NULL != i; i = i->next)
	{
		if(match(i->s, s)) return i;
	}
	return NULL;
}

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
	if(NULL == global_token)
	{
		line_error();
		fputs("EOF reached inside of require match\n", stderr);
		fputs("problem at end of file\n", stderr);
		fputs(message, stderr);
		exit(EXIT_FAILURE);
	}
	if(!match(global_token->s, required))
	{
		line_error();
		fputs(message, stderr);
		exit(EXIT_FAILURE);
	}
	global_token = global_token->next;
}

void maybe_bootstrap_error(char* feature)
{
	if (BOOTSTRAP_MODE)
	{
		line_error();
		fputs(feature, stderr);
		fputs(" is not supported in --bootstrap-mode\n", stderr);
		exit(EXIT_FAILURE);
	}
}

void expression();
void function_call(char* s, int bool)
{
	require_match("ERROR in process_expression_list\nNo ( was found\n", "(");
	require(NULL != global_token, "Improper function call\n");
	int passed = 0;

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
	{
		emit_out("PUSHR R13 R15\t# Prevent overwriting in recursion\n");
		emit_out("PUSHR R14 R15\t# Protect the old base pointer\n");
		emit_out("COPY R13 R15\t# Copy new base pointer\n");
	}
	else if(X86 == Architecture)
	{
		emit_out("push_edi\t# Prevent overwriting in recursion\n");
		emit_out("push_ebp\t# Protect the old base pointer\n");
		emit_out("mov_edi,esp\t# Copy new base pointer\n");
	}
	else if(AMD64 == Architecture)
	{
		emit_out("push_rdi\t# Prevent overwriting in recursion\n");
		emit_out("push_rbp\t# Protect the old base pointer\n");
		emit_out("mov_rdi,rsp\t# Copy new base pointer\n");
	}
	else if(ARMV7L == Architecture)
	{
		emit_out("{R11} PUSH_ALWAYS\t# Prevent overwriting in recursion\n");
		emit_out("{BP} PUSH_ALWAYS\t# Protect the old base pointer\n");
		emit_out("'0' SP R11 NO_SHIFT MOVE_ALWAYS\t# Copy new base pointer\n");
	}
	else if(AARCH64 == Architecture)
	{
		emit_out("PUSH_X16\t# Protect a tmp register we're going to use\n");
		emit_out("PUSH_LR\t# Protect the old return pointer (link)\n");
		emit_out("PUSH_BP\t# Protect the old base pointer\n");
		emit_out("SET_X16_FROM_SP\t# The base pointer to-be\n");
	}
	else if(RISCV32 == Architecture)
	{
		emit_out("rd_sp rs1_sp !-12 addi\t# Allocate stack\n");
		emit_out("rs1_sp rs2_ra @4 sw\t# Protect the old return pointer\n");
		emit_out("rs1_sp rs2_fp sw\t# Protect the old frame pointer\n");
		emit_out("rs1_sp rs2_tp @8 sw\t# Protect temp register we are going to use\n");
		emit_out("rd_tp rs1_sp mv\t# The base pointer to-be\n");
	}
	else if(RISCV64 == Architecture)
	{
		emit_out("rd_sp rs1_sp !-24 addi\t# Allocate stack\n");
		emit_out("rs1_sp rs2_ra @8 sd\t# Protect the old return pointer\n");
		emit_out("rs1_sp rs2_fp sd\t# Protect the old frame pointer\n");
		emit_out("rs1_sp rs2_tp @16 sd\t# Protect temp register we are going to use\n");
		emit_out("rd_tp rs1_sp mv\t# The base pointer to-be\n");
	}

	if(global_token->s[0] != ')')
	{
		expression();
		require(NULL != global_token, "incomplete function call, received EOF instead of )\n");
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("PUSHR R0 R15\t#_process_expression1\n");
		else if(X86 == Architecture) emit_out("push_eax\t#_process_expression1\n");
		else if(AMD64 == Architecture) emit_out("push_rax\t#_process_expression1\n");
		else if(ARMV7L == Architecture) emit_out("{R0} PUSH_ALWAYS\t#_process_expression1\n");
		else if(AARCH64 == Architecture) emit_out("PUSH_X0\t#_process_expression1\n");
		else if(RISCV32 == Architecture) emit_out("rd_sp rs1_sp !-4 addi\nrs1_sp rs2_a0 sw\t#_process_expression1\n");
		else if(RISCV64 == Architecture) emit_out("rd_sp rs1_sp !-8 addi\nrs1_sp rs2_a0 sd\t#_process_expression1\n");
		passed = 1;

		while(global_token->s[0] == ',')
		{
			global_token = global_token->next;
			require(NULL != global_token, "incomplete function call, received EOF instead of argument\n");
			expression();
			if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("PUSHR R0 R15\t#_process_expression2\n");
			else if(X86 == Architecture) emit_out("push_eax\t#_process_expression2\n");
			else if(AMD64 == Architecture) emit_out("push_rax\t#_process_expression2\n");
			else if(ARMV7L == Architecture) emit_out("{R0} PUSH_ALWAYS\t#_process_expression2\n");
			else if(AARCH64 == Architecture) emit_out("PUSH_X0\t#_process_expression2\n");
			else if(RISCV32 == Architecture) emit_out("rd_sp rs1_sp !-4 addi\nrs1_sp rs2_a0 sw\t#_process_expression2\n");
			else if(RISCV64 == Architecture) emit_out("rd_sp rs1_sp !-8 addi\nrs1_sp rs2_a0 sd\t#_process_expression2\n");
			passed = passed + 1;
		}
	}

	require_match("ERROR in process_expression_list\nNo ) was found\n", ")");

	if(TRUE == bool)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
		{
			emit_out("LOAD R0 R14 ");
			emit_out(s);
			emit_out("\nMOVE R14 R13\n");
			emit_out("CALL R0 R15\n");
		}
		else if(X86 == Architecture)
		{
			emit_out("lea_eax,[ebp+DWORD] %");
			emit_out(s);
			emit_out("\nmov_eax,[eax]\n");
			emit_out("mov_ebp,edi\n");
			emit_out("call_eax\n");
		}
		else if(AMD64 == Architecture)
		{
			emit_out("lea_rax,[rbp+DWORD] %");
			emit_out(s);
			emit_out("\nmov_rax,[rax]\n");
			emit_out("mov_rbp,rdi\n");
			emit_out("call_rax\n");
		}
		else if(ARMV7L == Architecture)
		{
			emit_out("!");
			emit_out(s);
			emit_out(" R0 SUB BP ARITH_ALWAYS\n");
			emit_out("!0 R0 LOAD32 R0 MEMORY\n");
			emit_out("{LR} PUSH_ALWAYS\t# Protect the old link register\n");
			emit_out("'0' R11 BP NO_SHIFT MOVE_ALWAYS\n");
			emit_out("'3' R0 CALL_REG_ALWAYS\n");
			emit_out("{LR} POP_ALWAYS\t# Prevent overwrite\n");
		}
		else if(AARCH64 == Architecture)
		{
			emit_out("SET_X0_FROM_BP\n");
			emit_out("LOAD_W1_AHEAD\nSKIP_32_DATA\n%");
			emit_out(s);
			emit_out("\nSUB_X0_X0_X1\n");
			emit_out("DEREF_X0\n");
			emit_out("SET_BP_FROM_X16\n");
			emit_out("SET_X16_FROM_X0\n");
			emit_out("BLR_X16\n");
		}
		else if(RISCV32 == Architecture)
		{
			emit_out("rd_a0 rs1_fp !");
			emit_out(s);
			emit_out(" addi\n");
			emit_out("rd_a0 rs1_a0 lw\n");
			emit_out("rd_fp rs1_tp mv\n");
			emit_out("rd_ra rs1_a0 jalr\n");
		}
		else if(RISCV64 == Architecture)
		{
			emit_out("rd_a0 rs1_fp !");
			emit_out(s);
			emit_out(" addi\n");
			emit_out("rd_a0 rs1_a0 ld\n");
			emit_out("rd_fp rs1_tp mv\n");
			emit_out("rd_ra rs1_a0 jalr\n");
		}
	}
	else
	{
		if((KNIGHT_NATIVE == Architecture) || (KNIGHT_POSIX == Architecture))
		{
			emit_out("MOVE R14 R13\n");
			emit_out("LOADR R0 4\nJUMP 4\n&FUNCTION_");
			emit_out(s);
			emit_out("\nCALL R0 R15\n");
		}
		else if(X86 == Architecture)
		{
			emit_out("mov_ebp,edi\n");
			emit_out("call %FUNCTION_");
			emit_out(s);
			emit_out("\n");
		}
		else if(AMD64 == Architecture)
		{
			emit_out("mov_rbp,rdi\n");
			emit_out("call %FUNCTION_");
			emit_out(s);
			emit_out("\n");
		}
		else if(ARMV7L == Architecture)
		{
			emit_out("{LR} PUSH_ALWAYS\t# Protect the old link register\n");
			emit_out("'0' R11 BP NO_SHIFT MOVE_ALWAYS\n");
			emit_out("^~FUNCTION_");
			emit_out(s);
			emit_out(" CALL_ALWAYS\n");
			emit_out("{LR} POP_ALWAYS\t# Restore the old link register\n");
		}
		else if(AARCH64 == Architecture)
		{
			emit_out("SET_BP_FROM_X16\n");
			emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&FUNCTION_");
			emit_out(s);
			emit_out("\n");
			emit_out("BLR_X16\n");
		}
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
		{
			emit_out("rd_fp rs1_tp mv\n");
			emit_out("rd_ra $FUNCTION_");
			emit_out(s);
			emit_out(" jal\n");
		}
	}

	for(; passed > 0; passed = passed - 1)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("POPR R1 R15\t# _process_expression_locals\n");
		else if(X86 == Architecture) emit_out("pop_ebx\t# _process_expression_locals\n");
		else if(AMD64 == Architecture) emit_out("pop_rbx\t# _process_expression_locals\n");
		else if(ARMV7L == Architecture) emit_out("{R1} POP_ALWAYS\t# _process_expression_locals\n");
		else if(AARCH64 == Architecture) emit_out("POP_X1\t# _process_expression_locals\n");
		else if(RISCV32 == Architecture) emit_out("rd_a1 rs1_sp lw\t# _process_expression_locals\nrd_sp rs1_sp !4 addi\n");
		else if(RISCV64 == Architecture) emit_out("rd_a1 rs1_sp ld\t# _process_expression_locals\nrd_sp rs1_sp !8 addi\n");
	}

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
	{
		emit_out("POPR R14 R15\t# Restore old base pointer\n");
		emit_out("POPR R13 R15\t# Prevent overwrite\n");
	}
	else if(X86 == Architecture)
	{
		emit_out("pop_ebp\t# Restore old base pointer\n");
		emit_out("pop_edi\t# Prevent overwrite\n");
	}
	else if(AMD64 == Architecture)
	{
		emit_out("pop_rbp\t# Restore old base pointer\n");
		emit_out("pop_rdi\t# Prevent overwrite\n");
	}
	else if(ARMV7L == Architecture)
	{
		emit_out("{BP} POP_ALWAYS\t# Restore old base pointer\n");
		emit_out("{R11} POP_ALWAYS\t# Prevent overwrite\n");
	}
	else if(AARCH64 == Architecture)
	{
		emit_out("POP_BP\t# Restore the old base pointer\n");
		emit_out("POP_LR\t# Restore the old return pointer (link)\n");
		emit_out("POP_X16\t# Restore a register we used as tmp\n");
	}
	else if(RISCV32 == Architecture)
	{
		emit_out("rd_fp rs1_sp lw\t# Restore old frame pointer\n");
		emit_out("rd_tp rs1_sp !8 lw\t# Restore temp register\n");
		emit_out("rd_ra rs1_sp !4 lw\t# Restore return address\n");
		emit_out("rd_sp rs1_sp !12 addi\t# Deallocate stack\n");
	}
	else if(RISCV64 == Architecture)
	{
		emit_out("rd_fp rs1_sp ld\t# Restore old frame pointer\n");
		emit_out("rd_tp rs1_sp !16 ld\t# Restore temp register\n");
		emit_out("rd_ra rs1_sp !8 ld\t# Restore return address\n");
		emit_out("rd_sp rs1_sp !24 addi\t# Deallocate stack\n");
	}
}

void constant_load(char* s)
{
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("LOADI R0 ");
	else if(X86 == Architecture) emit_out("mov_eax, %");
	else if(AMD64 == Architecture) emit_out("mov_rax, %");
	else if(ARMV7L == Architecture) emit_out("!0 R0 LOAD32 R15 MEMORY\n~0 JUMP_ALWAYS\n%");
	else if(AARCH64 == Architecture) emit_out("LOAD_W0_AHEAD\nSKIP_32_DATA\n%");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		emit_out("rd_a0 ~");
		emit_out(s);
		emit_out(" lui\nrd_a0 rs1_a0 !");
	}
	emit_out(s);
	if(RISCV32 == Architecture) emit_out(" addi\n");
	else if(RISCV64 == Architecture) emit_out(" addiw\n");
	emit_out("\n");
}

char* load_value_signed(unsigned size)
{
	if(size == 1)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) return "LOAD8 R0 R0 0\n";
		else if(X86 == Architecture) return "movsx_eax,BYTE_PTR_[eax]\n";
		else if(AMD64 == Architecture) return "movsx_rax,BYTE_PTR_[rax]\n";
		else if(ARMV7L == Architecture) return "LOADS8 R0 LOAD R0 HALF_MEMORY\n";
		else if(AARCH64 == Architecture) return "LDRSB_X0_[X0]\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) return "rd_a0 rs1_a0 lb\n";
	}
	else if(size == 2)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) return "LOAD16 R0 R0 0\n";
		else if(X86 == Architecture) return "movsx_eax,WORD_PTR_[eax]\n";
		else if(AMD64 == Architecture) return "movsx_rax,WORD_PTR_[rax]\n";
		else if(ARMV7L == Architecture) return "LOADS16 R0 LOAD R0 HALF_MEMORY\n";
		else if(AARCH64 == Architecture) return "LDRSH_X0_[X0]\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) return "rd_a0 rs1_a0 lh\n";
	}
	else if(size == 4)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) return "LOAD R0 R0 0\n";
		else if(X86 == Architecture) return "mov_eax,[eax]\n";
		else if(AMD64 == Architecture) return "movsx_rax,DWORD_PTR_[rax]\n";
		else if(ARMV7L == Architecture) return "!0 R0 LOAD32 R0 MEMORY\n";
		else if(AARCH64 == Architecture) return "LDR_W0_[X0]\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) return "rd_a0 rs1_a0 lw\n";
	}
	else if(size == 8)
	{
		if(AMD64 == Architecture) return "mov_rax,[rax]\n";
		else if(AARCH64 == Architecture) return "DEREF_X0\n";
		else if(RISCV64 == Architecture) return "rd_a0 rs1_a0 ld\n";
	}
	line_error();
	fputs(" Got unsupported size ", stderr);
	fputs(int2str(size, 10, TRUE), stderr);
	fputs(" when trying to load value.\n", stderr);
	exit(EXIT_FAILURE);
}

char* load_value_unsigned(unsigned size)
{
	if(size == 1)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) return "LOADU8 R0 R0 0\n";
		else if(X86 == Architecture) return "movzx_eax,BYTE_PTR_[eax]\n";
		else if(AMD64 == Architecture) return "movzx_rax,BYTE_PTR_[rax]\n";
		else if(ARMV7L == Architecture) return "!0 R0 LOAD R0 MEMORY\n";
		else if(AARCH64 == Architecture) return "DEREF_X0_BYTE\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) return "rd_a0 rs1_a0 lbu\n";
	}
	else if(size == 2)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) return "LOADU16 R0 R0 0\n";
		else if(X86 == Architecture) return "movzx_eax,WORD_PTR_[eax]\n";
		else if(AMD64 == Architecture) return "movzx_rax,WORD_PTR_[rax]\n";
		else if(ARMV7L == Architecture) return "NO_OFFSET R0 LOAD R0 HALF_MEMORY\n";
		else if(AARCH64 == Architecture) return "LDRH_W0_[X0]\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) return "rd_a0 rs1_a0 lhu\n";
	}
	else if(size == 4)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) return "LOAD R0 R0 0\n";
		else if(X86 == Architecture) return "mov_eax,[eax]\n";
		else if(AMD64 == Architecture) return "mov_eax,[rax]\n";
		else if(ARMV7L == Architecture) return "!0 R0 LOAD32 R0 MEMORY\n";
		else if(AARCH64 == Architecture) return "LDR_W0_[X0]\n";
		else if(RISCV32 == Architecture) return "rd_a0 rs1_a0 lw\n";
		else if(RISCV64 == Architecture) return "rd_a0 rs1_a0 lwu\n";
	}
	else if(size == 8)
	{
		if(AMD64 == Architecture) return "mov_rax,[rax]\n";
		else if(AARCH64 == Architecture) return "DEREF_X0\n";
		else if(RISCV64 == Architecture) return "rd_a0 rs1_a0 ld\n";
	}
	line_error();
	fputs(" Got unsupported size ", stderr);
	fputs(int2str(size, 10, TRUE), stderr);
	fputs(" when trying to load value.\n", stderr);
	exit(EXIT_FAILURE);
}

char* load_value(unsigned size, int is_signed)
{
	if(is_signed) return load_value_signed(size);
	return load_value_unsigned(size);
}

char* store_value(unsigned size)
{
	if(size == 1)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) return "STORE8 R0 R1 0\n";
		else if(X86 == Architecture) return "mov_[ebx],al\n";
		else if(AMD64 == Architecture) return "mov_[rbx],al\n";
		else if(ARMV7L == Architecture) return "!0 R0 STORE8 R1 MEMORY\n";
		else if(AARCH64 == Architecture) return "STR_BYTE_W0_[X1]\n";
		else if(RISCV32 == Architecture) return "rs1_a1 rs2_a0 sb\n";
		else if(RISCV64 == Architecture) return "rs1_a1 rs2_a0 sb\n";
	}
	else if(size == 2)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) return "STORE16 R0 R1 0\n";
		else if(X86 == Architecture) return "mov_[ebx],ax\n";
		else if(AMD64 == Architecture) return "mov_[rbx],ax\n";
		else if(ARMV7L == Architecture) return "NO_OFFSET R0 STORE16 R1 HALF_MEMORY\n";
		else if(AARCH64 == Architecture) return "STRH_W0_[X1]\n";
		else if(RISCV32 == Architecture || RISCV64 == Architecture) return "rs1_a1 rs2_a0 sh\n";
	}
	else if(size == 4)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) return "STORE R0 R1 0\n";
		else if(X86 == Architecture) return "mov_[ebx],eax\n";
		else if(AMD64 == Architecture) return "mov_[rbx],eax\n";
		else if(ARMV7L == Architecture) return "!0 R0 STORE32 R1 MEMORY\n";
		else if(AARCH64 == Architecture) return "STR_W0_[X1]\n";
		else if(RISCV32 == Architecture || RISCV64 == Architecture) return "rs1_a1 rs2_a0 sw\n";
	}
	else if(size == 8)
	{
		if(AMD64 == Architecture) return "mov_[rbx],rax\n";
		else if(AARCH64 == Architecture) return "STR_X0_[X1]\n";
		else if(RISCV64 == Architecture) return "rs1_a1 rs2_a0 sd\n";
	}
	/* Should not happen but print error message. */
	fputs("Got unsupported size ", stderr);
	fputs(int2str(size, 10, TRUE), stderr);
	fputs(" when storing number in register.\n", stderr);
	line_error();
	exit(EXIT_FAILURE);
}

int is_compound_assignment(char* token)
{
	if(match("+=", token)) return TRUE;
	else if(match("-=", token)) return TRUE;
	else if(match("*=", token)) return TRUE;
	else if(match("/=", token)) return TRUE;
	else if(match("%=", token)) return TRUE;
	else if(match("<<=", token)) return TRUE;
	else if(match(">>=", token)) return TRUE;
	else if(match("&=", token)) return TRUE;
	else if(match("^=", token)) return TRUE;
	else if(match("|=", token)) return TRUE;
	return FALSE;
}

void postfix_expr_stub();
void variable_load(struct token_list* a, int num_dereference)
{
	require(NULL != global_token, "incomplete variable load received\n");
	if((match("FUNCTION", a->type->name) || match("FUNCTION*", a->type->name)) && match("(", global_token->s))
	{
		function_call(int2str(a->depth, 10, TRUE), TRUE);
		return;
	}
	current_target = a->type;

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("ADDI R0 R14 ");
	else if(X86 == Architecture) emit_out("lea_eax,[ebp+DWORD] %");
	else if(AMD64 == Architecture) emit_out("lea_rax,[rbp+DWORD] %");
	else if(ARMV7L == Architecture) emit_out("!");
	else if(AARCH64 == Architecture) emit_out("SET_X0_FROM_BP\nLOAD_W1_AHEAD\nSKIP_32_DATA\n%");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 rs1_fp !");

	emit_out(int2str(a->depth, 10, TRUE));
	if(ARMV7L == Architecture) emit_out(" R0 SUB BP ARITH_ALWAYS");
	else if(AARCH64 == Architecture) emit_out("\nSUB_X0_X0_X1\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out(" addi");
	emit_out("\n");

	if(TRUE == Address_of) return;
	if(match(".", global_token->s))
	{
		postfix_expr_stub();
		return;
	}
	if(!match("=", global_token->s) && !is_compound_assignment(global_token->s))
	{
		emit_out(load_value(current_target->size, current_target->is_signed));
	}

	while (num_dereference > 0)
	{
		current_target = current_target->type;
		emit_out(load_value(current_target->size, current_target->is_signed));
		num_dereference = num_dereference - 1;
	}
}

void function_load(struct token_list* a)
{
	require(NULL != global_token, "incomplete function load\n");
	if(match("(", global_token->s))
	{
		function_call(a->s, FALSE);
		return;
	}

	if((KNIGHT_NATIVE == Architecture) || (KNIGHT_POSIX == Architecture)) emit_out("LOADR R0 4\nJUMP 4\n&FUNCTION_");
	else if(X86 == Architecture) emit_out("mov_eax, &FUNCTION_");
	else if(AMD64 == Architecture) emit_out("lea_rax,[rip+DWORD] %FUNCTION_");
	else if(ARMV7L == Architecture) emit_out("!0 R0 LOAD32 R15 MEMORY\n~0 JUMP_ALWAYS\n&FUNCTION_");
	else if(AARCH64 == Architecture) emit_out("LOAD_W0_AHEAD\nSKIP_32_DATA\n&FUNCTION_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 ~FUNCTION_");
	emit_out(a->s);
	if(RISCV32 == Architecture)
	{
		emit_out(" auipc\n");
		emit_out("rd_a0 rs1_a0 !FUNCTION_");
		emit_out(a->s);
		emit_out(" addi");
	}
	else if(RISCV64 == Architecture)
	{
		emit_out(" auipc\n");
		emit_out("rd_a0 rs1_a0 !FUNCTION_");
		emit_out(a->s);
		emit_out(" addiw");
	}
	emit_out("\n");
}

void global_load(struct token_list* a)
{
	current_target = a->type;
	if((KNIGHT_NATIVE == Architecture) || (KNIGHT_POSIX == Architecture)) emit_out("LOADR R0 4\nJUMP 4\n&GLOBAL_");
	else if(X86 == Architecture) emit_out("mov_eax, &GLOBAL_");
	else if(AMD64 == Architecture) emit_out("lea_rax,[rip+DWORD] %GLOBAL_");
	else if(ARMV7L == Architecture) emit_out("!0 R0 LOAD32 R15 MEMORY\n~0 JUMP_ALWAYS\n&GLOBAL_");
	else if(AARCH64 == Architecture) emit_out("LOAD_W0_AHEAD\nSKIP_32_DATA\n&GLOBAL_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 ~GLOBAL_");
	emit_out(a->s);
	if((RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		emit_out(" auipc\n");
		emit_out("rd_a0 rs1_a0 !GLOBAL_");
		emit_out(a->s);
		emit_out(" addi");
	}
	emit_out("\n");

	require(NULL != global_token, "unterminated global load\n");
	if(TRUE == Address_of) return;
	if(match(".", global_token->s))
	{
		postfix_expr_stub();
		return;
	}
	if(match("=", global_token->s) || is_compound_assignment(global_token->s)) return;

	emit_out(load_value(register_size, current_target->is_signed));
}

/*
 * primary-expr:
 * FAILURE
 * "String"
 * 'Char'
 * [0-9]*
 * [a-z,A-Z]*
 * ( expression )
 */

void primary_expr_failure()
{
	require(NULL != global_token, "hit EOF when expecting primary expression\n");
	line_error();
	fputs("Received ", stderr);
	fputs(global_token->s, stderr);
	fputs(" in primary_expr\n", stderr);
	exit(EXIT_FAILURE);
}

void primary_expr_string()
{
	char* number_string = int2str(current_count, 10, TRUE);
	current_count = current_count + 1;
	if((KNIGHT_NATIVE == Architecture) || (KNIGHT_POSIX == Architecture)) emit_out("LOADR R0 4\nJUMP 4\n&STRING_");
	else if(X86 == Architecture) emit_out("mov_eax, &STRING_");
	else if(AMD64 == Architecture) emit_out("lea_rax,[rip+DWORD] %STRING_");
	else if(ARMV7L == Architecture) emit_out("!0 R0 LOAD32 R15 MEMORY\n~0 JUMP_ALWAYS\n&STRING_");
	else if(AARCH64 == Architecture) emit_out("LOAD_W0_AHEAD\nSKIP_32_DATA\n&STRING_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 ~STRING_");
	uniqueID_out(function->s, number_string);
	if((RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		emit_out("auipc\n");
		emit_out("rd_a0 rs1_a0 !STRING_");
		uniqueID_out(function->s, number_string);
		emit_out("addi\n");
	}

	/* The target */
	strings_list = emit(":STRING_", strings_list);
	strings_list = uniqueID(function->s, strings_list, number_string);

	/* catch case of just "foo" from segfaulting */
	require(NULL != global_token->next, "a string by itself is not valid C\n");

	/* Parse the string */
	if('"' != global_token->next->s[0])
	{
		strings_list = emit(parse_string(global_token->s), strings_list);
		global_token = global_token->next;
	}
	else
	{
		char* s = calloc(MAX_STRING, sizeof(char));

		/* prefix leading string */
		s[0] = '"';
		int i = 1;

		int j;
		while('"' == global_token->s[0])
		{
			/* Step past the leading '"' */
			j = 1;

			/* Copy the rest of the string as is */
			while(0 != global_token->s[j])
			{
				require(i < MAX_STRING, "concat string exceeded max string length\n");
				s[i] = global_token->s[j];
				i = i + 1;
				j = j + 1;
			}

			/* Move on to the next token */
			global_token = global_token->next;
			require(NULL != global_token, "multi-string null is not valid C\n");
		}

		/* Now use it */
		strings_list = emit(parse_string(s), strings_list);
	}
}

void primary_expr_char()
{
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("LOADI R0 ");
	else if(X86 == Architecture) emit_out("mov_eax, %");
	else if(AMD64 == Architecture) emit_out("mov_rax, %");
	else if(ARMV7L == Architecture) emit_out("!");
	else if(AARCH64 == Architecture) emit_out("LOAD_W0_AHEAD\nSKIP_32_DATA\n%");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 !");
	emit_out(int2str(escape_lookup(global_token->s + 1), 10, TRUE));
	if(ARMV7L == Architecture) emit_out(" R0 LOADI8_ALWAYS");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out(" addi");
	emit_out("\n");
	global_token = global_token->next;
}

int hex2char(int c)
{
	if((c >= 0) && (c <= 9)) return (c + 48);
	else if((c >= 10) && (c <= 15)) return (c + 55);
	else return -1;
}

char* number_to_hex(int a, int bytes)
{
	require(bytes > 0, "number to hex must have a positive number of bytes greater than zero\n");
	char* result = calloc(1 + (bytes << 1), sizeof(char));
	if(NULL == result)
	{
		fputs("calloc failed in number_to_hex\n", stderr);
		exit(EXIT_FAILURE);
	}
	int i = 0;

	int divisor = (bytes << 3);
	require(divisor > 0, "unexpected wrap around in number_to_hex\n");

	/* Simply collect numbers until divisor is gone */
	while(0 != divisor)
	{
		divisor = divisor - 4;
		result[i] = hex2char((a >> divisor) & 0xF);
		i = i + 1;
	}

	return result;
}

void primary_expr_number(char* s)
{
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
	{
		int size = strtoint(s);
		if((32767 > size) && (size > -32768))
		{
			emit_out("LOADI R0 ");
			emit_out(s);
		}
		else
		{
			emit_out("LOADR R0 4\nJUMP 4\n'");
			emit_out(number_to_hex(size, register_size));
			emit_out("'");
		}
	}
	else if(X86 == Architecture)
	{
		emit_out("mov_eax, %");
		emit_out(s);
	}
	else if(AMD64 == Architecture)
	{
		emit_out("mov_rax, %");
		emit_out(s);
	}
	else if(ARMV7L == Architecture)
	{
		emit_out("!0 R0 LOAD32 R15 MEMORY\n~0 JUMP_ALWAYS\n%");
		emit_out(s);
	}
	else if(AARCH64 == Architecture)
	{
		emit_out("LOAD_W0_AHEAD\nSKIP_32_DATA\n%");
		emit_out(s);
	}
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		int size = strtoint(s);
		if((2047 > size) && (size > -2048))
		{
			emit_out("rd_a0 !");
			emit_out(s);
			emit_out(" addi");
		}
		else if (0 == (size >> 30))
		{
			emit_out("rd_a0 ~");
			emit_out(s);
			emit_out(" lui\n");
			emit_out("rd_a0 rs1_a0 !");
			emit_out(s);
			emit_out(" addi");
		}
		else
		{
			int high = size >> 30;
			int low = ((size >> 30) << 30) ^ size;
			emit_out("rd_a0 ~");
			emit_out(int2str(high, 10, TRUE));
			emit_out(" lui\n");
			emit_out("rd_a0 rs1_a0 !");
			emit_out(int2str(high, 10, TRUE));
			emit_out(" addi\n");
			emit_out("rd_a0 rs1_a0 rs2_x30 slli\n");
			emit_out("rd_t1 ~");
			emit_out(int2str(low, 10, TRUE));
			emit_out(" lui\n");
			emit_out("rd_t1 rs1_t1 !");
			emit_out(int2str(low, 10, TRUE));
			emit_out(" addi\n");
			emit_out("rd_a0 rs1_a0 rs2_t1 or\n");
		}
	}
	emit_out("\n");
}

void primary_expr_variable()
{
	int num_dereference = 0;
	while(global_token->s[0] == '*') {
		global_token = global_token->next;
		require(NULL != global_token, "Walked off the end of a variable dereference\n");
		num_dereference = num_dereference + 1;
	}
	char* s = global_token->s;
	global_token = global_token->next;
	struct token_list* a = sym_lookup(s, global_constant_list);
	if(NULL != a)
	{
		constant_load(a->arguments->s);
		return;
	}

	a = sym_lookup(s, function->locals);
	if(NULL != a)
	{
		variable_load(a, num_dereference);
		return;
	}

	a = sym_lookup(s, function->arguments);
	if(NULL != a)
	{
		variable_load(a, num_dereference);
		return;
	}

	a = sym_lookup(s, global_function_list);
	if(NULL != a)
	{
		function_load(a);
		return;
	}

	a = sym_lookup(s, global_symbol_list);
	if(NULL != a)
	{
		global_load(a);
		return;
	}

	line_error();
	fputs(s ,stderr);
	fputs(" is not a defined symbol\n", stderr);
	exit(EXIT_FAILURE);
}

void primary_expr();
struct type* promote_type(struct type* a, struct type* b)
{
	require(NULL != b, "impossible case 1 in promote_type\n");
	require(NULL != a, "impossible case 2 in promote_type\n");

	if(a == b) return a;

	struct type* i;
	for(i = global_types; NULL != i; i = i->next)
	{
		if(a->name == i->name) break;
		if(b->name == i->name) break;
		if(a->name == i->indirect->name) break;
		if(b->name == i->indirect->name) break;
		if(a->name == i->indirect->indirect->name) break;
		if(b->name == i->indirect->indirect->name) break;
	}
	require(NULL != i, "impossible case 3 in promote_type\n");
	return i;
}

void common_recursion(FUNCTION f)
{
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("PUSHR R0 R15\t#_common_recursion\n");
	else if(X86 == Architecture) emit_out("push_eax\t#_common_recursion\n");
	else if(AMD64 == Architecture) emit_out("push_rax\t#_common_recursion\n");
	else if(ARMV7L == Architecture) emit_out("{R0} PUSH_ALWAYS\t#_common_recursion\n");
	else if(AARCH64 == Architecture) emit_out("PUSH_X0\t#_common_recursion\n");
	else if(RISCV32 == Architecture) emit_out("rd_sp rs1_sp !-4 addi\t# _common_recursion\nrs1_sp rs2_a0 sw\n");
	else if(RISCV64 == Architecture) emit_out("rd_sp rs1_sp !-8 addi\t# _common_recursion\nrs1_sp rs2_a0 sd\n");

	struct type* last_type = current_target;
	global_token = global_token->next;
	require(NULL != global_token, "Received EOF in common_recursion\n");
	f();
	current_target = promote_type(current_target, last_type);

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("POPR R1 R15\t# _common_recursion\n");
	else if(X86 == Architecture) emit_out("pop_ebx\t# _common_recursion\n");
	else if(AMD64 == Architecture) emit_out("pop_rbx\t# _common_recursion\n");
	else if(ARMV7L == Architecture) emit_out("{R1} POP_ALWAYS\t# _common_recursion\n");
	else if(AARCH64 == Architecture) emit_out("POP_X1\t# _common_recursion\n");
	else if(RISCV32 == Architecture) emit_out("rd_a1 rs1_sp lw\nrd_sp rs1_sp !4 addi\t# _common_recursion\n");
	else if(RISCV64 == Architecture) emit_out("rd_a1 rs1_sp ld\nrd_sp rs1_sp !8 addi\t# _common_recursion\n");
}

void general_recursion(FUNCTION f, char* s, char* name, FUNCTION iterate)
{
	require(NULL != global_token, "Received EOF in general_recursion\n");
	if(match(name, global_token->s))
	{
		common_recursion(f);
		emit_out(s);
		iterate();
	}
}

void arithmetic_recursion(FUNCTION f, char* s1, char* s2, char* name, FUNCTION iterate)
{
	require(NULL != global_token, "Received EOF in arithmetic_recursion\n");
	if(match(name, global_token->s))
	{
		common_recursion(f);
		if(NULL == current_target)
		{
			emit_out(s1);
		}
		else if(current_target->is_signed)
		{
			emit_out(s1);
		}
		else
		{
			emit_out(s2);
		}
		iterate();
	}
}


/*
 * postfix-expr:
 *         primary-expr
 *         postfix-expr [ expression ]
 *         postfix-expr ( expression-list-opt )
 *         postfix-expr -> member
 *         postfix-expr . member
 */
struct type* lookup_member(struct type* parent, char* name);
void postfix_expr_arrow()
{
	emit_out("# looking up offset\n");
	global_token = global_token->next;
	require(NULL != global_token, "naked -> not allowed\n");

	struct type* i = lookup_member(current_target, global_token->s);
	current_target = i->type;
	global_token = global_token->next;
	require(NULL != global_token, "Unterminated -> expression not allowed\n");

	if(0 != i->offset)
	{
		emit_out("# -> offset calculation\n");
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
		{
			emit_out("ADDUI R0 R0 ");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out("\n");
		}
		else if(X86 == Architecture)
		{
			emit_out("mov_ebx, %");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out("\nadd_eax,ebx\n");
		}
		else if(AMD64 == Architecture)
		{
			emit_out("mov_rbx, %");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out("\nadd_rax,rbx\n");
		}
		else if(ARMV7L == Architecture)
		{
			emit_out("!0 R1 LOAD32 R15 MEMORY\n~0 JUMP_ALWAYS\n%");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out("\n'0' R0 R0 ADD R1 ARITH2_ALWAYS\n");
		}
		else if(AARCH64 == Architecture)
		{
			emit_out("LOAD_W1_AHEAD\nSKIP_32_DATA\n%");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out("\nADD_X0_X1_X0\n");
		}
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
		{
			emit_out("rd_a1 !");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out(" addi\n");
			emit_out("rd_a0 rs1_a1 rs2_a0 add\n");
		}
	}

	/* We don't yet support assigning structs to structs */
	if((!match("=", global_token->s) && !is_compound_assignment(global_token->s) && (register_size >= i->size)))
	{
		emit_out(load_value(i->size, i->is_signed));
	}
}

void postfix_expr_dot()
{
	maybe_bootstrap_error("Member access using .");
	emit_out("# looking up offset\n");
	global_token = global_token->next;
	require(NULL != global_token, "naked . not allowed\n");

	struct type* i = lookup_member(current_target, global_token->s);
	current_target = i->type;
	global_token = global_token->next;
	require(NULL != global_token, "Unterminated . expression not allowed\n");

	if(0 != i->offset)
	{
		emit_out("# . offset calculation\n");
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
		{
			emit_out("ADDUI R0 R0 ");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out("\n");
		}
		else if(X86 == Architecture)
		{
			emit_out("mov_ebx, %");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out("\nadd_eax,ebx\n");
		}
		else if(AMD64 == Architecture)
		{
			emit_out("mov_rbx, %");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out("\nadd_rax,rbx\n");
		}
		else if(ARMV7L == Architecture)
		{
			emit_out("!0 R1 LOAD32 R15 MEMORY\n~0 JUMP_ALWAYS\n%");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out("\n'0' R0 R0 ADD R1 ARITH2_ALWAYS\n");
		}
		else if(AARCH64 == Architecture)
		{
			emit_out("LOAD_W1_AHEAD\nSKIP_32_DATA\n%");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out("\nADD_X0_X1_X0\n");
		}
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
		{
			emit_out("rd_a1 !");
			emit_out(int2str(i->offset, 10, TRUE));
			emit_out(" addi\n");
			emit_out("rd_a0 rs1_a1 rs2_a0 add\n");
		}
	}
	if(match("=", global_token->s) || is_compound_assignment(global_token->s)) return;
	if(match("[", global_token->s)) return;

	emit_out(load_value(current_target->size, current_target->is_signed));
}

void postfix_expr_array()
{
	struct type* array = current_target;
	common_recursion(expression);
	current_target = array;
	require(NULL != current_target, "Arrays only apply to variables\n");

	char* assign = load_value(register_size, current_target->is_signed);

	/* Add support for Ints */
	if(match("char*", current_target->name))
	{
		assign = load_value(1, TRUE);
	}
	else
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("PUSHR R1 R15\nLOADI R1 ");
		else if(X86 == Architecture) emit_out("push_ebx\nmov_ebx, %");
		else if(AMD64 == Architecture) emit_out("push_rbx\nmov_rbx, %");
		else if(ARMV7L == Architecture) emit_out("{R1} PUSH_ALWAYS\n!0 R1 LOAD32 R15 MEMORY\n~0 JUMP_ALWAYS\n%");
		else if(AARCH64 == Architecture) emit_out("PUSH_X1\nLOAD_W1_AHEAD\nSKIP_32_DATA\n%");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a2 rs1_a1 addi\nrd_a1 !");
		emit_out(int2str(current_target->type->size, 10, TRUE));
		if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out(" addi");

		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("\nMULU R0 R1 R0\nPOPR R1 R15\n");
		else if(X86 == Architecture) emit_out("\nmul_ebx\npop_ebx\n");
		else if(AMD64 == Architecture) emit_out("\nmul_rbx\npop_rbx\n");
		else if(ARMV7L == Architecture) emit_out("\n'9' R0 '0' R1 MUL R0 ARITH2_ALWAYS\n{R1} POP_ALWAYS\n");
		else if(AARCH64 == Architecture) emit_out("\nMUL_X0_X1_X0\nPOP_X1\n");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("\nrd_a0 rs1_a1 rs2_a0 mul\nrd_a1 rs1_a2 addi\n");
	}

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("ADD R0 R0 R1\n");
	else if(X86 == Architecture) emit_out("add_eax,ebx\n");
	else if(AMD64 == Architecture) emit_out("add_rax,rbx\n");
	else if(ARMV7L == Architecture) emit_out("'0' R0 R0 ADD R1 ARITH2_ALWAYS\n");
	else if(AARCH64 == Architecture) emit_out("ADD_X0_X1_X0\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 rs1_a1 rs2_a0 add\n");

	require_match("ERROR in postfix_expr\nMissing ]\n", "]");
	require(NULL != global_token, "truncated array expression\n");

	if(match("=", global_token->s) || is_compound_assignment(global_token->s) || match(".", global_token->s))
	{
		assign = "";
	}
	if(match("[", global_token->s))
	{
		current_target = current_target->type;
	}

	emit_out(assign);
}

/*
 * unary-expr:
 *         &postfix-expr
 *         - postfix-expr
 *         !postfix-expr
 *         sizeof ( type )
 */
struct type* type_name();
void unary_expr_sizeof()
{
	global_token = global_token->next;
	require(NULL != global_token, "Received EOF when starting sizeof\n");
	require_match("ERROR in unary_expr\nMissing (\n", "(");
	struct type* a = type_name();
	require_match("ERROR in unary_expr\nMissing )\n", ")");

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("LOADUI R0 ");
	else if(X86 == Architecture) emit_out("mov_eax, %");
	else if(AMD64 == Architecture) emit_out("mov_rax, %");
	else if(ARMV7L == Architecture) emit_out("!0 R0 LOAD32 R15 MEMORY\n~0 JUMP_ALWAYS\n%");
	else if(AARCH64 == Architecture) emit_out("LOAD_W0_AHEAD\nSKIP_32_DATA\n%");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 !");
	emit_out(int2str(a->size, 10, TRUE));
	if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out(" addi");
	emit_out("\n");
}

void postfix_expr_stub()
{
	require(NULL != global_token, "Unexpected EOF, improperly terminated primary expression\n");
	if(match("[", global_token->s))
	{
		postfix_expr_array();
		postfix_expr_stub();
	}

	if(match("->", global_token->s))
	{
		postfix_expr_arrow();
		postfix_expr_stub();
	}

	if(match(".", global_token->s))
	{
		postfix_expr_dot();
		postfix_expr_stub();
	}
}

void postfix_expr()
{
	primary_expr();
	postfix_expr_stub();
}

/*
 * additive-expr:
 *         postfix-expr
 *         additive-expr * postfix-expr
 *         additive-expr / postfix-expr
 *         additive-expr % postfix-expr
 *         additive-expr + postfix-expr
 *         additive-expr - postfix-expr
 *         additive-expr << postfix-expr
 *         additive-expr >> postfix-expr
 */
void additive_expr_stub_a()
{
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
	{
		arithmetic_recursion(postfix_expr, "MUL R0 R1 R0\n", "MULU R0 R1 R0\n", "*", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "DIV R0 R1 R0\n", "DIVU R0 R1 R0\n", "/", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "MOD R0 R1 R0\n", "MODU R0 R1 R0\n", "%", additive_expr_stub_a);
	}
	else if(X86 == Architecture)
	{
		arithmetic_recursion(postfix_expr, "imul_ebx\n", "mul_ebx\n", "*", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "xchg_ebx,eax\ncdq\nidiv_ebx\n", "xchg_ebx,eax\nmov_edx, %0\ndiv_ebx\n", "/", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "xchg_ebx,eax\ncdq\nidiv_ebx\nmov_eax,edx\n", "xchg_ebx,eax\nmov_edx, %0\ndiv_ebx\nmov_eax,edx\n", "%", additive_expr_stub_a);
	}
	else if(AMD64 == Architecture)
	{
		arithmetic_recursion(postfix_expr, "imul_rbx\n", "mul_rbx\n", "*", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "xchg_rbx,rax\ncqo\nidiv_rbx\n", "xchg_rbx,rax\nmov_rdx, %0\ndiv_rbx\n", "/", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "xchg_rbx,rax\ncqo\nidiv_rbx\nmov_rax,rdx\n", "xchg_rbx,rax\nmov_rdx, %0\ndiv_rbx\nmov_rax,rdx\n", "%", additive_expr_stub_a);
	}
	else if(ARMV7L == Architecture)
	{
		arithmetic_recursion(postfix_expr, "'9' R0 '0' R1 MULS R0 ARITH2_ALWAYS\n", "'9' R0 '0' R1 MUL R0 ARITH2_ALWAYS\n", "*", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "{LR} PUSH_ALWAYS\n^~divides CALL_ALWAYS\n{LR} POP_ALWAYS\n", "{LR} PUSH_ALWAYS\n^~divide CALL_ALWAYS\n{LR} POP_ALWAYS\n", "/", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "{LR} PUSH_ALWAYS\n^~moduluss CALL_ALWAYS\n{LR} POP_ALWAYS\n", "{LR} PUSH_ALWAYS\n^~modulus CALL_ALWAYS\n{LR} POP_ALWAYS\n", "%", additive_expr_stub_a);
	}
	else if(AARCH64 == Architecture)
	{
		general_recursion(postfix_expr, "MUL_X0_X1_X0\n", "*", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "SDIV_X0_X1_X0\n", "UDIV_X0_X1_X0\n", "/", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "SDIV_X2_X1_X0\nMSUB_X0_X0_X2_X1\n", "UDIV_X2_X1_X0\nMSUB_X0_X0_X2_X1\n", "%", additive_expr_stub_a);
	}
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		general_recursion(postfix_expr, "rd_a0 rs1_a1 rs2_a0 mul\n", "*", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "rd_a0 rs1_a1 rs2_a0 div\n", "rd_a0 rs1_a1 rs2_a0 divu\n", "/", additive_expr_stub_a);
		arithmetic_recursion(postfix_expr, "rd_a0 rs1_a1 rs2_a0 rem\n", "rd_a0 rs1_a1 rs2_a0 remu\n", "%", additive_expr_stub_a);
	}
}


void additive_expr_a()
{
	postfix_expr();
	additive_expr_stub_a();
}

void additive_expr_stub_b()
{
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
	{
		arithmetic_recursion(additive_expr_a, "ADD R0 R1 R0\n", "ADDU R0 R1 R0\n", "+", additive_expr_stub_b);
		arithmetic_recursion(additive_expr_a, "SUB R0 R1 R0\n", "SUBU R0 R1 R0\n", "-", additive_expr_stub_b);
	}
	else if(X86 == Architecture)
	{
		arithmetic_recursion(additive_expr_a, "add_eax,ebx\n", "add_eax,ebx\n", "+", additive_expr_stub_b);
		arithmetic_recursion(additive_expr_a, "sub_ebx,eax\nmov_eax,ebx\n", "sub_ebx,eax\nmov_eax,ebx\n", "-", additive_expr_stub_b);
	}
	else if(AMD64 == Architecture)
	{
		arithmetic_recursion(additive_expr_a, "add_rax,rbx\n", "add_rax,rbx\n", "+", additive_expr_stub_b);
		arithmetic_recursion(additive_expr_a, "sub_rbx,rax\nmov_rax,rbx\n", "sub_rbx,rax\nmov_rax,rbx\n", "-", additive_expr_stub_b);
	}
	else if(ARMV7L == Architecture)
	{
		arithmetic_recursion(additive_expr_a, "'0' R0 R0 ADD R1 ARITH2_ALWAYS\n", "'0' R0 R0 ADD R1 ARITH2_ALWAYS\n", "+", additive_expr_stub_b);
		arithmetic_recursion(additive_expr_a, "'0' R0 R0 SUB R1 ARITH2_ALWAYS\n", "'0' R0 R0 SUB R1 ARITH2_ALWAYS\n", "-", additive_expr_stub_b);
	}
	else if(AARCH64 == Architecture)
	{
		general_recursion(additive_expr_a, "ADD_X0_X1_X0\n", "+", additive_expr_stub_b);
		general_recursion(additive_expr_a, "SUB_X0_X1_X0\n", "-", additive_expr_stub_b);
	}
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		general_recursion(additive_expr_a, "rd_a0 rs1_a1 rs2_a0 add\n", "+", additive_expr_stub_b);
		general_recursion(additive_expr_a, "rd_a0 rs1_a1 rs2_a0 sub\n", "-", additive_expr_stub_b);
	}
}


void additive_expr_b()
{
	additive_expr_a();
	additive_expr_stub_b();
}

void additive_expr_stub_c()
{
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
	{
		arithmetic_recursion(additive_expr_b, "SAL R0 R1 R0\n", "SL0 R0 R1 R0\n", "<<", additive_expr_stub_c);
		arithmetic_recursion(additive_expr_b, "SAR R0 R1 R0\n", "SR0 R0 R1 R0\n", ">>", additive_expr_stub_c);
	}
	else if(X86 == Architecture)
	{
		arithmetic_recursion(additive_expr_b, "mov_ecx,eax\nmov_eax,ebx\nsal_eax,cl\n", "mov_ecx,eax\nmov_eax,ebx\nshl_eax,cl\n", "<<", additive_expr_stub_c);
		arithmetic_recursion(additive_expr_b, "mov_ecx,eax\nmov_eax,ebx\nsar_eax,cl\n", "mov_ecx,eax\nmov_eax,ebx\nshr_eax,cl\n", ">>", additive_expr_stub_c);
	}
	else if(AMD64 == Architecture)
	{
		arithmetic_recursion(additive_expr_b, "mov_rcx,rax\nmov_rax,rbx\nsal_rax,cl\n", "mov_rcx,rax\nmov_rax,rbx\nshl_rax,cl\n", "<<", additive_expr_stub_c);
		arithmetic_recursion(additive_expr_b, "mov_rcx,rax\nmov_rax,rbx\nsar_rax,cl\n", "mov_rcx,rax\nmov_rax,rbx\nshr_rax,cl\n", ">>", additive_expr_stub_c);
	}
	else if(ARMV7L == Architecture)
	{
		arithmetic_recursion(additive_expr_b, "LEFT R1 R0 R0 SHIFT AUX_ALWAYS\n", "LEFT R1 R0 R0 SHIFT AUX_ALWAYS\n", "<<", additive_expr_stub_c);
		arithmetic_recursion(additive_expr_b, "ARITH_RIGHT R1 R0 R0 SHIFT AUX_ALWAYS\n", "RIGHT R1 R0 R0 SHIFT AUX_ALWAYS\n", ">>", additive_expr_stub_c);
	}
	else if(AARCH64 == Architecture)
	{
		general_recursion(additive_expr_b, "LSHIFT_X0_X1_X0\n", "<<", additive_expr_stub_c);
		arithmetic_recursion(additive_expr_b, "ARITH_RSHIFT_X0_X1_X0\n", "LOGICAL_RSHIFT_X0_X1_X0\n", ">>", additive_expr_stub_c);
	}
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		general_recursion(additive_expr_b, "rd_a0 rs1_a1 rs2_a0 sll\n", "<<", additive_expr_stub_c);
		arithmetic_recursion(additive_expr_b, "rd_a0 rs1_a1 rs2_a0 sra\n", "rd_a0 rs1_a1 rs2_a0 srl\n", ">>", additive_expr_stub_c);
	}
}


void additive_expr_c()
{
	additive_expr_b();
	additive_expr_stub_c();
}


/*
 * relational-expr:
 *         additive_expr
 *         relational-expr < additive_expr
 *         relational-expr <= additive_expr
 *         relational-expr >= additive_expr
 *         relational-expr > additive_expr
 */

void relational_expr_stub()
{
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
	{
		arithmetic_recursion(additive_expr_c, "CMP R0 R1 R0\nSET.L R0 R0 1\n", "CMPU R0 R1 R0\nSET.L R0 R0 1\n", "<", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "CMP R0 R1 R0\nSET.LE R0 R0 1\n", "CMPU R0 R1 R0\nSET.LE R0 R0 1\n", "<=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "CMP R0 R1 R0\nSET.GE R0 R0 1\n", "CMPU R0 R1 R0\nSET.GE R0 R0 1\n", ">=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "CMP R0 R1 R0\nSET.G R0 R0 1\n", "CMPU R0 R1 R0\nSET.G R0 R0 1\n", ">", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "CMP R0 R1 R0\nSET.E R0 R0 1\n", "CMPU R0 R1 R0\nSET.E R0 R0 1\n", "==", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "CMP R0 R1 R0\nSET.NE R0 R0 1\n", "CMPU R0 R1 R0\nSET.NE R0 R0 1\n", "!=", relational_expr_stub);
	}
	else if(X86 == Architecture)
	{
		arithmetic_recursion(additive_expr_c, "cmp\nsetl_al\nmovzx_eax,al\n", "cmp\nsetb_al\nmovzx_eax,al\n", "<", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "cmp\nsetle_al\nmovzx_eax,al\n", "cmp\nsetbe_al\nmovzx_eax,al\n", "<=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "cmp\nsetge_al\nmovzx_eax,al\n", "cmp\nsetae_al\nmovzx_eax,al\n", ">=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "cmp\nsetg_al\nmovzx_eax,al\n", "cmp\nseta_al\nmovzx_eax,al\n", ">", relational_expr_stub);
		general_recursion(additive_expr_c, "cmp\nsete_al\nmovzx_eax,al\n", "==", relational_expr_stub);
		general_recursion(additive_expr_c, "cmp\nsetne_al\nmovzx_eax,al\n", "!=", relational_expr_stub);
	}
	else if(AMD64 == Architecture)
	{
		arithmetic_recursion(additive_expr_c, "cmp_rbx,rax\nsetl_al\nmovzx_rax,al\n", "cmp_rbx,rax\nsetb_al\nmovzx_rax,al\n", "<", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "cmp_rbx,rax\nsetle_al\nmovzx_rax,al\n", "cmp_rbx,rax\nsetbe_al\nmovzx_rax,al\n", "<=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "cmp_rbx,rax\nsetge_al\nmovzx_rax,al\n", "cmp_rbx,rax\nsetae_al\nmovzx_rax,al\n", ">=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "cmp_rbx,rax\nsetg_al\nmovzx_rax,al\n", "cmp_rbx,rax\nseta_al\nmovzx_rax,al\n", ">", relational_expr_stub);
		general_recursion(additive_expr_c, "cmp_rbx,rax\nsete_al\nmovzx_rax,al\n", "==", relational_expr_stub);
		general_recursion(additive_expr_c, "cmp_rbx,rax\nsetne_al\nmovzx_rax,al\n", "!=", relational_expr_stub);
	}
	else if(ARMV7L == Architecture)
	{
		arithmetic_recursion(additive_expr_c, "'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_L\n", "'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_LO\n", "<", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_LE\n", "'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_LS\n", "<=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_GE\n", "'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_HS\n", ">=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_G\n", "'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_HI\n", ">", relational_expr_stub);
		general_recursion(additive_expr_c, "'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_EQUAL\n", "==", relational_expr_stub);
		general_recursion(additive_expr_c, "'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_NE\n", "!=", relational_expr_stub);
	}
	else if(AARCH64 == Architecture)
	{
		arithmetic_recursion(additive_expr_c, "CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_LT\nSET_X0_TO_0\n", "CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_LO\nSET_X0_TO_0\n", "<", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_LE\nSET_X0_TO_0\n", "CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_LS\nSET_X0_TO_0\n", "<=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_GE\nSET_X0_TO_0\n", "CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_HS\nSET_X0_TO_0\n", ">=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_GT\nSET_X0_TO_0\n", "CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_HI\nSET_X0_TO_0\n", ">", relational_expr_stub);
		general_recursion(additive_expr_c, "CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_EQ\nSET_X0_TO_0\n", "==", relational_expr_stub);
		general_recursion(additive_expr_c, "CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_NE\nSET_X0_TO_0\n", "!=", relational_expr_stub);
	}
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		arithmetic_recursion(additive_expr_c, "rd_a0 rs1_a1 rs2_a0 slt\n", "rd_a0 rs1_a1 rs2_a0 sltu\n", "<", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "rd_a0 rs1_a0 rs2_a1 slt\nrd_a0 rs1_a0 !1 xori\n", "rd_a0 rs1_a0 rs2_a1 sltu\nrd_a0 rs1_a0 !1 xori\n", "<=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "rd_a0 rs1_a1 rs2_a0 slt\nrd_a0 rs1_a0 !1 xori\n", "rd_a0 rs1_a1 rs2_a0 sltu\nrd_a0 rs1_a0 !1 xori\n", ">=", relational_expr_stub);
		arithmetic_recursion(additive_expr_c, "rd_a0 rs1_a0 rs2_a1 slt\n", "rd_a0 rs1_a0 rs2_a1 sltu\n", ">", relational_expr_stub);
		general_recursion(additive_expr_c, "rd_a0 rs1_a0 rs2_a1 sub\nrd_a0 rs1_a0 !1 sltiu\n", "==", relational_expr_stub);
		general_recursion(additive_expr_c, "rd_a0 rs1_a0 rs2_a1 sub\nrd_a0 rs2_a0 sltu\n", "!=", relational_expr_stub);
	}
}

void relational_expr()
{
	additive_expr_c();
	relational_expr_stub();
}

/*
 * bitwise-expr:
 *         relational-expr
 *         bitwise-expr & bitwise-expr
 *         bitwise-expr && bitwise-expr
 *         bitwise-expr | bitwise-expr
 *         bitwise-expr || bitwise-expr
 *         bitwise-expr ^ bitwise-expr
 */
void bitwise_expr_stub()
{
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
	{
		general_recursion(relational_expr, "AND R0 R0 R1\n", "&", bitwise_expr_stub);
		general_recursion(relational_expr, "AND R0 R0 R1\n", "&&", bitwise_expr_stub);
		general_recursion(relational_expr, "OR R0 R0 R1\n", "|", bitwise_expr_stub);
		general_recursion(relational_expr, "OR R0 R0 R1\n", "||", bitwise_expr_stub);
		general_recursion(relational_expr, "XOR R0 R0 R1\n", "^", bitwise_expr_stub);
	}
	else if(X86 == Architecture)
	{
		general_recursion(relational_expr, "and_eax,ebx\n", "&", bitwise_expr_stub);
		general_recursion(relational_expr, "and_eax,ebx\n", "&&", bitwise_expr_stub);
		general_recursion(relational_expr, "or_eax,ebx\n", "|", bitwise_expr_stub);
		general_recursion(relational_expr, "or_eax,ebx\n", "||", bitwise_expr_stub);
		general_recursion(relational_expr, "xor_eax,ebx\n", "^", bitwise_expr_stub);
	}
	else if(AMD64 == Architecture)
	{
		general_recursion(relational_expr, "and_rax,rbx\n", "&", bitwise_expr_stub);
		general_recursion(relational_expr, "and_rax,rbx\n", "&&", bitwise_expr_stub);
		general_recursion(relational_expr, "or_rax,rbx\n", "|", bitwise_expr_stub);
		general_recursion(relational_expr, "or_rax,rbx\n", "||", bitwise_expr_stub);
		general_recursion(relational_expr, "xor_rax,rbx\n", "^", bitwise_expr_stub);
	}
	else if(ARMV7L == Architecture)
	{
		general_recursion(relational_expr, "NO_SHIFT R0 R0 AND R1 ARITH2_ALWAYS\n", "&", bitwise_expr_stub);
		general_recursion(relational_expr, "NO_SHIFT R0 R0 AND R1 ARITH2_ALWAYS\n", "&&", bitwise_expr_stub);
		general_recursion(relational_expr, "NO_SHIFT R0 R0 OR R1 AUX_ALWAYS\n", "|", bitwise_expr_stub);
		general_recursion(relational_expr, "NO_SHIFT R0 R0 OR R1 AUX_ALWAYS\n", "||", bitwise_expr_stub);
		general_recursion(relational_expr, "'0' R0 R0 XOR R1 ARITH2_ALWAYS\n", "^", bitwise_expr_stub);
	}
	else if(AARCH64 == Architecture)
	{
		general_recursion(relational_expr, "AND_X0_X1_X0\n", "&", bitwise_expr_stub);
		general_recursion(relational_expr, "AND_X0_X1_X0\n", "&&", bitwise_expr_stub);
		general_recursion(relational_expr, "OR_X0_X1_X0\n", "|", bitwise_expr_stub);
		general_recursion(relational_expr, "OR_X0_X1_X0\n", "||", bitwise_expr_stub);
		general_recursion(relational_expr, "XOR_X0_X1_X0\n", "^", bitwise_expr_stub);
	}
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		general_recursion(relational_expr, "rd_a0 rs1_a1 rs2_a0 and\n", "&", bitwise_expr_stub);
		general_recursion(relational_expr, "rd_a0 rs1_a1 rs2_a0 and\n", "&&", bitwise_expr_stub);
		general_recursion(relational_expr, "rd_a0 rs1_a1 rs2_a0 or\n", "|", bitwise_expr_stub);
		general_recursion(relational_expr, "rd_a0 rs1_a1 rs2_a0 or\n", "||", bitwise_expr_stub);
		general_recursion(relational_expr, "rd_a0 rs1_a1 rs2_a0 xor\n", "^", bitwise_expr_stub);
	}
}


void bitwise_expr()
{
	relational_expr();
	bitwise_expr_stub();
}

/*
 * expression:
 *         bitwise-or-expr
 *         bitwise-or-expr = expression
 */

void primary_expr()
{
	require(NULL != global_token, "Received EOF where primary expression expected\n");
	if(match("&", global_token->s))
	{
		Address_of = TRUE;
		global_token = global_token->next;
		require(NULL != global_token, "Received EOF after & where primary expression expected\n");
	}
	else
	{
		Address_of = FALSE;
	}

	if(match("sizeof", global_token->s)) unary_expr_sizeof();
	else if('-' == global_token->s[0])
	{
		if(X86 == Architecture) emit_out("mov_eax, %0\n");
		else if(AMD64 == Architecture) emit_out("mov_rax, %0\n");
		else if(ARMV7L == Architecture) emit_out("!0 R0 LOADI8_ALWAYS\n");
		else if(AARCH64 == Architecture) emit_out("SET_X0_TO_0\n");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 mv\n");

		common_recursion(primary_expr);

		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("NEG R0 R0\n");
		else if(X86 == Architecture) emit_out("sub_ebx,eax\nmov_eax,ebx\n");
		else if(AMD64 == Architecture) emit_out("sub_rbx,rax\nmov_rax,rbx\n");
		else if(ARMV7L == Architecture) emit_out("'0' R0 R0 SUB R1 ARITH2_ALWAYS\n");
		else if(AARCH64 == Architecture) emit_out("SUB_X0_X1_X0\n");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 rs1_a1 rs2_a0 sub\n");
	}
	else if('!' == global_token->s[0])
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))  emit_out("LOADI R0 1\n");
		else if(X86 == Architecture) emit_out("mov_eax, %1\n");
		else if(AMD64 == Architecture) emit_out("mov_rax, %1\n");
		else if(ARMV7L == Architecture) emit_out("!1 R0 LOADI8_ALWAYS\n");
		else if(AARCH64 == Architecture) emit_out("SET_X0_TO_1\n");

		common_recursion(postfix_expr);

		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("CMPU R0 R1 R0\nSET.G R0 R0 1\n");
		else if(X86 == Architecture) emit_out("cmp\nseta_al\nmovzx_eax,al\n");
		else if(AMD64 == Architecture) emit_out("cmp_rbx,rax\nseta_al\nmovzx_rax,al\n");
		else if(ARMV7L == Architecture) emit_out("'0' R0 CMP R1 AUX_ALWAYS\n!0 R0 LOADI8_ALWAYS\n!1 R0 LOADI8_HI\n");
		else if(AARCH64 == Architecture) emit_out("CMP_X1_X0\nSET_X0_TO_1\nSKIP_INST_HI\nSET_X0_TO_0\n");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 rs1_a0 !1 sltiu\n");
	}
	else if('~' == global_token->s[0])
	{
		common_recursion(postfix_expr);

		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("NOT R0 R0\n");
		else if(X86 == Architecture) emit_out("not_eax\n");
		else if(AMD64 == Architecture) emit_out("not_rax\n");
		else if(ARMV7L == Architecture) emit_out("'0' R0 R0 MVN_ALWAYS\n");
		else if(AARCH64 == Architecture) emit_out("MVN_X0\n");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 rs1_a0 not\n");
	}
	else if(global_token->s[0] == '(')
	{
		global_token = global_token->next;
		expression();
		require_match("Error in Primary expression\nDidn't get )\n", ")");
	}
	else if(global_token->s[0] == '\'') primary_expr_char();
	else if(global_token->s[0] == '"') primary_expr_string();
	else if(in_set(global_token->s[0], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_")) primary_expr_variable();
	else if(global_token->s[0] == '*') primary_expr_variable();
	else if(in_set(global_token->s[0], "0123456789"))
	{
		primary_expr_number(global_token->s);
		global_token = global_token->next;
	}
	else primary_expr_failure();
}

char* compound_operation(char* operator, int is_signed)
{
	char* operation = "";
	if(match("+=", operator))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
		{
			if(is_signed) operation = "ADD R0 R1 R0\n";
			else operation = "ADDU R0 R1 R0\n";
		}
		else if(X86 == Architecture) operation = "add_eax,ebx\n";
		else if(AMD64 == Architecture) operation = "add_rax,rbx\n";
		else if(ARMV7L == Architecture) operation = "'0' R0 R0 ADD R1 ARITH2_ALWAYS\n";
		else if(AARCH64 == Architecture) operation = "ADD_X0_X1_X0\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) operation = "rd_a0 rs1_a1 rs2_a0 add\n";
	}
	else if(match("-=", operator))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
		{
			if(is_signed) operation = "SUB R0 R1 R0\n";
			else operation =  "SUBU R0 R1 R0\n";
		}
		else if(X86 == Architecture) operation = "sub_ebx,eax\nmov_eax,ebx\n";
		else if(AMD64 == Architecture) operation = "sub_rbx,rax\nmov_rax,rbx\n";
		else if(ARMV7L == Architecture) operation = "'0' R0 R0 SUB R1 ARITH2_ALWAYS\n";
		else if(AARCH64 == Architecture) operation = "SUB_X0_X1_X0\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) operation = "rd_a0 rs1_a1 rs2_a0 sub\n";
	}
	else if(match("*=", operator))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
		{
			if(is_signed) operation = "MUL R0 R1 R0\n";
			else operation =  "MULU R0 R1 R0\n";
		}
		else if(X86 == Architecture)
		{
			if(is_signed) operation = "imul_ebx\n";
			else operation = "mul_ebx\n";
		}
		else if(AMD64 == Architecture)
		{
			if(is_signed) operation = "imul_rbx\n";
			else operation = "mul_rbx\n";
		}
		else if(ARMV7L == Architecture) operation = "'9' R0 '0' R1 MULS R0 ARITH2_ALWAYS\n";
		else if(AARCH64 == Architecture) operation = "MUL_X0_X1_X0\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) operation = "rd_a0 rs1_a1 rs2_a0 mul\n";
	}
	else if(match("/=", operator))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
		{
			if(is_signed) operation = "DIV R0 R1 R0\n";
			else operation =  "DIVU R0 R1 R0\n";
		}
		else if(X86 == Architecture)
		{
			if (is_signed) operation = "xchg_ebx,eax\ncdq\nidiv_ebx\n";
			else operation = "xchg_ebx,eax\nmov_edx, %0\ndiv_ebx\n";
		}
		else if(AMD64 == Architecture)
		{
			if(is_signed) operation = "xchg_rbx,rax\ncqo\nidiv_rbx\n";
			else operation = "xchg_rbx,rax\nmov_rdx, %0\ndiv_rbx\n";
		}
		else if(ARMV7L == Architecture)
		{
			if(is_signed) operation = "{LR} PUSH_ALWAYS\n^~divides CALL_ALWAYS\n{LR} POP_ALWAYS\n";
			else operation = "{LR} PUSH_ALWAYS\n^~divide CALL_ALWAYS\n{LR} POP_ALWAYS\n";
		}
		else if(AARCH64 == Architecture)
		{
			if(is_signed) operation = "SDIV_X0_X1_X0\n";
			else operation = "UDIV_X0_X1_X0\n";
		}
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
		{
			if(is_signed) operation = "rd_a0 rs1_a1 rs2_a0 div\n";
			else operation = "rd_a0 rs1_a1 rs2_a0 divu\n";
		}
	}
	else if(match("%=", operator))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
		{
			if(is_signed) operation = "MOD R0 R1 R0\n";
			else operation = "MODU R0 R1 R0\n";
		}
		else if(X86 == Architecture)
		{
			if(is_signed) operation = "xchg_ebx,eax\ncdq\nidiv_ebx\nmov_eax,edx\n";
			else operation = "xchg_ebx,eax\nmov_edx, %0\ndiv_ebx\nmov_eax,edx\n";
		}
		else if(AMD64 == Architecture)
		{
			if(is_signed) operation = "xchg_rbx,rax\ncqo\nidiv_rbx\nmov_rax,rdx\n";
			else operation = "xchg_rbx,rax\nmov_rdx, %0\ndiv_rbx\nmov_rax,rdx\n";
		}
		else if(ARMV7L == Architecture)
		{
			if(is_signed) operation = "{LR} PUSH_ALWAYS\n^~moduluss CALL_ALWAYS\n{LR} POP_ALWAYS\n";
			else operation = "{LR} PUSH_ALWAYS\n^~modulus CALL_ALWAYS\n{LR} POP_ALWAYS\n";
		}
		else if(AARCH64 == Architecture)
		{
			if(is_signed) operation = "SDIV_X2_X1_X0\nMSUB_X0_X0_X2_X1\n";
			else operation = "UDIV_X2_X1_X0\nMSUB_X0_X0_X2_X1\n";
		}
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
		{
			if(is_signed) operation = "rd_a0 rs1_a1 rs2_a0 rem\n";
			else operation = "rd_a0 rs1_a1 rs2_a0 remu\n";
		}
	}
	else if(match("<<=", operator))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
		{
			if(is_signed) operation = "SAL R0 R1 R0\n";
			else operation = "SL0 R0 R1 R0\n";
		}
		else if(X86 == Architecture)
		{
			if(is_signed) operation = "mov_ecx,eax\nmov_eax,ebx\nsal_eax,cl\n";
			else operation = "mov_ecx,eax\nmov_eax,ebx\nshl_eax,cl\n";
		}
		else if(AMD64 == Architecture)
		{
			if(is_signed) operation = "mov_rcx,rax\nmov_rax,rbx\nsal_rax,cl\n";
			else operation = "mov_rcx,rax\nmov_rax,rbx\nshl_rax,cl\n";
		}
		else if(ARMV7L == Architecture) operation = "LEFT R1 R0 R0 SHIFT AUX_ALWAYS\n";
		else if(AARCH64 == Architecture) operation = "LSHIFT_X0_X1_X0\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) operation = "rd_a0 rs1_a1 rs2_a0 sll\n";
	}
	else if(match(">>=", operator))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture))
		{
			if(is_signed) operation = "SAR R0 R1 R0\n";
			else operation = "SR0 R0 R1 R0\n";
		}
		else if(X86 == Architecture)
		{
			if(is_signed) operation = "mov_ecx,eax\nmov_eax,ebx\nsar_eax,cl\n";
			else operation = "mov_ecx,eax\nmov_eax,ebx\nshr_eax,cl\n";
		}
		else if(AMD64 == Architecture)
		{
			if(is_signed) operation = "mov_rcx,rax\nmov_rax,rbx\nsar_rax,cl\n";
			else operation = "mov_rcx,rax\nmov_rax,rbx\nshr_rax,cl\n";
		}
		else if(ARMV7L == Architecture)
		{
			if(is_signed) operation = "ARITH_RIGHT R1 R0 R0 SHIFT AUX_ALWAYS\n";
			else operation = "RIGHT R1 R0 R0 SHIFT AUX_ALWAYS\n";
		}
		else if(AARCH64 == Architecture)
		{
			if(is_signed) operation = "ARITH_RSHIFT_X0_X1_X0\n";
			else operation = "LOGICAL_RSHIFT_X0_X1_X0\n";
		}
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
		{
			if(is_signed) operation = "rd_a0 rs1_a1 rs2_a0 sra\n";
			else operation = "rd_a0 rs1_a1 rs2_a0 srl\n";
		}
	}
	else if(match("&=", operator))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) operation = "AND R0 R0 R1\n";
		else if(X86 == Architecture) operation = "and_eax,ebx\n";
		else if(AMD64 == Architecture) operation = "and_rax,rbx\n";
		else if(ARMV7L == Architecture) operation = "NO_SHIFT R0 R0 AND R1 ARITH2_ALWAYS\n";
		else if(AARCH64 == Architecture) operation = "AND_X0_X1_X0\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) operation = "rd_a0 rs1_a1 rs2_a0 and\n";
	}
	else if(match("^=", operator))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) operation = "XOR R0 R0 R1\n";
		else if(X86 == Architecture) operation = "xor_eax,ebx\n";
		else if(AMD64 == Architecture) operation = "xor_rax,rbx\n";
		else if(ARMV7L == Architecture) operation = "'0' R0 R0 XOR R1 ARITH2_ALWAYS\n";
		else if(AARCH64 == Architecture) operation = "XOR_X0_X1_X0\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) operation = "rd_a0 rs1_a1 rs2_a0 xor\n";
	}
	else if(match("|=", operator))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) operation = "OR R0 R0 R1\n";
		else if(X86 == Architecture) operation = "or_eax,ebx\n";
		else if(AMD64 == Architecture) operation = "or_rax,rbx\n";
		else if(ARMV7L == Architecture) operation = "NO_SHIFT R0 R0 OR R1 AUX_ALWAYS\n";
		else if(AARCH64 == Architecture) operation = "OR_X0_X1_X0\n";
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) operation = "rd_a0 rs1_a1 rs2_a0 or\n";
	}
	else
	{
		fputs("Found illegal compound assignment operator: ", stderr);
		fputs(operator, stderr);
		fputc('\n', stderr);
		exit(EXIT_FAILURE);
	}
	return operation;
}


void expression()
{
	bitwise_expr();
	if(match("=", global_token->s))
	{
		char* store = "";
		if(match("]", global_token->prev->s))
		{
			store = store_value(current_target->type->size);
		}
		else
		{
			store = store_value(current_target->size);
		}

		common_recursion(expression);
		emit_out(store);
		current_target = integer;
	}
	else if(is_compound_assignment(global_token->s))
	{
		maybe_bootstrap_error("compound operator");
		char* push = "";
		char* load = "";
		char* operation = "";
		char* pop = "";
		char* store = "";
		struct type* last_type = current_target;

		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) push = "PUSHR R1 R15\n";
		else if(X86 == Architecture) push = "push_ebx\n";
		else if(AMD64 == Architecture) push = "push_rbx\n";
		else if(ARMV7L == Architecture) push = "{R1} PUSH_ALWAYS\n";
		else if(AARCH64 == Architecture) push = "PUSH_X1\n";
		else if(RISCV32 == Architecture) push = "rs1_sp rs2_a1 @-4 sw\n";
		else if(RISCV64 == Architecture) push = "rs1_sp rs2_a1 @-8 sd\n";

		if(!match("]", global_token->prev->s) || !match("char*", current_target->name))
		{
			if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) load = "LOAD R1 R1 0\n";
			else if(X86 == Architecture) load = "mov_ebx,[ebx]\n";
			else if(AMD64 == Architecture) load = "mov_rbx,[rbx]\n";
			else if(ARMV7L == Architecture) load = "!0 R1 LOAD32 R1 MEMORY\n";
			else if(AARCH64 == Architecture) load = "DEREF_X1\n";
			else if(RISCV32 == Architecture) load = "rd_a1 rs1_a1 lw\n";
			else if(RISCV64 == Architecture) load = "rd_a1 rs1_a1 ld\n";
		}
		else
		{
			if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) load = "LOAD8 R1 R1 0\n";
			else if(X86 == Architecture) load = "movsx_ebx,BYTE_PTR_[ebx]\n";
			else if(AMD64 == Architecture) load = "movsx_rbx,BYTE_PTR_[rbx]\n";
			else if(ARMV7L == Architecture) load = "LOADU8 R1 LOAD R1 MEMORY\n";
			else if(AARCH64 == Architecture) load = "DEREF_X1_BYTE\n";
			else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) load = "rd_a1 rs1_a1 lbu\n";
		}

		char *operator = global_token->s;

		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) pop = "POPR R1 R15\n";
		else if(X86 == Architecture) pop = "pop_ebx\n";
		else if(AMD64 == Architecture) pop = "pop_rbx\n";
		else if(ARMV7L == Architecture) pop = "{R1} POP_ALWAYS\n";
		else if(AARCH64 == Architecture) pop = "POP_X1\n";
		else if(RISCV32 == Architecture) pop = "rd_a1 rs1_sp !-4 lw\n";
		else if(RISCV64 == Architecture) pop = "rd_a1 rs1_sp !-8 ld\n";

		if(match("]", global_token->prev->s))
		{
			store = store_value(current_target->type->size);
		}
		else
		{
			store = store_value(current_target->size);
		}

		common_recursion(expression);
		current_target = promote_type(current_target, last_type);
		emit_out(push);
		emit_out(load);
		operation = compound_operation(operator, current_target->is_signed);
		emit_out(operation);
		emit_out(pop);
		emit_out(store);
		current_target = integer;
	}
}


int iskeywordp(char* s)
{
	if(match("auto", s)) return TRUE;
	if(match("break", s)) return TRUE;
	if(match("case", s)) return TRUE;
	if(match("char", s)) return TRUE;
	if(match("const", s)) return TRUE;
	if(match("continue", s)) return TRUE;
	if(match("default", s)) return TRUE;
	if(match("do", s)) return TRUE;
	if(match("double", s)) return TRUE;
	if(match("else", s)) return TRUE;
	if(match("enum", s)) return TRUE;
	if(match("extern", s)) return TRUE;
	if(match("float", s)) return TRUE;
	if(match("for", s)) return TRUE;
	if(match("goto", s)) return TRUE;
	if(match("if", s)) return TRUE;
	if(match("int", s)) return TRUE;
	if(match("long", s)) return TRUE;
	if(match("register", s)) return TRUE;
	if(match("return", s)) return TRUE;
	if(match("short", s)) return TRUE;
	if(match("signed", s)) return TRUE;
	if(match("sizeof", s)) return TRUE;
	if(match("static", s)) return TRUE;
	if(match("struct", s)) return TRUE;
	if(match("switch", s)) return TRUE;
	if(match("typedef", s)) return TRUE;
	if(match("union", s)) return TRUE;
	if(match("unsigned", s)) return TRUE;
	if(match("void", s)) return TRUE;
	if(match("volatile", s)) return TRUE;
	if(match("while", s)) return TRUE;
	return FALSE;
}

/* Similar to integer division a / b but rounds up */
unsigned ceil_div(unsigned a, unsigned b)
{
	return (a + b - 1) / b;
}

/* Process local variable */
void collect_local()
{
	if(NULL != break_target_func)
	{
		fputs("Local variable initialized inside of loop in file: ", stderr);
		line_error();
		fputs("\nMove the variable outside of the loop to resolve\n", stderr);
		fputs("Otherwise the binary will segfault while running\n", stderr);
		exit(EXIT_FAILURE);
	}
	struct type* type_size = type_name();
	require(NULL != global_token, "Received EOF while collecting locals\n");
	require(!in_set(global_token->s[0], "[{(<=>)}]|&!^%;:'\""), "forbidden character in local variable name\n");
	require(!iskeywordp(global_token->s), "You are not allowed to use a keyword as a local variable name\n");
	require(NULL != type_size, "Must have non-null type\n");
	struct token_list* a = sym_declare(global_token->s, type_size, function->locals);
	if(match("main", function->s) && (NULL == function->locals))
	{
		if(KNIGHT_NATIVE == Architecture) a->depth = register_size;
		else if(KNIGHT_POSIX == Architecture) a->depth = 20;
		else if(X86 == Architecture) a->depth = -20;
		else if(AMD64 == Architecture) a->depth = -40;
		else if(ARMV7L == Architecture) a->depth = 16;
		else if(AARCH64 == Architecture) a->depth = 32; /* argc, argv, envp and the local (8 bytes each) */
		else if(RISCV32 == Architecture) a->depth = -16;
		else if(RISCV64 == Architecture) a->depth = -32;
	}
	else if((NULL == function->arguments) && (NULL == function->locals))
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) a->depth = register_size;
		else if(X86 == Architecture) a->depth = -8;
		else if(AMD64 == Architecture) a->depth = -16;
		else if(ARMV7L == Architecture) a->depth = 8;
		else if(AARCH64 == Architecture) a->depth = register_size;
		else if(RISCV32 == Architecture) a->depth = -4;
		else if(RISCV64 == Architecture) a->depth = -8;
	}
	else if(NULL == function->locals)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) a->depth = function->arguments->depth + 8;
		else if(X86 == Architecture) a->depth = function->arguments->depth - 8;
		else if(AMD64 == Architecture) a->depth = function->arguments->depth - 16;
		else if(ARMV7L == Architecture) a->depth = function->arguments->depth + 8;
		else if(AARCH64 == Architecture) a->depth = function->arguments->depth + register_size;
		else if(RISCV32 == Architecture) a->depth = function->arguments->depth - 4;
		else if(RISCV64 == Architecture) a->depth = function->arguments->depth - 8;
	}
	else
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) a->depth = function->locals->depth + register_size;
		else if(X86 == Architecture) a->depth = function->locals->depth - register_size;
		else if(AMD64 == Architecture) a->depth = function->locals->depth - register_size;
		else if(ARMV7L == Architecture) a->depth = function->locals->depth + register_size;
		else if(AARCH64 == Architecture) a->depth = function->locals->depth + register_size;
		else if(RISCV32 == Architecture) a->depth = function->locals->depth - register_size;
		else if(RISCV64 == Architecture) a->depth = function->locals->depth - register_size;
	}

	/* Adjust the depth of local structs. When stack grows downwards, we want them to 
	   start at the bottom of allocated space. */
	unsigned struct_depth_adjustment = (ceil_div(a->type->size, register_size) - 1) * register_size;
	if(KNIGHT_POSIX == Architecture) a->depth = a->depth + struct_depth_adjustment;
	else if(KNIGHT_NATIVE == Architecture) a->depth = a->depth + struct_depth_adjustment;
	else if(X86 == Architecture) a->depth = a->depth - struct_depth_adjustment;
	else if(AMD64 == Architecture) a->depth = a->depth - struct_depth_adjustment;
	else if(ARMV7L == Architecture) a->depth = a->depth + struct_depth_adjustment;
	else if(AARCH64 == Architecture) a->depth = a->depth + struct_depth_adjustment;
	else if(RISCV32 == Architecture) a->depth = a->depth - struct_depth_adjustment;
	else if(RISCV64 == Architecture) a->depth = a->depth - struct_depth_adjustment;

	function->locals = a;

	emit_out("# Defining local ");
	emit_out(global_token->s);
	emit_out("\n");

	global_token = global_token->next;
	require(NULL != global_token, "incomplete local missing name\n");

	if(match("=", global_token->s))
	{
		global_token = global_token->next;
		require(NULL != global_token, "incomplete local assignment\n");
		expression();
	}

	require_match("ERROR in collect_local\nMissing ;\n", ";");

	unsigned i = (a->type->size + register_size - 1) / register_size;
	while(i != 0)
	{
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("PUSHR R0 R15\t#");
		else if(X86 == Architecture) emit_out("push_eax\t#");
		else if(AMD64 == Architecture) emit_out("push_rax\t#");
		else if(ARMV7L == Architecture) emit_out("{R0} PUSH_ALWAYS\t#");
		else if(AARCH64 == Architecture) emit_out("PUSH_X0\t#");
		else if(RISCV32 == Architecture) emit_out("rd_sp rs1_sp !-4 addi\nrs1_sp rs2_a0 sw\t#");
		else if(RISCV64 == Architecture) emit_out("rd_sp rs1_sp !-8 addi\nrs1_sp rs2_a0 sd\t#");
		emit_out(a->s);
		emit_out("\n");
		i = i - 1;
	}
}

void statement();

/* Evaluate if statements */
void process_if()
{
	char* number_string = int2str(current_count, 10, TRUE);
	current_count = current_count + 1;

	emit_out("# IF_");
	uniqueID_out(function->s, number_string);

	global_token = global_token->next;
	require_match("ERROR in process_if\nMISSING (\n", "(");
	expression();

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP.Z R0 @ELSE_");
	else if(X86 == Architecture) emit_out("test_eax,eax\nje %ELSE_");
	else if(AMD64 == Architecture) emit_out("test_rax,rax\nje %ELSE_");
	else if(ARMV7L == Architecture) emit_out("!0 CMPI8 R0 IMM_ALWAYS\n^~ELSE_");
	else if(AARCH64 == Architecture) emit_out("CBNZ_X0_PAST_BR\nLOAD_W16_AHEAD\nSKIP_32_DATA\n&ELSE_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rs1_a0 @8 bnez\n$ELSE_");

	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_EQUAL\n");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");

	require_match("ERROR in process_if\nMISSING )\n", ")");
	statement();
	require(NULL != global_token, "Reached EOF inside of function\n");

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @_END_IF_");
	else if(X86 == Architecture) emit_out("jmp %_END_IF_");
	else if(AMD64 == Architecture) emit_out("jmp %_END_IF_");
	else if(ARMV7L == Architecture) emit_out("^~_END_IF_");
	else if(AARCH64 == Architecture) emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&_END_IF_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$_END_IF_");

	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS\n");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");

	emit_out(":ELSE_");
	uniqueID_out(function->s, number_string);

	if(match("else", global_token->s))
	{
		global_token = global_token->next;
		require(NULL != global_token, "Received EOF where an else statement expected\n");
		statement();
		require(NULL != global_token, "Reached EOF inside of function\n");
	}
	emit_out(":_END_IF_");
	uniqueID_out(function->s, number_string);
}

void process_case()
{
process_case_iter:
	if(match("case", global_token->s)) return;
	if(match(":default", global_token->s)) return;

	if(match("break", global_token->s))
	{
		statement();
	}
	else
	{
		statement();
		goto process_case_iter;
	}
}

void process_switch()
{
	maybe_bootstrap_error("switch/case statements");
	struct token_list* nested_locals = break_frame;
	char* nested_break_head = break_target_head;
	char* nested_break_func = break_target_func;
	char* nested_break_num = break_target_num;
	char* nested_continue_head = continue_target_head;

	char* number_string = int2str(current_count, 10, TRUE);
	current_count = current_count + 1;

	break_target_head = "_SWITCH_END_";
	continue_target_head = NULL; /* don't allow continue in switch statements */
	break_target_num = number_string;
	break_frame = function->locals;
	break_target_func = function->s;

	emit_out("# switch_");
	uniqueID_out(function->s, number_string);

	/* get what we are casing on */
	global_token = global_token->next;
	require_match("ERROR in process_switch\nMISSING (\n", "(");
	expression();
	require_match("ERROR in process_switch\nMISSING )\n", ")");

	/* Put the value in R1 as it is currently in R0 */
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("MOVE R1 R0\n");
	else if(X86 == Architecture) emit_out("mov_ebx,eax\n");
	else if(AMD64 == Architecture) emit_out("push_rax\npop_rbx\n");
	else if(ARMV7L == Architecture) emit_out("'0' R1 R0 NO_SHIFT MOVE_ALWAYS\n");
	else if(AARCH64 == Architecture) emit_out("SET_X1_FROM_X0\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a1 rs1_a0 mv\n");

	/* Jump to the switch table */
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @_SWITCH_TABLE_");
	else if(X86 == Architecture) emit_out("jmp %_SWITCH_TABLE_");
	else if(AMD64 == Architecture) emit_out("jmp %_SWITCH_TABLE_");
	else if(ARMV7L == Architecture) emit_out("^~_SWITCH_TABLE_");
	else if(AARCH64 == Architecture) emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&_SWITCH_TABLE_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$_SWITCH_TABLE_");

	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS\n");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");

	/* must be switch (exp) {$STATEMENTS}; form */
	require_match("ERROR in process_switch\nMISSING {\n", "{");
	struct case_list* backtrack = NULL;
process_switch_iter:
	if(match("case", global_token->s))
	{
		global_token = global_token->next;
		if(':' == global_token->s[0])
		{
			struct case_list* c = calloc(1, sizeof(struct case_list));
			c->next = backtrack;
			c->value = global_token->s + 1;
			backtrack = c;
			emit_out(":_SWITCH_CASE_");
			emit_out(c->value);
			emit_out("_");
			uniqueID_out(function->s, number_string);
			global_token = global_token->next;
			process_case();
		}
		else line_error();
		goto process_switch_iter;
	}
	else if(match(":default", global_token->s))
	{ /* because of how M2-Planet treats labels */
		global_token = global_token->next;
		emit_out(":_SWITCH_DEFAULT_");
		uniqueID_out(function->s, number_string);

		/* collect statements until } */
		while(!match("}", global_token->s))
		{
			statement();
		}

		/* jump over the switch table */
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @_SWITCH_END_");
		else if(X86 == Architecture) emit_out("jmp %_SWITCH_END_");
		else if(AMD64 == Architecture) emit_out("jmp %_SWITCH_END_");
		else if(ARMV7L == Architecture) emit_out("^~_SWITCH_END_");
		else if(AARCH64 == Architecture) emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&_SWITCH_END_");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$_SWITCH_END_");

		uniqueID_out(function->s, number_string);
		if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS\n");
		else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");
	}

	/* Switch statements must end with } */
	require_match("ERROR in process_switch\nMISSING }\n", "}");

	/* create the table */
	emit_out(":_SWITCH_TABLE_");
	uniqueID_out(function->s, number_string);

	struct case_list* hold;
	while(NULL != backtrack)
	{
		/* put case value in R0 as the switch (value) is in R1 */
		primary_expr_number(backtrack->value);
		hold = backtrack->next;

		/* compare R0 and R1 and jump to case if equal */
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("CMPU R0 R0 R1\nJUMP.E R0 @_SWITCH_CASE_");
		else if(X86 == Architecture) emit_out("cmp\nje %_SWITCH_CASE_");
		else if(AMD64 == Architecture) emit_out("cmp_rbx,rax\nje %_SWITCH_CASE_");
		else if(ARMV7L == Architecture) emit_out("'0' R0 CMP R1 AUX_ALWAYS\n^~_SWITCH_CASE_");
		else if(AARCH64 == Architecture) emit_out("CMP_X1_X0\nSKIP_32_DATA\n&_SWITCH_CASE_");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rd_a0 rs1_a0 rs2_a1 sub\nrs1_a0 @8 bnez\n$_SWITCH_CASE_");

		emit_out(backtrack->value);
		emit_out("_");
		uniqueID_out(function->s, number_string);
		if(ARMV7L == Architecture) emit_out(" JUMP_EQUAL\n");
		else if(AARCH64 == Architecture) emit_out("\nSKIP_INST_NE\nBR_X16\n");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");

		free(backtrack);
		backtrack = hold;
	}

	/* Default to :default */
	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @_SWITCH_DEFAULT_");
	else if(X86 == Architecture) emit_out("jmp %_SWITCH_DEFAULT_");
	else if(AMD64 == Architecture) emit_out("jmp %_SWITCH_DEFAULT_");
	else if(ARMV7L == Architecture) emit_out("^~_SWITCH_DEFAULT_");
	else if(AARCH64 == Architecture) emit_out("SKIP_32_DATA\n&_SWITCH_DEFAULT_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$_SWITCH_DEFAULT_");

	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS\n");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");

	/* put the exit of the switch */
	emit_out(":_SWITCH_END_");
	uniqueID_out(function->s, number_string);

	break_target_head = nested_break_head;
	break_target_func = nested_break_func;
	break_target_num = nested_break_num;
	continue_target_head = nested_continue_head;
	break_frame = nested_locals;
}

void process_for()
{
	struct token_list* nested_locals = break_frame;
	char* nested_break_head = break_target_head;
	char* nested_break_func = break_target_func;
	char* nested_break_num = break_target_num;
	char* nested_continue_head = continue_target_head;

	char* number_string = int2str(current_count, 10, TRUE);
	current_count = current_count + 1;

	break_target_head = "FOR_END_";
	continue_target_head = "FOR_ITER_";
	break_target_num = number_string;
	break_frame = function->locals;
	break_target_func = function->s;

	emit_out("# FOR_initialization_");
	uniqueID_out(function->s, number_string);

	global_token = global_token->next;

	require_match("ERROR in process_for\nMISSING (\n", "(");
	if(!match(";",global_token->s))
	{
		expression();
	}

	emit_out(":FOR_");
	uniqueID_out(function->s, number_string);

	require_match("ERROR in process_for\nMISSING ;1\n", ";");
	expression();

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP.Z R0 @FOR_END_");
	else if(X86 == Architecture) emit_out("test_eax,eax\nje %FOR_END_");
	else if(AMD64 == Architecture) emit_out("test_rax,rax\nje %FOR_END_");
	else if(ARMV7L == Architecture) emit_out("!0 CMPI8 R0 IMM_ALWAYS\n^~FOR_END_");
	else if(AARCH64 == Architecture) emit_out("CBNZ_X0_PAST_BR\nLOAD_W16_AHEAD\nSKIP_32_DATA\n&FOR_END_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rs1_a0 @8 bnez\n$FOR_END_");
	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_EQUAL\n");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @FOR_THEN_");
	else if(X86 == Architecture) emit_out("jmp %FOR_THEN_");
	else if(AMD64 == Architecture) emit_out("jmp %FOR_THEN_");
	else if(ARMV7L == Architecture) emit_out("^~FOR_THEN_");
	else if(AARCH64 == Architecture) emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&FOR_THEN_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$FOR_THEN_");
	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS\n");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");

	emit_out(":FOR_ITER_");
	uniqueID_out(function->s, number_string);

	require_match("ERROR in process_for\nMISSING ;2\n", ";");
	expression();

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @FOR_");
	else if(X86 == Architecture) emit_out("jmp %FOR_");
	else if(AMD64 == Architecture) emit_out("jmp %FOR_");
	else if(ARMV7L == Architecture) emit_out("^~FOR_");
	else if(AARCH64 == Architecture) emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&FOR_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$FOR_");
	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS\n");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");

	emit_out(":FOR_THEN_");
	uniqueID_out(function->s, number_string);

	require_match("ERROR in process_for\nMISSING )\n", ")");
	statement();
	require(NULL != global_token, "Reached EOF inside of function\n");

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @FOR_ITER_");
	else if(X86 == Architecture) emit_out("jmp %FOR_ITER_");
	else if(AMD64 == Architecture) emit_out("jmp %FOR_ITER_");
	else if(ARMV7L == Architecture) emit_out("^~FOR_ITER_");
	else if(AARCH64 == Architecture) emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&FOR_ITER_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$FOR_ITER_");
	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS\n");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");

	emit_out(":FOR_END_");
	uniqueID_out(function->s, number_string);

	break_target_head = nested_break_head;
	break_target_func = nested_break_func;
	break_target_num = nested_break_num;
	continue_target_head = nested_continue_head;
	break_frame = nested_locals;
}

/* Process Assembly statements */
void process_asm()
{
	global_token = global_token->next;
	require_match("ERROR in process_asm\nMISSING (\n", "(");
	while('"' == global_token->s[0])
	{
		emit_out((global_token->s + 1));
		emit_out("\n");
		global_token = global_token->next;
		require(NULL != global_token, "Received EOF inside asm statement\n");
	}
	require_match("ERROR in process_asm\nMISSING )\n", ")");
	require_match("ERROR in process_asm\nMISSING ;\n", ";");
}

/* Process do while loops */
void process_do()
{
	struct token_list* nested_locals = break_frame;
	char* nested_break_head = break_target_head;
	char* nested_break_func = break_target_func;
	char* nested_break_num = break_target_num;
	char* nested_continue_head = continue_target_head;

	char* number_string = int2str(current_count, 10, TRUE);
	current_count = current_count + 1;

	break_target_head = "DO_END_";
	continue_target_head = "DO_TEST_";
	break_target_num = number_string;
	break_frame = function->locals;
	break_target_func = function->s;

	emit_out(":DO_");
	uniqueID_out(function->s, number_string);

	global_token = global_token->next;
	require(NULL != global_token, "Received EOF where do statement is expected\n");
	statement();
	require(NULL != global_token, "Reached EOF inside of function\n");

	emit_out(":DO_TEST_");
	uniqueID_out(function->s, number_string);

	require_match("ERROR in process_do\nMISSING while\n", "while");
	require_match("ERROR in process_do\nMISSING (\n", "(");
	expression();
	require_match("ERROR in process_do\nMISSING )\n", ")");
	require_match("ERROR in process_do\nMISSING ;\n", ";");

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP.NZ R0 @DO_");
	else if(X86 == Architecture) emit_out("test_eax,eax\njne %DO_");
	else if(AMD64 == Architecture) emit_out("test_rax,rax\njne %DO_");
	else if(ARMV7L == Architecture) emit_out("!0 CMPI8 R0 IMM_ALWAYS\n^~DO_");
	else if(AARCH64 == Architecture) emit_out("CBZ_X0_PAST_BR\nLOAD_W16_AHEAD\nSKIP_32_DATA\n&DO_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rs1_a0 @DO_END_");
	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_NE\n");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture))
	{
		emit_out("beqz\n$DO_");
		uniqueID_out(function->s, number_string);
		emit_out("jal\n");
	}

	emit_out(":DO_END_");
	uniqueID_out(function->s, number_string);

	break_frame = nested_locals;
	break_target_head = nested_break_head;
	break_target_func = nested_break_func;
	break_target_num = nested_break_num;
	continue_target_head = nested_continue_head;
}


/* Process while loops */
void process_while()
{
	struct token_list* nested_locals = break_frame;
	char* nested_break_head = break_target_head;
	char* nested_break_func = break_target_func;
	char* nested_break_num = break_target_num;
	char* nested_continue_head = continue_target_head;

	char* number_string = int2str(current_count, 10, TRUE);
	current_count = current_count + 1;

	break_target_head = "END_WHILE_";
	continue_target_head = "WHILE_";
	break_target_num = number_string;
	break_frame = function->locals;
	break_target_func = function->s;

	emit_out(":WHILE_");
	uniqueID_out(function->s, number_string);

	global_token = global_token->next;
	require_match("ERROR in process_while\nMISSING (\n", "(");
	expression();

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP.Z R0 @END_WHILE_");
	else if(X86 == Architecture) emit_out("test_eax,eax\nje %END_WHILE_");
	else if(AMD64 == Architecture) emit_out("test_rax,rax\nje %END_WHILE_");
	else if(ARMV7L == Architecture) emit_out("!0 CMPI8 R0 IMM_ALWAYS\n^~END_WHILE_");
	else if(AARCH64 == Architecture) emit_out("CBNZ_X0_PAST_BR\nLOAD_W16_AHEAD\nSKIP_32_DATA\n&END_WHILE_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("rs1_a0 @8 bnez\n$END_WHILE_");
	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_EQUAL\t");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");
	emit_out("# THEN_while_");
	uniqueID_out(function->s, number_string);

	require_match("ERROR in process_while\nMISSING )\n", ")");
	statement();
	require(NULL != global_token, "Reached EOF inside of function\n");

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @WHILE_");
	else if(X86 == Architecture) emit_out("jmp %WHILE_");
	else if(AMD64 == Architecture) emit_out("jmp %WHILE_");
	else if(ARMV7L == Architecture) emit_out("^~WHILE_");
	else if(AARCH64 == Architecture) emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&WHILE_");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$WHILE_");
	uniqueID_out(function->s, number_string);
	if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS\n");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("jal\n");
	emit_out(":END_WHILE_");
	uniqueID_out(function->s, number_string);

	break_target_head = nested_break_head;
	break_target_func = nested_break_func;
	break_target_num = nested_break_num;
	continue_target_head = nested_continue_head;
	break_frame = nested_locals;
}

/* Ensure that functions return */
void return_result()
{
	global_token = global_token->next;
	require(NULL != global_token, "Incomplete return statement received\n");
	if(global_token->s[0] != ';') expression();

	require_match("ERROR in return_result\nMISSING ;\n", ";");

	struct token_list* i;
	unsigned size_local_var;
	for(i = function->locals; NULL != i; i = i->next)
	{
		size_local_var = ceil_div(i->type->size, register_size);
		while(size_local_var != 0)
		{
			if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("POPR R1 R15\t# _return_result_locals\n");
			else if(X86 == Architecture) emit_out("pop_ebx\t# _return_result_locals\n");
			else if(AMD64 == Architecture) emit_out("pop_rbx\t# _return_result_locals\n");
			else if(ARMV7L == Architecture) emit_out("{R1} POP_ALWAYS\t# _return_result_locals\n");
			else if(AARCH64 == Architecture) emit_out("POP_X1\t# _return_result_locals\n");
			else if(RISCV32 == Architecture) emit_out("rd_a1 rs1_sp lw	# _return_result_locals\nrd_sp rs1_sp !4 addi\n");
			else if(RISCV64 == Architecture) emit_out("rd_a1 rs1_sp ld	# _return_result_locals\nrd_sp rs1_sp !8 addi\n");
			size_local_var = size_local_var - 1;
		}
	}

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("RET R15\n");
	else if(X86 == Architecture) emit_out("ret\n");
	else if(AMD64 == Architecture) emit_out("ret\n");
	else if(ARMV7L == Architecture) emit_out("'1' LR RETURN\n");
	else if(AARCH64 == Architecture) emit_out("RETURN\n");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("ret\n");
}

void process_break()
{
	if(NULL == break_target_head)
	{
		line_error();
		fputs("Not inside of a loop or case statement\n", stderr);
		exit(EXIT_FAILURE);
	}
	struct token_list* i = function->locals;
	while(i != break_frame)
	{
		if(NULL == i) break;
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("POPR R1 R15\t# break_cleanup_locals\n");
		else if(X86 == Architecture) emit_out("pop_ebx\t# break_cleanup_locals\n");
		else if(AMD64 == Architecture) emit_out("pop_rbx\t# break_cleanup_locals\n");
		else if(ARMV7L == Architecture) emit_out("{R1} POP_ALWAYS\t# break_cleanup_locals\n");
		else if(AARCH64 == Architecture) emit_out("POP_X1\t# break_cleanup_locals\n");
		else if(RISCV32 == Architecture) emit_out("rd_a1 rs1_sp lw\t# break_cleanup_locals\nrd_sp rs1_sp !4 addi\n");
		else if(RISCV64 == Architecture) emit_out("rd_a1 rs1_sp ld\t# break_cleanup_locals\nrd_sp rs1_sp !8 addi\n");
		i = i->next;
	}
	global_token = global_token->next;

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @");
	else if(X86 == Architecture) emit_out("jmp %");
	else if(AMD64 == Architecture) emit_out("jmp %");
	else if(ARMV7L == Architecture) emit_out("^~");
	else if(AARCH64 == Architecture) emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$");

	emit_out(break_target_head);
	emit_out(break_target_func);
	emit_out("_");
	emit_out(break_target_num);
	if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out(" jal");
	emit_out("\n");
	require_match("ERROR in break statement\nMissing ;\n", ";");
}

void process_continue()
{
	if(NULL == continue_target_head)
	{
		line_error();
		fputs("Not inside of a loop\n", stderr);
		exit(EXIT_FAILURE);
	}
	global_token = global_token->next;

	if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @");
	else if(X86 == Architecture) emit_out("jmp %");
	else if(AMD64 == Architecture) emit_out("jmp %");
	else if(ARMV7L == Architecture) emit_out("^~");
	else if(AARCH64 == Architecture) emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$");

	emit_out(continue_target_head);
	emit_out(break_target_func);
	emit_out("_");
	emit_out(break_target_num);
	if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS");
	else if(AARCH64 == Architecture) emit_out("\nBR_X16");
	else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out(" jal");
	emit_out("\n");
	require_match("ERROR in continue statement\nMissing ;\n", ";");
}

void recursive_statement()
{
	global_token = global_token->next;
	require(NULL != global_token, "Received EOF in recursive statement\n");
	struct token_list* frame = function->locals;

	while(!match("}", global_token->s))
	{
		statement();
		require(NULL != global_token, "Received EOF in recursive statement prior to }\n");
	}
	global_token = global_token->next;

	/* Clean up any locals added */

	if(((X86 == Architecture) && !match("ret\n", output_list->s)) ||
	   ((AMD64 == Architecture) && !match("ret\n", output_list->s)) ||
	   (((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) && !match("RET R15\n", output_list->s)) ||
	   ((ARMV7L == Architecture) && !match("'1' LR RETURN\n", output_list->s)) ||
	   ((AARCH64 == Architecture) && !match("RETURN\n", output_list->s)) ||
	   (((RISCV32 == Architecture) || (RISCV64 == Architecture)) && !match("ret\n", output_list->s)))
	{
		struct token_list* i;
		for(i = function->locals; frame != i; i = i->next)
		{
			if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("POPR R1 R15\t# _recursive_statement_locals\n");
			else if(X86 == Architecture) emit_out( "pop_ebx\t# _recursive_statement_locals\n");
			else if(AMD64 == Architecture) emit_out("pop_rbx\t# _recursive_statement_locals\n");
			else if(ARMV7L == Architecture) emit_out("{R1} POP_ALWAYS\t# _recursive_statement_locals\n");
			else if(AARCH64 == Architecture) emit_out("POP_X1\t# _recursive_statement_locals\n");
			else if(RISCV32 == Architecture) emit_out("rd_a1 rs1_sp lw\t# _recursive_statement_locals\nrd_sp rs1_sp !4 addi\n");
			else if(RISCV64 == Architecture) emit_out("rd_a1 rs1_sp ld\t# _recursive_statement_locals\nrd_sp rs1_sp !8 addi\n");
		}
	}
	function->locals = frame;
}

/*
 * statement:
 *     { statement-list-opt }
 *     type-name identifier ;
 *     type-name identifier = expression;
 *     if ( expression ) statement
 *     if ( expression ) statement else statement
 *     do statement while ( expression ) ;
 *     while ( expression ) statement
 *     for ( expression ; expression ; expression ) statement
 *     asm ( "assembly" ... "assembly" ) ;
 *     goto label ;
 *     label:
 *     return ;
 *     break ;
 *     expr ;
 */

struct type* lookup_type(char* s, struct type* start);
void statement()
{
	require(NULL != global_token, "expected a C statement but received EOF\n");
	/* Always an integer until told otherwise */
	current_target = integer;

	if(global_token->s[0] == '{')
	{
		recursive_statement();
	}
	else if(':' == global_token->s[0])
	{
		emit_out(global_token->s);
		emit_out("\t#C goto label\n");
		global_token = global_token->next;
	}
	else if((NULL != lookup_type(global_token->s, prim_types)) ||
	          match("struct", global_token->s))
	{
		collect_local();
	}
	else if(match("if", global_token->s))
	{
		process_if();
	}
	else if(match("switch", global_token->s))
	{
		process_switch();
	}
	else if(match("do", global_token->s))
	{
		process_do();
	}
	else if(match("while", global_token->s))
	{
		process_while();
	}
	else if(match("for", global_token->s))
	{
		process_for();
	}
	else if(match("asm", global_token->s))
	{
		process_asm();
	}
	else if(match("goto", global_token->s))
	{
		global_token = global_token->next;
		require(NULL != global_token, "naked goto is not supported\n");
		if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) emit_out("JUMP @");
		else if(X86 == Architecture) emit_out("jmp %");
		else if(AMD64 == Architecture) emit_out("jmp %");
		else if(ARMV7L == Architecture) emit_out("^~");
		else if(AARCH64 == Architecture) emit_out("LOAD_W16_AHEAD\nSKIP_32_DATA\n&");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out("$");
		emit_out(global_token->s);
		if(ARMV7L == Architecture) emit_out(" JUMP_ALWAYS");
		else if(AARCH64 == Architecture) emit_out("\nBR_X16");
		else if((RISCV32 == Architecture) || (RISCV64 == Architecture)) emit_out(" jal");
		emit_out("\n");
		global_token = global_token->next;
		require_match("ERROR in statement\nMissing ;\n", ";");
	}
	else if(match("return", global_token->s))
	{
		return_result();
	}
	else if(match("break", global_token->s))
	{
		process_break();
	}
	else if(match("continue", global_token->s))
	{
		process_continue();
	}
	else
	{
		expression();
		require_match("ERROR in statement\nMISSING ;\n", ";");
	}
}

/* Collect function arguments */
void collect_arguments()
{
	global_token = global_token->next;
	require(NULL != global_token, "Received EOF when attempting to collect arguments\n");
	struct type* type_size;
	struct token_list* a;

	while(!match(")", global_token->s))
	{
		type_size = type_name();
		require(NULL != global_token, "Received EOF when attempting to collect arguments\n");
		require(NULL != type_size, "Must have non-null type\n");
		if(global_token->s[0] == ')')
		{
			/* foo(int,char,void) doesn't need anything done */
			continue;
		}
		else if(global_token->s[0] != ',')
		{
			/* deal with foo(int a, char b) */
			require(!in_set(global_token->s[0], "[{(<=>)}]|&!^%;:'\""), "forbidden character in argument variable name\n");
			require(!iskeywordp(global_token->s), "You are not allowed to use a keyword as a argument variable name\n");
			a = sym_declare(global_token->s, type_size, function->arguments);
			if(NULL == function->arguments)
			{
				if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) a->depth = 0;
				else if(X86 == Architecture) a->depth = -4;
				else if(AMD64 == Architecture) a->depth = -8;
				else if(ARMV7L == Architecture) a->depth = 4;
				else if(AARCH64 == Architecture) a->depth = register_size;
				else if(RISCV32 == Architecture) a->depth = -4;
				else if(RISCV64 == Architecture) a->depth = -8;
			}
			else
			{
				if((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) a->depth = function->arguments->depth + register_size;
				else if(X86 == Architecture) a->depth = function->arguments->depth - register_size;
				else if(AMD64 == Architecture) a->depth = function->arguments->depth - register_size;
				else if(ARMV7L == Architecture) a->depth = function->arguments->depth + register_size;
				else if(AARCH64 == Architecture) a->depth = function->arguments->depth + register_size;
				else if(RISCV32 == Architecture) a->depth = function->arguments->depth - register_size;
				else if(RISCV64 == Architecture) a->depth = function->arguments->depth - register_size;
			}

			global_token = global_token->next;
			require(NULL != global_token, "Incomplete argument list\n");
			function->arguments = a;
		}

		/* ignore trailing comma (needed for foo(bar(), 1); expressions*/
		if(global_token->s[0] == ',')
		{
			global_token = global_token->next;
			require(NULL != global_token, "naked comma in collect arguments\n");
		}

		require(NULL != global_token, "Argument list never completed\n");
	}
	global_token = global_token->next;
}

void declare_function()
{
	current_count = 0;
	function = sym_declare(global_token->prev->s, NULL, global_function_list);

	/* allow previously defined functions to be looked up */
	global_function_list = function;
	if((KNIGHT_NATIVE == Architecture) && match("main", function->s))
	{
		require_match("Impossible error ( vanished\n", "(");
		require_match("Reality ERROR (USING KNIGHT-NATIVE)\nHardware does not support arguments\nthus neither can main on this architecture\ntry tape_01 and tape_02 instead\n", ")");
	}
	else collect_arguments();

	require(NULL != global_token, "Function definitions either need to be prototypes or full\n");
	/* If just a prototype don't waste time */
	if(global_token->s[0] == ';') global_token = global_token->next;
	else
	{
		emit_out("# Defining function ");
		emit_out(function->s);
		emit_out("\n");
		emit_out(":FUNCTION_");
		emit_out(function->s);
		emit_out("\n");
		statement();

		/* Prevent duplicate RETURNS */
		if(((KNIGHT_POSIX == Architecture) || (KNIGHT_NATIVE == Architecture)) && !match("RET R15\n", output_list->s)) emit_out("RET R15\n");
		else if((X86 == Architecture) && !match("ret\n", output_list->s)) emit_out("ret\n");
		else if((AMD64 == Architecture) && !match("ret\n", output_list->s)) emit_out("ret\n");
		else if((ARMV7L == Architecture) && !match("'1' LR RETURN\n", output_list->s)) emit_out("'1' LR RETURN\n");
		else if((AARCH64 == Architecture) && !match("RETURN\n", output_list->s)) emit_out("RETURN\n");
		else if((RISCV32 == Architecture) && !match("ret\n", output_list->s)) emit_out("ret\n");
		else if((RISCV64 == Architecture) && !match("ret\n", output_list->s)) emit_out("ret\n");
	}
}

void global_constant()
{
	global_token = global_token->next;
	require(NULL != global_token, "CONSTANT lacks a name\n");
	global_constant_list = sym_declare(global_token->s, NULL, global_constant_list);

	require(NULL != global_token->next, "CONSTANT lacks a value\n");
	if(match("sizeof", global_token->next->s))
	{
		global_token = global_token->next->next;
		require_match("ERROR in CONSTANT with sizeof\nMissing (\n", "(");
		struct type* a = type_name();
		require_match("ERROR in CONSTANT with sizeof\nMissing )\n", ")");
		global_token->prev->s = int2str(a->size, 10, TRUE);
		global_constant_list->arguments = global_token->prev;
	}
	else
	{
		global_constant_list->arguments = global_token->next;
		global_token = global_token->next->next;
	}
}

struct type* global_typedef()
{
	struct type* type_size;
	/* typedef $TYPE $NAME; */
	global_token = global_token->next;
	type_size = type_name();
	require(NULL != global_token, "Received EOF while reading typedef\n");
	type_size = mirror_type(type_size, global_token->s);
	add_primitive(type_size);
	global_token = global_token->next;
	require_match("ERROR in typedef statement\nMissing ;\n", ";");
	return type_size;
}

void global_static_array(struct type* type_size, struct token_list* name)
{
	int size;
	maybe_bootstrap_error("global array definitions");
	globals_list = emit(":GLOBAL_", globals_list);
	globals_list = emit(name->s, globals_list);
	globals_list = emit("\n&GLOBAL_STORAGE_", globals_list);
	globals_list = emit(name->s, globals_list);
	if (AARCH64 == Architecture || AMD64 == Architecture || RISCV64 == Architecture)
	{
		globals_list = emit(" %0", globals_list);
	}
	globals_list = emit("\n:GLOBAL_STORAGE_", globals_list);
	globals_list = emit(name->s, globals_list);

	require(NULL != global_token->next, "Unterminated global\n");
	global_token = global_token->next;

	/* Make sure not negative */
	if(match("-", global_token->s))
	{
		line_error();
		fputs("Negative values are not supported for allocated arrays\n", stderr);
		exit(EXIT_FAILURE);
	}

	/* length */
	size = strtoint(global_token->s) * type_size->size;

	/* Stop bad states */
	if((size < 0) || (size > 0x100000))
	{
		line_error();
		fputs("M2-Planet is very inefficient so you probably don't want to allocate over 1MB into your binary for NULLs\n", stderr);
		exit(EXIT_FAILURE);
	}

	/* Ensure properly closed */
	global_token = global_token->next;
	require_match("missing close bracket\n", "]");
	require_match("missing ;\n", ";");

	globals_list = emit("\n'", globals_list);
	while (0 != size)
	{
		globals_list = emit(" 00", globals_list);
		size = size - 1;
	}
	globals_list = emit("'\n", globals_list);
}

void global_assignment()
{
	/* Store the global's value*/
	globals_list = emit(":GLOBAL_", globals_list);
	globals_list = emit(global_token->prev->s, globals_list);
	globals_list = emit("\n", globals_list);
	global_token = global_token->next;
	require(NULL != global_token, "Global locals value in assignment\n");
	unsigned padding_zeroes;
	if(in_set(global_token->s[0], "0123456789"))
	{ /* Assume Int */
		globals_list = emit("%", globals_list);
		globals_list = emit(global_token->s, globals_list);

		/* broken for big endian architectures */
		padding_zeroes = (register_size / 4) - 1;
		while(padding_zeroes > 0)
		{
			/* Assume positive Int */
			globals_list = emit(" %0", globals_list);
			padding_zeroes = padding_zeroes - 1;
		}
		globals_list = emit("\n", globals_list);
	}
	else if(('"' == global_token->s[0]))
	{ /* Assume a string*/
		globals_list = emit("&GLOBAL_", globals_list);
		globals_list = emit(global_token->prev->prev->s, globals_list);
		globals_list = emit("_contents\n", globals_list);

		globals_list = emit(":GLOBAL_", globals_list);
		globals_list = emit(global_token->prev->prev->s, globals_list);
		globals_list = emit("_contents\n", globals_list);
		globals_list = emit(parse_string(global_token->s), globals_list);
	}
	else
	{
		line_error();
		fputs("Received ", stderr);
		fputs(global_token->s, stderr);
		fputs(" in program\n", stderr);
		exit(EXIT_FAILURE);
	}

	global_token = global_token->next;
	require_match("ERROR in Program\nMissing ;\n", ";");
}

/*
 * program:
 *     declaration
 *     declaration program
 *
 * declaration:
 *     CONSTANT identifer value
 *     typedef identifer type;
 *     type-name identifier ;
 *     type-name identifier = value ;
 *     type-name identifier [ value ];
 *     type-name identifier ( parameter-list ) ;
 *     type-name identifier ( parameter-list ) statement
 *
 * parameter-list:
 *     parameter-declaration
 *     parameter-list, parameter-declaration
 *
 * parameter-declaration:
 *     type-name identifier-opt
 */
void program()
{
	unsigned i;
	function = NULL;
	Address_of = FALSE;
	struct type* type_size;

new_type:
	/* Deal with garbage input */
	if (NULL == global_token) return;
	require('#' != global_token->s[0], "unhandled macro directive\n");
	require(!match("\n", global_token->s), "unexpected newline token\n");

	/* Handle cc_* CONSTANT statements */
	if(match("CONSTANT", global_token->s))
	{
		global_constant();
		goto new_type;
	}

	/* Handle c typedef statements */
	if(match("typedef", global_token->s))
	{
		type_size = global_typedef();
		goto new_type;
	}

	type_size = type_name();
	/* Deal with case of struct definitions */
	if(NULL == type_size) goto new_type;

	require(NULL != global_token->next, "Unterminated global\n");

	/* Add to global symbol table */
	global_symbol_list = sym_declare(global_token->s, type_size, global_symbol_list);
	global_token = global_token->next;

	/* Deal with global variables */
	if(match(";", global_token->s))
	{
		/* Ensure enough bytes are allocated to store global variable.
		   In some cases it allocates too much but that is harmless. */
		globals_list = emit(":GLOBAL_", globals_list);
		globals_list = emit(global_token->prev->s, globals_list);

		/* round up division */
		i = ceil_div(type_size->size, register_size);
		globals_list = emit("\n", globals_list);
		while(i != 0)
		{
			globals_list = emit("NULL\n", globals_list);
			i = i - 1;
		}
		global_token = global_token->next;
		goto new_type;
	}

	/* Deal with global functions */
	if(match("(", global_token->s))
	{
		declare_function();
		goto new_type;
	}

	/* Deal with assignment to a global variable */
	if(match("=", global_token->s))
	{
		global_assignment();
		goto new_type;
	}

	/* Deal with global static arrays */
	if(match("[", global_token->s))
	{
		global_static_array(type_size, global_token->prev);
		goto new_type;
	}

	/* Everything else is just an error */
	line_error();
	fputs("Received ", stderr);
	fputs(global_token->s, stderr);
	fputs(" in program\n", stderr);
	exit(EXIT_FAILURE);
}

void recursive_output(struct token_list* head, FILE* out)
{
	struct token_list* i = reverse_list(head);
	while(NULL != i)
	{
		fputs(i->s, out);
		i = i->next;
	}
}

void output_tokens(struct token_list *i, FILE* out)
{
	while(NULL != i)
	{
		fputs(i->s, out);
		fputs(" ", out);
		i = i->next;
	}
}
