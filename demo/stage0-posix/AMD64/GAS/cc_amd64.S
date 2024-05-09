# Copyright (C) 2017 Jeremiah Orians
# Copyright (C) 2022 Andrius Štikonas
# This file is part of stage0.
#
# stage0 is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# stage0 is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with stage0.  If not, see <http://www.gnu.org/licenses/>.

.intel_syntax noprefix
.global _start
.text

    # Register usage:
    # RAX, RSI, RDI => Temps
    # R13 => MALLOC
    # R14 => Output_file
    # R15 => Input_file

    # Struct TYPE format: (size 56)
    # NEXT => 0
    # SIZE => 8
    # OFFSET => 16
    # INDIRECT => 24
    # MEMBERS => 32
    # TYPE => 40
    # NAME => 48

    # Struct TOKEN_LIST format: (size 40)
    # NEXT => 0
    # LOCALS/PREV => 8
    # S => 16
    # TYPE => 24
    # ARGS/DEPTH => 32

# Where the ELF Header is going to hit
# Simply jump to _start
# Our main function
_start:
    pop rax                     # Get the number of arguments
    pop rdi                     # Get the program name
    pop rdi                     # Get the actual input name
    mov rsi, 0                  # prepare read_only
    mov rax, 2                  # the syscall number for open()
    syscall                     # Now open that damn file
    mov r15, rax                # Preserve the file pointer we were given

    pop rdi                     # Get the actual output name
    mov rsi, 577                # Prepare file as O_WRONLY|O_CREAT|O_TRUNC
    mov rdx, 384                # Prepare file as RW for owner only (600 in octal)
    mov rax, 2                  # the syscall number for open()
    syscall                     # Now open that damn file
    cmp rax, 0                  # Check for missing output
    jg _start_out               # Have real input
    mov rax, 1                  # Use stdout

_start_out:
    mov r14, rax                # Preserve the file pointer we were given

    mov rax, 12                 # the Syscall # for SYS_BRK
    mov rdi, 0                  # Get current brk
    syscall                     # Let the kernel do the work
    mov r13, rax                # Set our malloc pointer

    call fix_types              # Resolve relative addresses in types struct to absolute
    mov rax, 0                  # HEAD = NULL
    call read_all_tokens        # Read all tokens
    call Reverse_List           # Reverse order
#    call debug_list             # Try to figure out what is wrong
    mov [rip+global_token], rax # Set global_token
    call program                # Convert into program
    lea rax, [rip+header_string1] # Our header string
    call File_Print             # Print it
    mov rax, [rip+output_list]  # Our output_list
    call recursive_output       # Print core program
#    lea rax, [rip+header_string2] # Our Enable debug
#    call File_Print             # Print it
    lea rax, [rip+header_string3] # Our second label
    call File_Print             # Print it
    mov rax, [rip+globals_list] # Our globals
    call recursive_output       # Get them
    lea rax, [rip+header_string4] # Our final header
    call File_Print             # Print it
    mov rax, [rip+strings_list] # Our strings
    call recursive_output       # Get them
    lea rax, [rip+header_string5] # Our final header
    call File_Print             # Print it

Done:
    # program completed Successfully
    mov rdi, 0                  # All is well
    mov rax, 0x3c               # put the exit syscall number in eax
    syscall                     # Call it a good day

header_string1: .asciz "\n# Core program\n"
header_string2: .asciz "\n:ELF_data\n"
header_string3: .asciz "\n# Program global variables\n"
header_string4: .asciz "\n# Program strings\n"
header_string5: .asciz "\n:ELF_end\n"


# Resolve relative addresses in types struct to absolute
# Uses RAX to store current type, RBX for temp
fix_types:
    push rbx                    # Protect RBX

    lea rax, [rip+prim_types]   # Get address of prim_types
    mov [rip+global_types], rax # Write it to global_types

fix_type:
    mov rbx, [rax+48]           # Get offset to NAME
    add rbx, rax                # Get NAME
    mov [rax+48], rbx           # Store NAME

    mov rbx, [rax+40]           # Get offset to TYPE
    add rbx, rax                # Get TYPE
    mov [rax+40], rbx           # Store TYPE

    mov rbx, [rax+24]           # Get offset to INDIRECT
    add rbx, rax                # Get INDIRECT
    mov [rax+24], rbx           # Store INDIRECT

    mov rbx, [rax]              # Get offset to NEXT
    cmp rbx, 0                  # If no more types
    je fix_types_done           # Then we are done

    add rbx, rax                # Get NEXT
    mov [rax], rbx              # Store NEXT

    add rax, 56                 # Go to next type
    jmp fix_type

fix_types_done:

    pop rbx                     # Restore RBX
    ret


# read_all_tokens function
# Receives FILE* in R15 and Token_List* in RAX
# Tokenizes all input and returns updated list in RAX
# Returns TOKEN in RAX
# Uses RAX for C
read_all_tokens:
    mov [rip+Token], rax
    call fgetc
read_all_tokens_loop:
    cmp rax, -4                 # Check for EOF
    je read_all_tokens_done     # Stop if found
    call get_token              # Read all tokens
    jmp read_all_tokens_loop    # Loop
read_all_tokens_done:
    mov rax, [rip+Token]
    ret


# get_token function
# Receives INT in RAX and FILE* in R15
# Makes a list of TOKEN_LIST
# C and STRING_INDEX are stored in memory, RCX is used for S and RDX is used for current
# Returns C in RAX
get_token:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX

    mov [rip+C], rax            # Set C

    mov rax, 40                 # Malloc CURRENT
    call malloc                 # Get Pointer
    mov rdx, rax                # Set CURRENT

    mov rax, 256                # Malloc the string
    call malloc                 # Get pointer to S
    mov rcx, rax                # Set S
    mov [rdx+16], rcx           # CURRENT->S = S
reset:
    mov [rip+string_index], rcx # S[0]
    mov rax, [rip+C]            # Using C

    call clear_white_space      # Clear WhiteSpace
    mov [rip+C], rax            # Set C

    cmp rax, -4                 # Check for EOF
    je get_token_abort          # if EOF abort

    cmp rax, 35                 # Check for '#'
    jne get_token_alpha         # Nope

    # Deal with # line comments
    call purge_macro            # Let it handle it
    mov [rip+C], rax            # Set C
    jmp reset                   # Try again

get_token_alpha:
    mov rax, [rip+C]            # Send C
    lea rbx, [rip+alphas]       # Get alphanumerics
    call In_Set                 # See if in set
    cmp rax, 1                  # IF TRUE
    jne get_token_symbol        # Otherwise

    # Store keywords
    mov rax, [rip+C]            # Send C
    call preserve_keyword       # Store
    mov [rip+C], rax            # Set C
    jmp get_token_done          # Be done with this token

get_token_symbol:
    mov rax, [rip+C]            # Send C
    lea rbx, [rip+symbols]      # Get symbols
    call In_Set                 # See if in set
    cmp rax, 1                  # IF TRUE
    jne get_token_strings       # Otherwise

    # Store symbols
    mov rax, [rip+C]            # Send C
    call preserve_symbol        # Store
    mov [rip+C], rax            # Set C
    jmp get_token_done          # Be done with this token

get_token_strings:
    mov rax, [rip+C]            # Send C
    lea rbx, [rip+strings]      # Get strings
    call In_Set                 # See if in set
    cmp rax, 1                  # IF TRUE
    jne get_token_comment       # Otherwise

    # Store String
    mov rax, [rip+C]            # Send C
    call consume_word           # Store
    mov [rip+C], rax            # Set C
    jmp get_token_done          # Be done with this token

get_token_comment:
    mov rax, [rip+C]            # Send C
    cmp rax, 47                 # IF '/' == C
    jne get_token_else          # Otherwise

    call consume_byte           # Hope it just is '/'
    mov [rip+C], rax            # Set C

    cmp rax, 42                 # IF '*' we have '/*'
    jne get_token_comment_line  # Check for '//'

    # Deal with /* block comments */
    call fgetc                  # get next C
    mov [rip+C], rax            # Set C
get_token_comment_block_outer:
    mov rax, [rip+C]            # Using C
    cmp rax, 47                 # IF '/' != C
    je get_token_comment_block_done # be done

get_token_comment_block_inner:
    mov rax, [rip+C]            # Using C
    cmp rax, 42                 # IF '*' != C
    je get_token_comment_block_iter # jump over

    # Deal with inner loop
    call fgetc                  # get next C
    mov [rip+C], rax            # Set C
    jmp get_token_comment_block_inner # keep going

get_token_comment_block_iter:
    call fgetc                  # get next C
    mov [rip+C], rax            # Set C
    jmp get_token_comment_block_outer

get_token_comment_block_done:
    call fgetc                  # get next C
    mov [rip+C], rax            # Set C
    jmp reset                   # throw away, try again

get_token_comment_line:
    cmp rax, 47                 # IF '/' we have //
    jne get_token_done          # keep if just '/'

    # Deal with // line comment
    call fgetc                  # drop to match
    mov [rip+C], rax            # Set C
    jmp reset                   # throw away, try again

get_token_else:
    mov rax, [rip+C]            # Send C
    call consume_byte
    mov [rip+C], rax            # Set C

get_token_done:
    mov rax, [rip+Token]        # TOKEN
    mov [rdx+8], rax            # CURRENT->PREV = TOKEN
    mov [rdx], rax              # CURRENT->NEXT = TOKEN
    mov [rip+Token], rdx        # TOKEN = CURRENT

get_token_abort:
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    mov rax, [rip+C]            # Return C
    ret


# Malloc isn't actually required if the program being built fits in the initial memory
# However, it doesn't take much to add it.
# Requires R13 to be initialized and RAX to have the number of desired bytes
malloc:
    mov rdi, r13                # Using the current pointer
    add rdi, rax                # Request the number of desired bytes
    mov rax, 12                 # the Syscall # for SYS_BRK
    push rcx                    # Protect rcx
    push r11                    # Protect r11
    syscall                     # call the Kernel
    pop r11                     # Restore r11
    pop rcx                     # Restore rcx
    mov rax, r13                # Return pointer
    mov r13, rdi                # Update pointer
    ret


# clear_white_space function
# Receives INT C in RAX and FILE* in R15
# Returns first non-whitespace char in RAX
clear_white_space:
    cmp rax, 32                 # Check for ' '
    je clear_white_space_wipe   # wipe it out

    cmp rax, 10                 # Check for '\n'
    je clear_white_space_wipe   # wipe it output

    cmp rax, 9                  # Check for '\t'
    jne clear_white_space_done  # looks like non-whitespace

clear_white_space_wipe:
    call fgetc                  # Read a new byte
    cmp rax, -4                 # Check for EOF
    je clear_white_space_done   # Short circuit
    jmp clear_white_space       # iterate

clear_white_space_done:
    ret


# In_Set function
# Receives Char C in RAX and CHAR* in RBX
# Returns 1 if true, zero if false in RAX
In_Set:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
In_Set_loop:
    mov cl, [rbx]               # Read char
    movzx rcx, cl               # Zero extend it

    cmp rax, rcx                # See if they match
    je In_Set_True              # return true

    cmp rcx, 0                  # Check for NULL
    je In_Set_False             # return false

    add rbx, 1                  # s = s + 1
    jmp In_Set_loop             # Keep looping

In_Set_True:
    mov rax, 1                  # Set True
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

In_Set_False:
    mov rax, 0                  # Set FALSE
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

alphas: .asciz "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz"
symbols: .asciz "<=>|&!-"
strings: .byte 34, 39, 0


# purge_macro function
# Receives CH in RAX
# Reads chars until Line feed is read
# returns line feed
purge_macro:
    call fgetc                  # read next char
    cmp rax, 10                 # Check for '\n'
    jne purge_macro             # Keep going
    ret


# preserve_keyword function
# Receives INT C in RAX
# collects all chars in keyword
# Returns C in RAX
# Uses RCX for INT C
preserve_keyword:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rcx, rax                # Setup C
    lea rbx, [rip+alphas]       # Concerning ourselves with "abc.."
preserve_keyword_loop:
    call In_Set                 # Check if alphanumerics
    cmp rax, 1                  # IF TRUE
    jne preserve_keyword_label  # Otherwise check for label

    mov rax, rcx                # Pass C
    call consume_byte           # consume that byte
    mov rcx, rax                # Update C
    jmp preserve_keyword_loop   # keep looping

preserve_keyword_label:
    mov rax, rcx                # Fix return
    cmp rax, 58                 # Check for ':'
    jne preserve_keyword_done   # be done

    # Fix our goto label
    call fixup_label            # Fix the label
    mov rax, 32                 # Return Whitespace

preserve_keyword_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# preserve_symbol function
# Receives INT C in RAX
# collects all chars in symbol
# Returns C in RAX
# Uses RCX for INT C
preserve_symbol:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rcx, rax                # Setup C
    lea rbx, [rip+symbols]      # Concerning ourselves with "<=>.."
preserve_symbol_loop:
    call In_Set                 # Check if symbol
    cmp rax, 1                  # IF TRUE
    jne preserve_symbol_done    # Otherwise be done

    mov rax, rcx                # Pass C
    call consume_byte           # consume that byte
    mov rcx, rax                # Update C
    jmp preserve_symbol_loop    # keep looping

preserve_symbol_done:
    mov rax, rcx                # Fix return
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# consume_word function
# receives INT C in RAX
# returns INT C in RAX
# Uses RAX for C, RBX for FREQ and RCX for ESCAPE
consume_word:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rbx, rax                # FREQ = C
    mov rcx, 0                  # ESCAPE = FALSE
consume_word_loop:
    cmp rcx, 0                  # IF !ESCAPE
    jne consume_word_escape     # Enable escape

    cmp rax, 92                 # if '\\'
    jne consume_word_iter       # keep state

    mov rcx, 1                  # ESCAPE = TRUE
    jmp consume_word_iter       # keep going

consume_word_escape:
    mov rcx, 0                  # ESCAPE = FALSE

consume_word_iter:
    call consume_byte           # read next char

    cmp rcx, 0                  # IF ESCAPE
    jne consume_word_loop       # keep looping

    cmp rax, rbx                # IF C != FREQ
    jne consume_word_loop       # keep going

    call fgetc                  # return next char
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# consume_byte function
# Receives INT C in RAX
# Inserts C into string S, updates String S
# Returns Next char in RAX
consume_byte:
    push rbx                    # Protect RBX
    mov rbx, [rip+string_index] # S[0]
    mov [rbx], al               # S[0] = C
    add rbx, 1                  # S = S + 1
    mov [rip+string_index], rbx # Update S
    call fgetc
    pop rbx                     # Restore RBX
    ret


# fixup_label function
# Receives S in RCX
# prepends ':' to string and returns registers unchanged
# Uses RAX for HOLD, RBX for PREV and RCX for S[0]
fixup_label:
    push rax                    # Protect RAX
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rax, 58                 # HOLD = ':'
    mov rcx, [rdx+16]           # HOLD_STRING[0]
fixup_label_loop:
    mov rbx, rax                # PREV = HOLD
    mov al, [rcx]               # HOLD = HOLD_STRING[I]
    movzx rax, al               # make useful
    mov [rcx], bl               # HOLD_STRING[I] = PREV
    add rcx, 1                  # I = I + 1
    cmp rax, 0                  # IF NULL == HOLD
    jne fixup_label_loop        # Keep looping

    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    pop rax                     # Restore RAX
    ret


# fgetc function
# Receives FILE* in R15
# Returns -4 (EOF) or char in RAX
fgetc:
    mov rax, -4                 # Put EOF in rax
    push rax                    # Assume bad (If nothing read, value will remain EOF)
    lea rsi, [rsp]              # Get stack address
    mov rdi, r15                # Where are we reading from
    mov rax, 0                  # the syscall number for read
    push rdx                    # Protect RDX
    mov rdx, 1                  # set the size of chars we want
    push rcx                    # Protect RCX
    push r11                    # Protect R11
    syscall                     # call the Kernel
    pop r11                     # Restore R11
    pop rcx                     # Restore RCX
    pop rdx                     # Restore RDX
    pop rax                     # Get either char or EOF
    cmp rax, -4                 # Check for EOF
    je fgetc_done               # Return as is
    movzx rax, al               # Make it useful
fgetc_done:
    ret


# Reverse_List function
# Receives List in RAX
# Returns the list reversed in RAX
Reverse_List:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rbx, rax                # Set HEAD
    mov rax, 0                  # ROOT = NULL
Reverse_List_Loop:
    cmp rbx, 0                  # WHILE HEAD != NULL
    je Reverse_List_Done        # Stop otherwise

    mov rcx, [rbx]              # NEXT = HEAD->NEXT
    mov [rbx], rax              # HEAD->NEXT = ROOT
    mov rax, rbx                # ROOT = HEAD
    mov rbx, rcx                # HEAD = NEXT
    jmp Reverse_List_Loop       # Keep Going

Reverse_List_Done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# recursive_output function
# Receives list in RAX
# walks the list and prints the I->S for all nodes backwards
# Uses RBX for I
recursive_output:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    cmp rax, 0                  # Check for NULL
    je recursive_output_done    # Skip the work
    mov rbx, rax                # I = Head

    mov rax, [rbx]              # Iterate to next Token
    call recursive_output       # Recurse

    mov rax, [rbx+16]           # Using S
    call File_Print             # Print it

recursive_output_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# File_Print function
# Receives CHAR* in RAX
# calls fputc for every non-null char
File_Print:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rbx, rax                # Protect S
    cmp rax, 0                  # Protect against nulls
    je File_Print_Done          # Simply don't try to print them
File_Print_Loop:
    mov al, [rbx]               # Read byte
    movzx rax, al               # zero extend
    cmp rax, 0                  # Check for NULL
    je File_Print_Done          # Stop at NULL

    call fputc                  # write it
    add rbx, 1                  # S = S + 1
    jmp File_Print_Loop         # Keep going

File_Print_Done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# fputc function
# receives CHAR in RAX and FILE* in R14
# writes char and returns
fputc:
    push rax                    # We are writing rax
    lea rsi, [rsp]              # Get stack address
    mov rdi, r14                # Write to target file
    mov rax, 1                  # the syscall number for write
    push rdx                    # Protect RDX
    mov rdx, 1                  # set the size of chars we want
    push rcx                    # Protect RCX
    push r11                    # Protect R11
    syscall                     # call the Kernel
    pop r11                     # Restore R11
    pop rcx                     # Restore RCX
    pop rdx                     # Restore RDX
    pop rax                     # Restore stack
    ret


# program function
# receives nothing, returns nothing
# Uses RAX for type_size
program:
    # The binary initialized the globals to null, so we can skip those steps
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX

new_type:
    mov rax, [rip+global_token] # Using global_token
    cmp rax, 0                  # Check if NULL
    je program_done             # Be done if null

    mov rbx, [rax+16]           # GLOBAL_TOKEN->S
    lea rax, [rip+constant]     # "CONSTANT"
    call match                  # IF GLOBAL_TOKEN->S == "CONSTANT"
    cmp rax, 0                  # If true
    jne program_else            # Looks like not a constant

    # Deal with minimal constant case
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next

    mov rax, [rax+16]           # global_token->S
    mov rbx, 0                  # NULL
    mov rcx, [rip+global_constant_list] # global_constant_list
    call sym_declare            # Declare that constant
    mov [rip+global_constant_list], rax # global_constant_list = sym_declare(global_token->s, NULL, global_constant_list);

    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx]              # global_token->next
    mov [rax+32], rbx           # global_constant_list->arguments = global_token->next

    mov rbx, [rbx]              # global_token->next->next
    mov [rip+global_token], rbx # global_token = global_token->next->next;
    jmp new_type                # go around again

program_else:
    call type_name              # Figure out the type_size
    cmp rax, 0                  # IF NULL == type_size
    je new_type                 # it was a new type

    # Add to global symbol table
    mov rbx, rax                # put type_size in the right spot
    mov rax, [rip+global_token] # Using global token
    mov rax, [rax+16]           # global_token->S
    mov rcx, [rip+global_symbol_list] # Using global_symbol_list
    call sym_declare            # Declare symbol
    mov [rip+global_symbol_list], rax # global_symbol_list = sym_declare(global_token->s, type_size, global_symbol_list);
    mov rbx, [rip+global_token] # Using global token
    mov rbx, [rbx]              # global_token->next
    mov [rip+global_token], rbx # global_token = global_token->next

    mov rbx, [rip+global_token] # Using global token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+semicolon]    # ";"
    call match                  # if(match(";", global_token->s))
    cmp rax, 0                  # If true
    jne program_function        # looks like not a match

    # Deal with the global variable
    mov rbx, [rip+globals_list] # Using globals_list
    lea rax, [rip+program_string_0] # ":GLOBAL_"
    call emit                   # Emit it
    mov rbx, rax                # update globals_list

    mov rax, [rip+global_token] # Using global token
    mov rax, [rax+8]            # global token->prev
    mov rax, [rax+16]           # global token->prev->s
    call emit                   # Emit it

    mov rbx, rax                # update globals_list
    lea rax, [rip+program_string_1] # "\nNULL\n"
    call emit                   # Emit it
    mov [rip+globals_list], rax # update globals_list

    mov rax, [rip+global_token] # Using global token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next
    jmp new_type                # go around again

program_function:
    mov rbx, [rip+global_token] # Using global token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+open_paren]   # "("
    call match                  # if(match(";", global_token->s))
    cmp rax, 0                  # If true
    jne program_error           # Otherwise deal with error case

    # Deal with function definition
    call declare_function       # Lets get the parsing rolling
    jmp new_type                # Keep looping through functions

program_error:
    # Deal with the case of something we don't support

program_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

# Strings needed by the program function
program_string_0: .asciz ":GLOBAL_"
program_string_1: .asciz "\nNULL\n"


# declare_function function
# Receives nothing and returns nothing
# Sets current function and adds it to the global function list
declare_function:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rax, 0                  # Using NULL
    mov [rip+current_count], rax # current_count = 0

    mov rax, [rip+global_token] # Using global token
    mov rax, [rax+8]            # global token->prev
    mov rax, [rax+16]           # global token->prev->s
    mov rbx, 0                  # NULL
    mov rcx, [rip+global_function_list] # global_function_list
    call sym_declare            # sym_declare(global_token->prev->s, NULL, global_function_list);
    mov [rip+function], rax     # function = sym_declare(global_token->prev->s, NULL, global_function_list);
    mov [rip+global_function_list], rax # global_function_list = function

    call collect_arguments      # collect all of the function arguments

    mov rax, [rip+global_token] # Using global token
    mov rax, [rax+16]           # global token->s
    lea rbx, [rip+semicolon]    # ";"
    call match                  # IF global token->s == ";"
    cmp rax, 0                  # If true
    jne declare_function_full   # It was a prototype

    # Deal with prototypes
    mov rax, [rip+global_token] # Using global token
    mov rax, [rax]              # global token->next
    mov [rip+global_token], rax # global token = global token->next
    jmp declare_function_done   # Move on

declare_function_full:
    # Deal with full function definitions
    lea rax, [rip+declare_function_string_0] # "# Defining function "
    call emit_out               # emit it

    mov rax, [rip+function]     # function
    mov rax, [rax+16]           # function->s
    call emit_out               # emit it

    lea rax, [rip+declare_function_string_1] # "\n:FUNCTION_"
    call emit_out               # emit it

    mov rax, [rip+function]     # function
    mov rax, [rax+16]           # function->s
    call emit_out               # emit it

    lea rax, [rip+declare_function_string_3] # "\n"
    call emit_out               # emit it

    call statement              # Recursively get the function pieces

    mov rax, [rip+output_list]  # output
    mov rax, [rax+16]           # output->s
    lea rbx, [rip+declare_function_string_2] # "RETURN\n"
    call match                  # IF output->s == "RETURN\n"
    cmp rax, 0                  # If true we can skip adding it
    je declare_function_done    # otherwise we need to add it

    # Add the return to the end of a function lacking a return;
    lea rax, [rip+declare_function_string_2] # "RETURN\n"
    call emit_out               # emit it

declare_function_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

declare_function_string_0: .asciz "# Defining function "
declare_function_string_1: .asciz "\n:FUNCTION_"
declare_function_string_2: .asciz "RETURN\n"
declare_function_string_3: .byte 10, 0


# collect_arguments function
# Receives nothing
# Returns Nothing
# Adds arguments to the function definition
# holds struct type* type_size in RCX, then replace with struct token_list* a in RCX when type_size is used
collect_arguments:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next
collect_arguments_loop:
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+close_paren]  # ")"
    call match                  # IF global_token->S == ")"
    cmp rax, 0                  # we reached the end
    je collect_arguments_done   # be done

    # deal with the case of there are arguments
    call type_name              # Get the type
    mov rcx, rax                # put type_size safely out of the way

    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+close_paren]  # ")"
    call match                  # IF global_token->S == ")"
    cmp rax, 0                  # is a foo(int, char,void) case
    je collect_arguments_common # deal with commas

    # Trying second else
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+comma]        # ","
    call match                  # IF global_token->S == ","
    cmp rax, 0                  # then deal with the common
    je collect_arguments_common # case of commas between arguments

    # deal with foo(int a, char b)
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov rbx, rcx                # put type_size in the right place
    mov rcx, [rip+function]     # Using function
    mov rcx, [rcx+32]           # function->args
    call sym_declare            # sym_declare(global_token->s, type_size, function->arguments);
    mov rcx, rax                # put a in a safe place

    mov rax, [rip+function]     # Using function
    mov rax, [rax+32]           # function->args
    cmp rax, 0                  # IF function->args == NULL
    jne collect_arguments_another # otherwise it isn't the first

    # Deal with the case of first argument in the function
    mov rax, -8                 # -8
    mov [rcx+32], rax           # a->depth = -8
    jmp collect_arguments_next  # get to next

collect_arguments_another:
    # deal with the case of non-first arguments
    mov rax, [rip+function]     # Using function
    mov rax, [rax+32]           # function->args
    mov rax, [rax+32]           # function->args->depth
    sub rax, 8                  # function->args->depth - 8
    mov [rcx+32], rax           # a->depth = function->args->depth - 8

collect_arguments_next:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next

    mov rax, [rip+function]     # Using function
    mov [rax+32], rcx           # function->args = a

collect_arguments_common:
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+comma]        # ","
    call match                  # IF global_token->S == ","
    cmp rax, 0                  # then deal with the comma
    jne collect_arguments_loop  # otherwise loop

    # keep foo(bar(), 1) expressions working
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next
    jmp collect_arguments_loop  # keep going

collect_arguments_done:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# statement function
# Receives nothing
# Returns nothing
# Walks down global_token recursively to collect the contents of the function
statement:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX

    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+open_curly_brace] # "{"
    call match                  # IF global_token->S == "{"
    jne statement_label         # otherwise try label

    # deal with { statement }
    call recursive_statement    # Statements inside of statements for days
    jmp statement_done          # Be done

statement_label:
    mov al, [rbx]               # global_token->S[0]
    movzx rax, al               # make it useful
    cmp rax, 58                 # IF global_token->S == ':'
    jne statement_local         # otherwise try locals

    # deal with labels
    mov rax, rbx                # put global_token->S in the right spot
    call emit_out               # emit it

    lea rax, [rip+statement_string_0] # Using "\t#C goto label\n"
    call emit_out               # emit it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next
    jmp statement_done          # be done

statement_local:
    mov rax, rbx                # put global_token->S in the right place
    lea rbx, [rip+prim_types]   # pointer to primitive types
    call lookup_type            # See if found
    cmp rax, 0                  # IF NULL == lookup_type(global_token->S, prim_types)
    jne statement_local_success # Sweet a new local

    # Second chance
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+struct]       # "struct"
    call match                  # IF global_token->S == "struct"
    cmp rax, 0                  # then we are a local
    jne statement_if            # otherwise try IF

statement_local_success:
    call collect_local          # Grab those locals
    jmp statement_done          # be done

statement_if:
    lea rax, [rip+if_string]    # Using "if"
    call match                  # IF global_token->S == "if"
    cmp rax, 0                  # then we have an if statement
    jne statement_do            # otherwise try DO

    # Deal with IF statement
    call process_if             # DO IT
    jmp statement_done          # be done

statement_do:
    lea rax, [rip+do_string]    # Using "do"
    call match                  # IF global_token->S == "do"
    cmp rax, 0                  # then we have a do statement
    jne statement_while         # otherwise try WHILE

    # Deal with DO statement
    call process_do             # DO IT
    jmp statement_done          # be done

statement_while:
    lea rax, [rip+while_string] # Using "while"
    call match                  # IF global_token->S == "while"
    cmp rax, 0                  # then we have a while statement
    jne statement_for           # otherwise try FOR

    # Deal with WHILE statement
    call process_while          # DO IT
    jmp statement_done          # be done

statement_for:
    lea rax, [rip+for_string]   # Using "for"
    call match                  # IF global_token->S == "for"
    cmp rax, 0                  # then we have a for statement
    jne statement_asm           # otherwise try ASM

    # Deal with FOR statement
    call process_for            # DO IT
    jmp statement_done          # be done

statement_asm:
    lea rax, [rip+asm_string]   # Using "asm"
    call match                  # IF global_token->S == "asm"
    cmp rax, 0                  # then we have an asm statement
    jne statement_goto          # otherwise try GOTO

    # Deal with ASM statement
    call process_asm            # Hit it
    jmp statement_done          # be done

statement_goto:
    lea rax, [rip+goto_string]  # Using "goto"
    call match                  # IF global_token->S == "goto"
    cmp rax, 0                  # then we have a goto statement
    jne statement_return        # Otherwise try RETURN

    # Deal with GOTO statement
    lea rax, [rip+statement_string_1] # Using "JUMP %"
    call emit_out               # emit it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next

    mov rax, [rax+16]           # global_token->S
    call emit_out               # emit it

    lea rax, [rip+statement_string_2] # Using "\n"
    call emit_out               # emit it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next

    lea rax, [rip+statement_string_4] # Using "ERROR in statement\nMissing ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure it has the required
    jmp statement_done          # Be done

statement_return:
    lea rax, [rip+return_string] # Using "return"
    call match                  # IF global_token->S == "return"
    cmp rax, 0                  # then we have a return statement
    jne statement_break         # Otherwise try BREAK

    # Deal with RETURN Statement
    call return_result          # Return anything they want
    jmp statement_done          # be done

statement_break:
    lea rax, [rip+break_string] # Using "break"
    call match                  # IF global_token->S == "break"
    cmp rax, 0                  # then we have a break statement
    jne statement_continue      # Otherwise try CONTINUE

    # Deal with BREAK statement
    call process_break          # Lets do some damage
    jmp statement_done          # be done

statement_continue:
    lea rax, [rip+continue_string] # Using "continue"
    call match                  # IF global_token->S == "continue"
    cmp rax, 0                  # then we have a continue statement
    jne statement_else          # Otherwise we are punting to an expression

    # Deal with CONTINUE statement
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next

    lea rax, [rip+statement_string_3] # Using "\n#continue statement\n"
    call emit_out               # emit it

    lea rax, [rip+statement_string_4] # Using "ERROR in statement\nMissing ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Don't forget the ";"
    jmp statement_done          # Be done

statement_else:
    call expression             # Collect expression
    lea rax, [rip+statement_string_4] # Using "ERROR in statement\nMissing ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # make sure we have it

statement_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

statement_string_0: .asciz "\t#C goto label\n"
statement_string_1: .asciz "JUMP %"
statement_string_2: .byte 10, 0
statement_string_3: .asciz "\n#continue statement\n"
statement_string_4: .asciz "ERROR in statement\nMissing ;\n"


# recursive_statement function
# Receives nothing
# Returns nothing
# Walks the global_token list to build the contents of statements
# Uses struct token_list* frame in RCX
recursive_statement:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next

    mov rcx, [rip+function]     # Using function
    mov rcx, [rcx+8]            # frame = function->locals

recursive_statement_loop:
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+close_curly_brace] # Using "}"
    call match                  # IF global_token->S == "}"
    cmp rax, 0                  # Then we are done recursing
    je recursive_statement_cleanup # and then we clean up

    # Deal with the recursive calls
    call statement              # Deal with another statement
    jmp recursive_statement_loop # loop some more

recursive_statement_cleanup:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next

    lea rax, [rip+recursive_statement_string_0] # Using "RETURN\n"
    mov rbx, [rip+output_list]  # Using output
    mov rbx, [rbx+16]           # output->S
    call match                  # IF output->S == "RETURN\n"
    cmp rax, 0                  # Then we can skip the clean up
    je recursive_statement_done # and be done

    # Deal with cleanup
    mov rbx, [rip+function]     # Using function
    mov rbx, [rbx+8]            # i = function->locals
    lea rax, [rip+recursive_statement_string_1] # Using "POP_RBX\t# _recursive_statement_locals\n"

recursive_statement_locals:
    cmp rbx, rcx                # IF frame != i
    je recursive_statement_done # Otherwise be done

    # Lets emit
    call emit_out               # emit it
    mov rbx, [rbx]              # i = i->next
    jmp recursive_statement_locals # keep going

recursive_statement_done:
    mov rax, [rip+function]     # Using function
    mov [rax+8], rcx            # function->locals = frame
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

recursive_statement_string_0: .asciz "RETURN\n"
recursive_statement_string_1: .asciz "POP_RBX\t# _recursive_statement_locals\n"


# return_result function
# Receives nothing
# Returns nothing
# Cleans up function and generates return
# Also handles returning expressions
return_result:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next

    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # make it useful
    cmp rax, 59                 # If global_token->S[0] == ';'
    je return_result_cleanup    # Go straight to cleanup

    call expression             # get the expression we are returning

return_result_cleanup:
    lea rax, [rip+return_result_string_0] # Using "ERROR in return_result\nMISSING ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it

    mov rbx, [rip+function]     # Using function
    mov rbx, [rbx+8]            # function->locals
    lea rax, [rip+return_result_string_1] # Using "POP_RBX\t# _return_result_locals\n"
return_result_locals:
    cmp rbx, 0                  # IF NULL == i
    je return_result_done       # Be done

    call emit_out               # Emit out pop
    mov rbx, [rbx]              # i = i->NEXT
    jmp return_result_locals    # Keep going

return_result_done:
    lea rax, [rip+return_result_string_2] # Using "RETURN\n"
    call emit_out               # Emit it
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

return_result_string_0: .asciz "ERROR in return_result\nMISSING ;\n"
return_result_string_1: .asciz "POP_RBX\t# _return_result_locals\n"
return_result_string_2: .asciz "RETURN\n"


# collect_local function
# Receives nothing
# Returns nothing
# Walks global_token list to create function locals
# Uses RCX for struct token_list* A
collect_local:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    call type_name              # Get the local's type

    mov rbx, rax                # Put struct type* type_size in the right place
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov rcx, [rip+function]     # Using function
    mov rcx, [rcx+8]            # function->locals
    call sym_declare            # Declare it
    mov rcx, rax                # put it away safely

    # Try for main
    lea rax, [rip+main_string]  # Using "main"
    mov rbx, [rip+function]     # Using function
    mov rbx, [rbx+16]           # function->S
    call match                  # IF match("main", function->s)
    cmp rax, 0                  # possible
    jne collect_local_fresh     # try to see if fresh function

    # Ok we are in main, now to see if main is fresh
    mov rax, [rip+function]     # Using function
    mov rax, [rax+8]            # function->locals
    cmp rax, 0                  # IF NULL == function->locals
    jne collect_local_fresh     # try to see if fresh function

    # Sweet we are in a fresh main
    mov rax, -40                # We start at -40
    mov [rcx+32], rax           # a->DEPTH = -40
    jmp collect_local_common    # Go to the commons

collect_local_fresh:
    mov rax, [rip+function]     # Using function
    mov rax, [rax+32]           # function->args
    cmp rax, 0                  # IF NULL == function->args
    jne collect_local_first     # Otherwise see if first

    mov rax, [rip+function]     # Using function
    mov rax, [rax+8]            # function->locals
    cmp rax, 0                  # IF NULL == function->locals
    jne collect_local_first     # Otherwise try first

    # Sweet we are in a fresh function
    mov rax, -16                # We start at -16
    mov [rcx+32], rax           # a->DEPTH = -16
    jmp collect_local_common    # Go to the commons

collect_local_first:
    mov rax, [rip+function]     # Using function
    mov rax, [rax+8]            # function->locals
    cmp rax, 0                  # IF NULL == function->locals
    jne collect_local_else      # Looks like we are just another local

    # Ok we are the first local
    mov rax, [rip+function]     # Using function
    mov rax, [rax+32]           # function->args
    mov rax, [rax+32]           # function->args->depth
    sub rax, 16                 # function->arguments->depth - 16
    mov [rcx+32], rax           # a->DEPTH = function->arguments->depth - 16
    jmp collect_local_common    # Go to the commons

collect_local_else:
    # Always the last to know
    mov rax, [rip+function]     # Using function
    mov rax, [rax+8]            # function->locals
    mov rax, [rax+32]           # function->locals->depth
    sub rax, 8                  # function->locals->depth - 8
    mov [rcx+32], rax           # a->DEPTH = function->locals->depth - 8

collect_local_common:
    mov rax, [rip+function]     # Using function

    mov [rax+8], rcx            # function->locals = a
    mov rcx, [rcx+16]           # a->S

    lea rax, [rip+collect_local_string_0] # Using "# Defining local "
    call emit_out               # emit it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    call emit_out               # emit it

    lea rax, [rip+collect_local_string_1] # Using "\n"
    call emit_out               # emit it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    mov rbx, [rax+16]           # global_token->S
    lea rax, [rip+equal]        # Using "="
    call match                  # IF match("=", global_token->s)
    cmp rax, 0                  # Deal with assignment
    jne collect_local_done      # Otherwise finish it

    # Deal with assignment
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    call expression             # Recurse

collect_local_done:
    lea rax, [rip+collect_local_string_2] # Using "ERROR in collect_local\nMissing ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it

    lea rax, [rip+collect_local_string_3] # Using "PUSH_RAX\t#"
    call emit_out               # emit it

    mov rax, rcx                # put A->S where it belongs
    call emit_out               # emit it

    lea rax, [rip+collect_local_string_1] # Using "\n"
    call emit_out               # emit it

    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

collect_local_string_0: .asciz "# Defining local "
collect_local_string_1: .byte 10, 0
collect_local_string_2: .asciz "ERROR in collect_local\nMissing ;\n"
collect_local_string_3: .asciz "PUSH_RAX\t#"


# process_asm function
# Receives nothing
# Returns nothing
# Simply inlines the asm statements
# Uses RBX for global_token temp storage
process_asm:
    push rbx                    # Protect RBX
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    lea rax, [rip+process_asm_string_0] # Using "ERROR in process_asm\nMISSING (\n"
    lea rbx, [rip+open_paren]   # Using "("
    call require_match          # Make sure we have it

    mov rbx, [rip+global_token] # Using global_token
process_asm_iter:
    mov rax, [rbx+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    cmp rax, 34                 # IF global_token->S[0] == '"'
    jne process_asm_done        # Otherwise be done

    mov rax, [rbx+16]           # global_token->S
    add rax, 1                  # global_token->S + 1
    call emit_out               # Emit it

    lea rax, [rip+process_asm_string_1] # Using "\n"
    call emit_out               # Emit it

    mov rbx, [rbx]              # global_token->NEXT
    mov [rip+global_token], rbx # global_token = global_token->NEXT
    jmp process_asm_iter        # keep going

process_asm_done:
    lea rax, [rip+process_asm_string_2] # Using "ERROR in process_asm\nMISSING )\n"
    lea rbx, [rip+close_paren]  # Using ")"
    call require_match          # Make sure we have it

    lea rax, [rip+process_asm_string_3] # Using "ERROR in process_asm\nMISSING ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it

    pop rbx                     # Restore RBX
    ret

process_asm_string_0: .asciz "ERROR in process_asm\nMISSING (\n"
process_asm_string_1: .byte 10, 0
process_asm_string_2: .asciz "ERROR in process_asm\nMISSING )\n"
process_asm_string_3: .asciz "ERROR in process_asm\nMISSING ;\n"


# process_if function
# Receives nothing
# Returns Nothing
# Increments current_count recurses into expression + statement
# Uses RCX for char* NUMBER_STRING
process_if:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rax, [rip+current_count] # Using current count
    mov rbx, rax                # Preparing for update
    add rbx, 1                  # current_count + 1
    mov [rip+current_count], rbx # current_count = current_count + 1
    call numerate_number        # convert to string
    mov rcx, rax                # put NUMBER_STRING in place

    lea rax, [rip+process_if_string_0] # Using "# IF_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    lea rax, [rip+process_if_string_1] # Using "ERROR in process_if\nMISSING (\n"
    lea rbx, [rip+open_paren]   # Using "("
    call require_match          # Make sure we have it

    call expression             # Recurse to get the IF(...) part

    lea rax, [rip+process_if_string_2] # Using "TEST\nJUMP_EQ %ELSE_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_if_string_3] # Using "ERROR in process_if\nMISSING )\n"
    lea rbx, [rip+close_paren]  # Using ")"
    call require_match          # Make sure we have it

    call statement              # Recursive to get the IF(){...} part

    lea rax, [rip+process_if_string_4] # Using "JUMP %_END_IF_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_if_string_5] # Using ":ELSE_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+else_string]  # Using "else"
    call match                  # IF global_token->S == "else"
    cmp rax, 0                  # Then we need to collect the else too
    jne process_if_done         # Otherwise finish up

    # deal with else statement
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    call statement              # Recurse to get the ELSE {...} part

process_if_done:
    lea rax, [rip+process_if_string_6] # Using ":_END_IF_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

process_if_string_0: .asciz "# IF_"
process_if_string_1: .asciz "ERROR in process_if\nMISSING (\n"
process_if_string_2: .asciz "TEST\nJUMP_EQ %ELSE_"
process_if_string_3: .asciz "ERROR in process_if\nMISSING )\n"
process_if_string_4: .asciz "JUMP %_END_IF_"
process_if_string_5: .asciz ":ELSE_"
process_if_string_6: .asciz ":_END_IF_"


# save_break_frame microfunction
# Overwrites RAX and RBX
# Saves break frame on stack
# Returns to caller
save_break_frame:
    pop rbx                     # Save return Address
    mov rax, [rip+break_frame]  # Get break_frame
    push rax                    # Store as nested_locals
    mov rax, [rip+break_target_head] # Get break_target_head
    push rax                    # Store as nested_break_head
    mov rax, [rip+break_target_func] # Get break_target_func
    push rax                    # Store as nested_break_func
    mov rax, [rip+break_target_num] # Get break_target_num
    push rax                    # Store as nested_break_num
    push rbx                    # Put return back in place
    ret                         # Return to caller


# restore_break_frame microfunction
# Overwrites RAX and RBX
# Restores break frame from stack
# Returns to caller
restore_break_frame:
    pop rbx                     # Save return Address
    pop rax                     # Get nested_break_num
    mov [rip+break_target_num], rax # Restore break_target_num
    pop rax                     # Get nested_break_func
    mov [rip+break_target_func], rax # Restore break_target_func
    pop rax                     # Get nested_break_head
    mov [rip+break_target_head], rax # Restore break_target_head
    pop rax                     # Get nested_locals
    mov [rip+break_frame], rax  # Restore break_frame
    push rbx                    # Put return back in place
    ret                         # Return to caller


# set_break_frame microfunction
# Receives char* head in RAX and char* num in RBX
# Overwrites RAX and RBX
# Returns to calling function
set_break_frame:
    mov [rip+break_target_head], rax # update break_target_head
    mov [rip+break_target_num], rbx # update break_target_num
    mov rbx, [rip+function]     # Using function
    mov rax, [rbx+8]            # function->LOCALS
    mov [rip+break_frame], rax  # break_frame = function->LOCALS
    mov rax, [rbx+16]           # function->S
    mov [rip+break_target_func], rax # break_target_func = function->S
    ret                         # Return to sender


# process_do function
# Receives Nothing
# Returns Nothing
# Increments current_count and leverages save/restore_break_frame pieces
# Uses RCX for char* NUMBER_STRING
process_do:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    call save_break_frame       # Save the frame

    mov rax, [rip+current_count] # Using current count
    mov rbx, rax                # Preparing for update
    add rbx, 1                  # current_count + 1
    mov [rip+current_count], rbx # current_count = current_count + 1
    call numerate_number        # convert to string
    mov rcx, rax                # put NUMBER_STRING in place

    lea rax, [rip+process_do_string_0] # Using "DO_END_"
    mov rbx, rcx                # Passing NUMBER_STRING
    call set_break_frame        # Set the frame

    lea rax, [rip+process_do_string_1] # Using ":DO_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    call statement              # Do the DO {...} part

    lea rax, [rip+process_do_string_2] # Using "ERROR in process_do\nMISSING while\n"
    lea rbx, [rip+while_string] # Using "while"
    call require_match          # Make sure we have it

    lea rax, [rip+process_do_string_3] # Using "ERROR in process_do\nMISSING (\n"
    lea rbx, [rip+open_paren]   # Using "("
    call require_match          # Make sure we have it

    call expression             # Do the WHILE (...) part

    lea rax, [rip+process_do_string_4] # Using "ERROR in process_do\nMISSING )\n"
    lea rbx, [rip+close_paren]  # Using ")"
    call require_match          # Make sure we have it

    lea rax, [rip+process_do_string_5] # Using "ERROR in process_do\nMISSING ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it

    lea rax, [rip+process_do_string_6] # Using "TEST\nJUMP_NE %DO_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_do_string_7] # Using ":DO_END_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    call restore_break_frame    # Restore the old break frame

    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

process_do_string_0: .asciz "DO_END_"
process_do_string_1: .asciz ":DO_"
process_do_string_2: .asciz "ERROR in process_do\nMISSING while\n"
process_do_string_3: .asciz "ERROR in process_do\nMISSING (\n"
process_do_string_4: .asciz "ERROR in process_do\nMISSING )\n"
process_do_string_5: .asciz "ERROR in process_do\nMISSING ;\n"
process_do_string_6: .asciz "TEST\nJUMP_NE %DO_"
process_do_string_7: .asciz ":DO_END_"


# process_while function
# Receives nothing
# Returns nothing
# Increments current_count and leverages save/restore_break_frame pieces
# Uses RCX for char* NUMBER_STRING
process_while:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    call save_break_frame       # Save break_frame

    mov rax, [rip+current_count] # Using current count
    mov rbx, rax                # Preparing for update
    add rbx, 1                  # current_count + 1
    mov [rip+current_count], rbx # current_count = current_count + 1
    call numerate_number        # convert to string
    mov rcx, rax                # put NUMBER_STRING in place

    lea rax, [rip+process_while_string_0] # Using "END_WHILE_"
    mov rbx, rcx                # Passing NUMBER_STRING
    call set_break_frame        # Set it and forget it

    lea rax, [rip+process_while_string_1] # Using ":WHILE_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    lea rax, [rip+process_while_string_2] # Using "ERROR in process_while\nMISSING (\n"
    lea rbx, [rip+open_paren]         # Using "("
    call require_match          # Make sure we have it

    call expression             # Deal with the WHILE (...) part

    lea rax, [rip+process_while_string_3] # Using "TEST\nJUMP_EQ %END_WHILE_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_while_string_4] # Using "# THEN_while_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_while_string_5] # Using "ERROR in process_while\nMISSING )\n"
    lea rbx, [rip+close_paren]  # Using ")"
    call require_match          # Make sure we have it

    call statement              # Deal with the {....} part

    lea rax, [rip+process_while_string_6] # Using "JUMP %WHILE_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_while_string_7] # Using ":END_WHILE_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    call restore_break_frame    # Restore the old break frame

    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

process_while_string_0: .asciz "END_WHILE_"
process_while_string_1: .asciz ":WHILE_"
process_while_string_2: .asciz "ERROR in process_while\nMISSING (\n"
process_while_string_3: .asciz "TEST\nJUMP_EQ %END_WHILE_"
process_while_string_4: .asciz "# THEN_while_"
process_while_string_5: .asciz "ERROR in process_while\nMISSING )\n"
process_while_string_6: .asciz "JUMP %WHILE_"
process_while_string_7: .asciz ":END_WHILE_"


# process_for function
# Receives Nothing
# Returns Nothing
# Increments current_count and leverages save/restore_break_frame pieces
# Uses RCX for char* NUMBER_STRING
process_for:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    call save_break_frame       # Save the frame

    mov rax, [rip+current_count] # Using current count
    mov rbx, rax                # Preparing for update
    add rbx, 1                  # current_count + 1
    mov [rip+current_count], rbx # current_count = current_count + 1
    call numerate_number        # convert to string
    mov rcx, rax                # put NUMBER_STRING in place

    lea rax, [rip+process_for_string_0] # Using "FOR_END_"
    mov rbx, rcx                # Passing NUMBER_STRING
    call set_break_frame        # Set it and forget it

    lea rax, [rip+process_for_string_1] # Using "# FOR_initialization_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    lea rax, [rip+process_for_string_2] # Using "ERROR in process_for\nMISSING (\n"
    lea rbx, [rip+open_paren]   # Using "("
    call require_match          # Make Sure we have it

    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+semicolon]    # Using ";"
    call match                  # IF global_token->S == ";"
    cmp rax, 0                  # Then no initializer
    je process_for_terminator   # And skip getting the expression

    # Deal with FOR (...; case
    call expression             # Get the FOR ( ... ; part

process_for_terminator:
    lea rax, [rip+process_for_string_3] # Using ":FOR_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_for_string_4] # Using "ERROR in process_for\nMISSING ;1\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it

    call expression             # Get the FOR ( ; ... ; Part

    lea rax, [rip+process_for_string_5] # Using "TEST\nJUMP_EQ %FOR_END_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_for_string_6] # Using "JUMP %FOR_THEN_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_for_string_7] # Using ":FOR_ITER_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_for_string_8] # Using "ERROR in process_for\nMISSING ;2\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it

    call expression             # Get the FOR (;;...) part

    lea rax, [rip+process_for_string_9] # Using "JUMP %FOR_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_for_string_10] # Using ":FOR_THEN_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_for_string_11] # Using "ERROR in process_for\nMISSING )\n"
    lea rbx, [rip+close_paren]  # Using ")"
    call require_match          # Make sure we have it

    call statement              # Get FOR (;;) {...} part

    lea rax, [rip+process_for_string_12] # Using "JUMP %FOR_ITER_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Passing NUMBER_STRING
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    lea rax, [rip+process_for_string_13] # Using ":FOR_END_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    call uniqueID_out           # uniqueID_out(function->s, number_string)

    call restore_break_frame    # Restore the old break frame

    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

process_for_string_0: .asciz "FOR_END_"
process_for_string_1: .asciz "# FOR_initialization_"
process_for_string_2: .asciz "ERROR in process_for\nMISSING (\n"
process_for_string_3: .asciz ":FOR_"
process_for_string_4: .asciz "ERROR in process_for\nMISSING ;1\n"
process_for_string_5: .asciz "TEST\nJUMP_EQ %FOR_END_"
process_for_string_6: .asciz "JUMP %FOR_THEN_"
process_for_string_7: .asciz ":FOR_ITER_"
process_for_string_8: .asciz "ERROR in process_for\nMISSING ;2\n"
process_for_string_9: .asciz "JUMP %FOR_"
process_for_string_10: .asciz ":FOR_THEN_"
process_for_string_11: .asciz "ERROR in process_for\nMISSING )\n"
process_for_string_12: .asciz "JUMP %FOR_ITER_"
process_for_string_13: .asciz ":FOR_END_"


# process_break function
# Receives nothing
# Returns nothing
# Handles the break out of loops case
# Uses RBX for struct token_list* break_frame and RCX for struct token_list* I
process_break:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rax, [rip+break_target_head] # Catch big error
    cmp rax, 0                  # IF(NULL == break_target_head)
    je process_break_bad        # I'm sorry Mr White but you have stage-3 lung cancer

    mov rax, [rip+function]     # Using function
    mov rcx, [rax+8]            # I = function->LOCALS
    mov rbx, [rip+break_frame]  # Put break_frame in the right spot
    lea rax, [rip+process_break_string_1] # Using "POP_RBX\t# break_cleanup_locals\n"

process_break_iter:
    cmp rcx, 0                  # IF (NULL == I)
    je process_break_cleaned    # We are done

    cmp rcx, rbx                # IF I != break_frame
    je process_break_cleaned    # We are done

    call emit_out               # Emit it
    mov rcx, [rcx]              # I = I->NEXT
    jmp process_break_iter      # Keep looping

process_break_cleaned:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    lea rax, [rip+process_break_string_2] # Using "JUMP %"
    call emit_out               # Emit it

    mov rax, [rip+break_target_head] # Get what we are in
    call emit_out               # Emit it

    mov rax, [rip+break_target_func] # Get what function we are in
    call emit_out               # Emit it

    lea rax, [rip+underline]    # Using "_"
    call emit_out               # Emit it

    mov rax, [rip+break_target_num] # Get dem digits
    call emit_out               # Emit it

    lea rax, [rip+process_break_string_3] # Using "\n"
    call emit_out               # Emit it

    lea rax, [rip+process_break_string_4] # Using "ERROR in break statement\nMissing ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it

    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


process_break_bad:
    # Breaking badly
    mov r14, 2                  # write to standard error
#    call line_error             # Write useful debug info
    mov rax, rcx                # put S in the right place
    call File_Print             # print it

    lea rax, [rip+process_break_string_0] # Ending string
    call File_Print             # print it
    jmp Exit_Failure            # Abort Hard

process_break_string_0: .asciz "Not inside of a loop or case statement"
process_break_string_1: .asciz "POP_RBX\t# break_cleanup_locals\n"
process_break_string_2: .asciz "JUMP %"
process_break_string_3: .byte 10, 0
process_break_string_4: .asciz "ERROR in break statement\nMissing ;\n"


# expression function
# Receives Nothing
# Returns Nothing
# Walks global_token and updates output_list
# Uses RAX and RBX for match and RCX for char* store
expression:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    call bitwise_expr           # Collect bitwise expressions

    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+equal]        # "="
    call match                  # IF global_token->S == "="
    cmp rax, 0                  # We have to deal with assignment
    jne expression_done         # Looks like nope

    # Deal with possible assignment
    lea rcx, [rip+expression_string_1] # Assume "STORE_CHAR\n" by default
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+8]            # global_token->PREV
    mov rbx, [rbx+16]           # global_token->PREV->S
    lea rax, [rip+close_bracket] # Using "]"
    call match                  # IF global_token->S == "]"
    cmp rax, 0                  # Then we might have a char
    jne expression_int          # Otherwise INT

    mov rbx, [rip+current_target] # Using current_target
    mov rbx, [rbx+48]           # current_target->NAME
    lea rax, [rip+type_char_indirect_name] # Using "char*"
    call match                  # Intentional inefficiency because I feel like it
    cmp rax, 0                  # IF current_target->NAME == "char*"
    jne expression_int          # Do char anyway

    jmp expression_common       # Looks like we have to use "STORE_CHAR\n"

expression_int:
    lea rcx, [rip+expression_string_0] # Use "STORE_INTEGER\n"

expression_common:
    lea rax, [rip+expression]   # Passing expression
    call common_recursion       # Recurse
    mov rax, rcx                # Using Store
    call emit_out               # Emit it
    mov rax, 0                  # Using NULL
    mov [rip+current_target], rax # current_target = NULL

expression_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

expression_string_0: .asciz "STORE_INTEGER\n"
expression_string_1: .asciz "STORE_CHAR\n"


# bitwise_expr function
# Receives nothing
# Returns nothing
# Walks global_token list and updates output list
# Just calls other functions
bitwise_expr:
    call relational_expr        # Walk up the tree
    call bitwise_expr_stub      # Let general recursion do the work
    ret


# bitwise_expr_stub function
# Receives nothing
# Returns Nothing
# Just calls general_recursion a bunch
# Uses RAX, RBX, RCX and RDX for passing constants to general recursion
bitwise_expr_stub:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX

    lea rax, [rip+relational_expr] # Using relational_expr
    lea rbx, [rip+bitwise_expr_stub_string_0] # Using "AND_rax_rbx\n"
    lea rcx, [rip+bitwise_and]  # Using "&"
    lea rdx, [rip+bitwise_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+relational_expr] # Using relational_expr
    lea rbx, [rip+bitwise_expr_stub_string_0] # Using "AND_rax_rbx\n"
    lea rcx, [rip+logical_and]  # Using "&&"
    lea rdx, [rip+bitwise_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+relational_expr] # Using relational_expr
    lea rbx, [rip+bitwise_expr_stub_string_1] # Using "OR_rax_rbx\n"
    lea rcx, [rip+bitwise_or]   # Using "|"
    lea rdx, [rip+bitwise_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+relational_expr] # Using relational_expr
    lea rbx, [rip+bitwise_expr_stub_string_1] # Using "OR_rax_rbx\n"
    lea rcx, [rip+logical_or]   # Using "||"
    lea rdx, [rip+bitwise_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+relational_expr] # Using relational_expr
    lea rbx, [rip+bitwise_expr_stub_string_2] # Using "XOR_rbx_rax_into_rax\n"
    lea rcx, [rip+bitwise_xor]  # Using "^"
    lea rdx, [rip+bitwise_expr_stub] # And recurse
    call general_recursion      # Hit it

    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

bitwise_expr_stub_string_0: .asciz "AND_rax_rbx\n"
bitwise_expr_stub_string_1: .asciz "OR_rax_rbx\n"
bitwise_expr_stub_string_2: .asciz "XOR_rbx_rax_into_rax\n"


# relational_expr function
# Receives nothing
# Returns Nothing
# Walks global_token list and updates output list
# just calls other function
relational_expr:
    call additive_expr          # Walk up the tree
    call relational_expr_stub   # Recurse
    ret


# relational_expr_stub function
# Receives nothing
# Returns Nothing
# Just calls general_recursion a bunch
# Uses RAX, RBX, RCX and RDX for passing constants to general recursion
relational_expr_stub:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX

    lea rax, [rip+additive_expr] # Using additive_expr
    lea rbx, [rip+relational_expr_stub_string_0] # Using "CMP\nSETL\nMOVEZX\n"
    lea rcx, [rip+less_than_string] # Using "<"
    lea rdx, [rip+relational_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+additive_expr] # Using additive_expr
    lea rbx, [rip+relational_expr_stub_string_1] # Using "CMP\nSETLE\nMOVEZX\n"
    lea rcx, [rip+less_than_equal_string] # Using "<="
    lea rdx, [rip+relational_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+additive_expr] # Using additive_expr
    lea rbx, [rip+relational_expr_stub_string_2] # Using "CMP\nSETGE\nMOVEZX\n"
    lea rcx, [rip+greater_than_equal_string] # Using ">="
    lea rdx, [rip+relational_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+additive_expr] # Using additive_expr
    lea rbx, [rip+relational_expr_stub_string_3] # Using "CMP\nSETG\nMOVEZX\n"
    lea rcx, [rip+greater_than_string] # Using ">"
    lea rdx, [rip+relational_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+additive_expr] # Using additive_expr
    lea rbx, [rip+relational_expr_stub_string_4] # Using "CMP\nSETE\nMOVEZX\n"
    lea rcx, [rip+equal_to_string] # Using "=="
    lea rdx, [rip+relational_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+additive_expr] # Using additive_expr
    lea rbx, [rip+relational_expr_stub_string_5] # Using "CMP\nSETNE\nMOVEZX\n"
    lea rcx, [rip+not_equal_string] # Using "!="
    lea rdx, [rip+relational_expr_stub] # And recurse
    call general_recursion      # Hit it

    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

relational_expr_stub_string_0: .asciz "CMP\nSETL\nMOVEZX\n"
relational_expr_stub_string_1: .asciz "CMP\nSETLE\nMOVEZX\n"
relational_expr_stub_string_2: .asciz "CMP\nSETGE\nMOVEZX\n"
relational_expr_stub_string_3: .asciz "CMP\nSETG\nMOVEZX\n"
relational_expr_stub_string_4: .asciz "CMP\nSETE\nMOVEZX\n"
relational_expr_stub_string_5: .asciz "CMP\nSETNE\nMOVEZX\n"


# additive_expr function
# Receives nothing
# Returns Nothing
# Walks global_token list and updates output list
# just calls other function
additive_expr:
    call postfix_expr           # Walk up the tree
    call additive_expr_stub     # Recurse
    ret


# additive_expr_stub function
# Receives nothing
# Returns Nothing
# Just calls general_recursion a bunch
# Uses RAX, RBX, RCX and RDX for passing constants to general recursion
additive_expr_stub:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX

    lea rax, [rip+postfix_expr] # Using postfix_expr
    lea rbx, [rip+additive_expr_stub_string_0] # Using "ADD_rbx_to_rax\n"
    lea rcx, [rip+plus_string]  # Using "+"
    lea rdx, [rip+additive_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+postfix_expr] # Using postfix_expr
    lea rbx, [rip+additive_expr_stub_string_1] # Using "SUBTRACT_rax_from_rbx_into_rbx\nMOVE_rbx_to_rax\n"
    lea rcx, [rip+minus_string] # Using "-"
    lea rdx, [rip+additive_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+postfix_expr] # Using postfix_expr
    lea rbx, [rip+additive_expr_stub_string_2] # Using "MULTIPLY_rax_by_rbx_into_rax\n"
    lea rcx, [rip+multiply_string] # Using "*"
    lea rdx, [rip+additive_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+postfix_expr] # Using postfix_expr
    lea rbx, [rip+additive_expr_stub_string_3] # Using "XCHG_rax_rbx\nLOAD_IMMEDIATE_rdx %0\nDIVIDE_rax_by_rbx_into_rax\n"
    lea rcx, [rip+divide_string] # Using "/"
    lea rdx, [rip+additive_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+postfix_expr] # Using postfix_expr
    lea rbx, [rip+additive_expr_stub_string_4] # Using "XCHG_rax_rbx\nLOAD_IMMEDIATE_rdx %0\nMODULUS_rax_from_rbx_into_rbx\nMOVE_rdx_to_rax\n"
    lea rcx, [rip+modulus_string] # Using "%"
    lea rdx, [rip+additive_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+postfix_expr] # Using postfix_expr
    lea rbx, [rip+additive_expr_stub_string_5] # Using "COPY_rax_to_rcx\nCOPY_rbx_to_rax\nSAL_rax_cl\n"
    lea rcx, [rip+left_shift_string] # Using "<<"
    lea rdx, [rip+additive_expr_stub] # And recurse
    call general_recursion      # Hit it

    lea rax, [rip+postfix_expr] # Using postfix_expr
    lea rbx, [rip+additive_expr_stub_string_6] # Using "COPY_rax_to_rcx\nCOPY_rbx_to_rax\nSAR_rax_cl\n"
    lea rcx, [rip+right_shift_string] # Using ">>"
    lea rdx, [rip+additive_expr_stub] # And recurse
    call general_recursion      # Hit it

    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

additive_expr_stub_string_0: .asciz "ADD_rbx_to_rax\n"
additive_expr_stub_string_1: .asciz "SUBTRACT_rax_from_rbx_into_rbx\nMOVE_rbx_to_rax\n"
additive_expr_stub_string_2: .asciz "MULTIPLY_rax_by_rbx_into_rax\n"
additive_expr_stub_string_3: .asciz "XCHG_rax_rbx\nLOAD_IMMEDIATE_rdx %0\nDIVIDE_rax_by_rbx_into_rax\n"
additive_expr_stub_string_4: .asciz "XCHG_rax_rbx\nLOAD_IMMEDIATE_rdx %0\nMODULUS_rax_from_rbx_into_rbx\nMOVE_rdx_to_rax\n"
additive_expr_stub_string_5: .asciz "COPY_rax_to_rcx\nCOPY_rbx_to_rax\nSAL_rax_cl\n"
additive_expr_stub_string_6: .asciz "COPY_rax_to_rcx\nCOPY_rbx_to_rax\nSAR_rax_cl\n"


# postfix_expr function
# Receives nothing
# Returns Nothing
# Walks global_token list and updates output list
# just calls other function
postfix_expr:
    call primary_expr           # Walk up the tree
    call postfix_expr_stub      # Recurse
    ret


# postfix_expr_stub function
# Receives nothing
# Returns Nothing
# Checks for "[" and "->" and deals with them otherwise does nothing
# Uses RAX, RBX, RCX and RDX for passing constants to general recursion
postfix_expr_stub:
    push rbx                    # Protect RBX
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+open_bracket] # Using "["
    call match                  # IF global_token->S == "["
    cmp rax, 0                  # then we have an array
    jne postfix_expr_stub_arrow # Otherwise try arrow

    # Deal with array
    call postfix_expr_array     # Get it
    call postfix_expr_stub      # Recurse

postfix_expr_stub_arrow:
    lea rax, [rip+arrow_string] # Using "->"
    call match                  # IF global_token->S == "->"
    cmp rax, 0                  # Then we need to deal with struct offsets
    jne postfix_expr_stub_done  # Otherwise be done

    # Deal with arrow
    call postfix_expr_arrow     # Get it
    call postfix_expr_stub      # Recurse

postfix_expr_stub_done:
    pop rbx                     # Restore RBX
    ret


# unary_expr_sizeof function
# Receives nothing
# Returns nothing
# Uses RCX for A->SIZE
unary_expr_sizeof:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    lea rax, [rip+unary_expr_sizeof_string_0] # Using "ERROR in unary_expr\nMissing (\n"
    lea rbx, [rip+open_paren]   # Using "("
    call require_match          # Make sure we have it

    call type_name              # Get the type
    mov rcx, [rax+8]            # Set A->TYPE

    lea rax, [rip+unary_expr_sizeof_string_1] # Using "ERROR in unary_expr\nMissing )\n"
    lea rbx, [rip+close_paren]  # Using ")"
    call require_match          # Make sure we have it

    lea rax, [rip+unary_expr_sizeof_string_2] # Using "LOAD_IMMEDIATE_rax %"
    call emit_out               # Emit it

    mov rax, rcx                # Put A->SIZE in the right place
    call numerate_number        # Turn into string
    call emit_out               # Emit it

    lea rax, [rip+unary_expr_sizeof_string_3] # Using "\n"
    call emit_out               # Emit it

    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

unary_expr_sizeof_string_0: .asciz "ERROR in unary_expr\nMissing (\n"
unary_expr_sizeof_string_1: .asciz "ERROR in unary_expr\nMissing )\n"
unary_expr_sizeof_string_2: .asciz "LOAD_IMMEDIATE_rax %"
unary_expr_sizeof_string_3: .byte 10, 0


# postfix_expr_array function
# Receives Nothing
# Returns Nothing
# Uses RBX for struct type* ARRAY and RCX for char* ASSIGN
postfix_expr_array:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rax, [rip+current_target] # ARRAY = current_target
    push rax                    # Protect it

    lea rax, [rip+expression]   # Using expression
    call common_recursion       # Recurse

    pop rbx                     # Restore array
    mov [rip+current_target], rbx # current_target = ARRAY

    lea rcx, [rip+postfix_expr_array_string_0] # ASSIGN = "LOAD_INTEGER\n"

    lea rax, [rip+type_char_indirect_name] # Using "char*"
    mov rbx, [rbx+48]           # current_target->NAME
    call match                  # IF current_target->NAME == "char*"
    cmp rax, 0                  # load a byte
    jne postfix_expr_array_large # Otherwise adjust

    # Deal with loading byte
    lea rcx, [rip+postfix_expr_array_string_1] # ASSIGN = "LOAD_BYTE\n"
    jmp postfix_expr_array_common # Do the next bit

postfix_expr_array_large:
    # deal with arrays made of things other than chars
    lea rax, [rip+postfix_expr_array_string_2] # Using "SAL_rax_Immediate8 !"
    call emit_out               # Emit it

    mov rax, [rip+current_target] # Using current_target
    mov rax, [rax+24]           # current_target->INDIRECT
    mov rax, [rax+8]            # current_target->INDIRECT->SIZE
    call ceil_log2              # ceil_log2(current_target->indirect->size)
    call numerate_number        # numerate_number(ceil_log2(current_target->indirect->size))
    call emit_out               # Emit it

    lea rax, [rip+postfix_expr_array_string_3] # Using "\n"
    call emit_out               # Emit it

postfix_expr_array_common:
    lea rax, [rip+postfix_expr_array_string_4] # Using "ADD_rbx_to_rax\n"
    call emit_out               # Emit it

    lea rax, [rip+postfix_expr_array_string_5] # Using "ERROR in postfix_expr\nMissing ]\n"
    lea rbx, [rip+close_bracket] # Using "]"
    call require_match          # Make sure we have it

    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+equal]        # Using "="
    call match                  # IF global_token->S == "="
    cmp rax, 0                  # We need to preserve address
    jne postfix_expr_array_done # Otherwise be done

    # Clearing out assign
    lea rcx, [rip+postfix_expr_array_string_6] # ASSIGN = ""

postfix_expr_array_done:
    mov rax, rcx                # Using ASSIGN
    call emit_out               # Emit it

    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

postfix_expr_array_string_0: .asciz "LOAD_INTEGER\n"
postfix_expr_array_string_1: .asciz "LOAD_BYTE\n"
postfix_expr_array_string_2: .asciz "SAL_rax_Immediate8 !"
postfix_expr_array_string_3: .byte 10, 0
postfix_expr_array_string_4: .asciz "ADD_rbx_to_rax\n"
postfix_expr_array_string_5: .asciz "ERROR in postfix_expr\nMissing ]\n"
postfix_expr_array_string_6: .byte 0


# ceil_log2 function
# Receives int a in RAX
# Performs log2 on A and
# Returns result in RAX
# Uses RBX for INT A and RCX for INT RESULT
ceil_log2:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rcx, 0                  # RESULT = 0

    mov rbx, rax                # put A in right place
    sub rax, 1                  # (A - 1)
    and rax, rbx                # A & (A - 1)
    cmp rax, 0                  # IF 0 == (A & (A - 1))
    jne ceil_log2_iter          # Starting from -1

    mov rcx, -1                 # RESULT = -1

ceil_log2_iter:
    cmp rbx, 0                  # IF A > 0
    jle ceil_log2_done          # Otherwise be done

    add rcx, 1                  # RESULT = RESULT + 1
    shr rbx, 1                  # A = A >> 1
    jmp ceil_log2_iter          # Keep looping

ceil_log2_done:
    mov rax, rcx                # Return RESULT
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# postfix_expr_arrow function
# Receives nothing
# Returns nothing
# Emits a bunch and updates current_target
# Uses RBX for struct type* I
postfix_expr_arrow:
    push rbx                    # Protect RBX
    lea rax, [rip+postfix_expr_arrow_string_0] # Using "# looking up offset\n"
    call emit_out               # Emit it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    mov rbx, [rax+16]           # Using global_token->S
    mov rax, [rip+current_target] # Using current_target
    call lookup_member          # lookup_member(current_target, global_token->s)
    mov rbx, rax                # struct type* I = lookup_member(current_target, global_token->s)

    mov rax, [rax+40]           # I->TYPE
    mov [rip+current_target], rax # current_target = I->TYPE

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    mov rax, [rbx+16]           # I->OFFSET
    cmp rax, 0                  # IF 0 != I->OFFSET
    je postfix_expr_arrow_first # Then we don't need to do an offset

    # Deal with needing an offset
    lea rax, [rip+postfix_expr_arrow_string_1] # Using "# -> offset calculation\nLOAD_IMMEDIATE_rbx %"
    call emit_out               # Emit it

    mov rax, [rbx+16]           # I->OFFSET
    call numerate_number        # Convert to string
    call emit_out               # Emit it

    lea rax, [rip+postfix_expr_arrow_string_2] # Using "\nADD_rbx_to_rax\n"
    call emit_out               # Emit it

postfix_expr_arrow_first:
    mov rax, [rbx+8]            # I->SIZE
    cmp rax, 4                  # IF I->SIZE >= 4
    jl postfix_expr_arrow_done  # Otherwise be done

    # Last chance for load
    mov rax, [rip+global_token] # Using global_token
    mov rbx, [rax+16]           # global_token->S
    lea rax, [rip+equal]        # Using "="
    call match                  # IF global_token->S == "="
    cmp rax, 0                  # Then we have assignment and should not load
    je postfix_expr_arrow_done  # Be done

    # Deal with load case
    lea rax, [rip+postfix_expr_arrow_string_3] # Using "LOAD_INTEGER\n"
    call emit_out               # Emit it

postfix_expr_arrow_done:
    pop rbx                     # Restore RBX
    ret

postfix_expr_arrow_string_0: .asciz "# looking up offset\n"
postfix_expr_arrow_string_1: .asciz "# -> offset calculation\nLOAD_IMMEDIATE_rbx %"
postfix_expr_arrow_string_2: .asciz "\nADD_rbx_to_rax\n"
postfix_expr_arrow_string_3: .asciz "LOAD_INTEGER\n"


# primary_expr function
# Receives nothing
# Returns nothing
primary_expr:
    push rbx                    # Protect RBX

    mov rax, [rip+global_token] # Using global_token
    mov rbx, [rax+16]           # global_token->S
    lea rax, [rip+sizeof_string] # Using "sizeof"
    call match                  # See if match
    cmp rax, 0                  # IF match
    jne primary_expr_neg        # Otherwise try negatives

    # Deal with sizeof
    call unary_expr_sizeof      # Lets do this
    jmp primary_expr_done       # Be done

primary_expr_neg:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    cmp rax, 45                 # IF global_token->S[0] == "-"
    jne primary_expr_not        # Otherwise try logical NOT

    # Deal with negative numbers
    lea rax, [rip+primary_expr_string_0] # Using "LOAD_IMMEDIATE_rax %0\n"
    call emit_out               # Emit it

    lea rax, [rip+postfix_expr] # Passing postfix_expr
    call common_recursion       # Get what it is notting

    lea rax, [rip+primary_expr_string_1] # Using "SUBTRACT_rax_from_rbx_into_rbx\nMOVE_rbx_to_rax\n"
    call emit_out               # Emit it
    jmp primary_expr_done       # Be done

primary_expr_not:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    cmp rax, 33                 # IF global_token->S[0] == "!"
    jne primary_expr_bin        # Otherwise try '~'

    # Deal with logical not
    lea rax, [rip+primary_expr_string_2] # Using "LOAD_IMMEDIATE_rax %1\n"
    call emit_out               # Emit it

    lea rax, [rip+postfix_expr] # Passing postfix_expr
    call common_recursion       # Get what it is notting

    lea rax, [rip+primary_expr_string_3] # Using "XOR_rbx_rax_into_rax\n"
    call emit_out               # Emit it
    jmp primary_expr_done       # Be done

primary_expr_bin:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    cmp rax, 126                # IF global_token->S[0] == "~"
    jne primary_expr_paren      # Otherwise try paren

    # Deal with binary NOT
    lea rax, [rip+postfix_expr] # Passing postfix_expr
    call common_recursion       # Get what it is notting
    lea rax, [rip+primary_expr_string_4] # Using "NOT_rax\n"
    call emit_out               # Emit it
    jmp primary_expr_done       # Be done

primary_expr_paren:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    cmp rax, 40                 # IF global_token->S[0] == "("
    jne primary_expr_ch         # Otherwise try char

    # deal with nesting
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT
    call expression             # Lets recurse
    lea rax, [rip+primary_expr_string_5] # Using "Error in Primary expression\nDidn't get )\n"
    lea rbx, [rip+close_paren]  # Using ")"
    call require_match          # Make sure we have it
    jmp primary_expr_done       # Be done

primary_expr_ch:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    cmp rax, 39                 # Using "'"
    jne primary_expr_str        # Otherwise try string

    # Deal with chars
    call primary_expr_char      # Handle that char
    jmp primary_expr_done       # Be done

primary_expr_str:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    cmp rax, 34                 # Using '"'
    jne primary_expr_var        # Otherwise try a variable

    # Deal with strings
    call primary_expr_string    # Handle that string
    jmp primary_expr_done       # Be done

primary_expr_var:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    lea rbx, [rip+primary_expr_string_6] # Using "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_"
    call In_Set                 # See if we have a match
    cmp rax, 1                  # IF match
    jne primary_expr_num        # otherwise try number

    # Deal with variables
    call primary_expr_variable  # Deal with variable
    jmp primary_expr_done       # Be done

primary_expr_num:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    lea rbx, [rip+primary_expr_string_7] # Using "0123456789"
    call In_Set                 # See if we have a match
    cmp rax, 1                  # IF match
    jne primary_expr_fail       # otherwise we failed hard

    # Deal with numbers
    call primary_expr_number    # Collect the number
    jmp primary_expr_done       # Be done

primary_expr_fail:
    # looks like we hit bad input
    # abort before it gets bad
    call primary_expr_failure   # No match means failure
primary_expr_done:
    pop rbx                     # Restore RBX
    ret

primary_expr_string_0: .asciz "LOAD_IMMEDIATE_rax %0\n"
primary_expr_string_1: .asciz "SUBTRACT_rax_from_rbx_into_rbx\nMOVE_rbx_to_rax\n"
primary_expr_string_2: .asciz "LOAD_IMMEDIATE_rax %1\n"
primary_expr_string_3: .asciz "XOR_rbx_rax_into_rax\n"
primary_expr_string_4: .asciz "NOT_rax\n"
primary_expr_string_5: .asciz "Error in Primary expression\nDidn't get )\n"
primary_expr_string_6: .asciz "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_"
primary_expr_string_7: .asciz "0123456789"


# primary_expr_variable function
# Receives nothing
# Returns nothing
# Walks global and updates output
# Uses RAX for struct token_list* a and RCX for char* S
primary_expr_variable:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rax, [rip+global_token] # Using global_token
    mov rcx, [rax+16]           # S = global_token->S
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    mov rax, rcx                # Using S
    mov rbx, [rip+global_constant_list] # Using global_constant_list
    call sym_lookup             # sym_lookup(s, global_constant_list)
    cmp rax, 0                  # IF NULL == sym_lookup(s, global_constant_list)
    je primary_expr_variable_local # Try locals next

    # Deal with constant load
    mov rbx, [rax+32]           # a->ARGS
    lea rax, [rip+primary_expr_variable_string_2] # Using "LOAD_IMMEDIATE_rax %"
    call emit_out               # Emit it

    mov rax, [rbx+16]           # a->ARGS->S
    call emit_out               # Emit it

    lea rax, [rip+primary_expr_variable_string_1] # Using "\n"
    call emit_out               # Emit it
    jmp primary_expr_variable_done # Be done

primary_expr_variable_local:
    mov rax, rcx                # Using S
    mov rbx, [rip+function]     # Using function
    mov rbx, [rbx+8]            # function->locals
    call sym_lookup             # sym_lookup(s, function->locals)
    cmp rax, 0                  # IF NULL == sym_lookup(s, function->locals)
    je primary_expr_variable_arguments # try arguments next

    # Deal with local load
    call variable_load          # Collect it
    jmp primary_expr_variable_done # Be done

primary_expr_variable_arguments:
    mov rax, rcx                # Using S
    mov rbx, [rip+function]     # Using function
    mov rbx, [rbx+32]           # function->args
    call sym_lookup             # sym_lookup(s, function->args)
    cmp rax, 0                  # IF NULL == sym_lookup(s, function->args)
    je primary_expr_variable_function # try functions next

    # Deal with argument load
    call variable_load          # Collect it
    jmp primary_expr_variable_done # Be done

primary_expr_variable_function:
    mov rax, rcx                # Using S
    mov rbx, [rip+global_function_list] # Using global_function_list
    call sym_lookup             # sym_lookup(s, global_function_list)
    cmp rax, 0                  # IF NULL == sym_lookup(s, global_function_list)
    je primary_expr_variable_global # try globals next

    # Deal with functions
    call function_load          # Deal with the function
    jmp primary_expr_variable_done # Be done

primary_expr_variable_global:
    mov rax, rcx                # Using S
    mov rbx, [rip+global_symbol_list] # Using global_symbol_list
    call sym_lookup             # sym_lookup(s, global_symbol_list)
    cmp rax, 0                  # IF NULL == sym_lookup(s, global_symbol_list)
    je primary_expr_variable_error # Give up

    # Deal with globals
    call global_load               # Collect that global
    jmp primary_expr_variable_done # Be done

primary_expr_variable_error:
    mov r14, 2                  # write to standard error
#    call line_error             # Write useful debug info
    mov rax, rcx                # put S in the right place
    call File_Print             # print it

    lea rax, [rip+primary_expr_variable_string_0] # Ending string
    call File_Print             # print it
    jmp Exit_Failure            # Abort Hard

primary_expr_variable_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

primary_expr_variable_string_0: .asciz " is not a defined symbol\n"
primary_expr_variable_string_1: .byte 10, 0
primary_expr_variable_string_2: .asciz "LOAD_IMMEDIATE_rax %"


# function_call function
# Receives char* S in RAX and int BOOL in RBX
# Builds stack frames before and tears them down after function calls
# Uses RCX for char* S, RDX for int BOOL, RSI for PASSED
function_call:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    push rsi                    # Protect RSI
    mov rcx, rax                # Put S in place
    mov rdx, rbx                # Put BOOL in place
    mov rsi, 0                  # PASSED = 0

    lea rax, [rip+function_call_string_0] # Using "ERROR in process_expression_list\nNo ( was found\n"
    lea rbx, [rip+open_paren]   # Using "("
    call require_match          # Make sure we have it

    lea rax, [rip+function_call_string_1] # Using "PUSH_RDI\t# Prevent overwriting in recursion\n"
    call emit_out               # Emit it

    lea rax, [rip+function_call_string_2] # Using "PUSH_RBP\t# Protect the old base pointer\n"
    call emit_out               # Emit it

    lea rax, [rip+function_call_string_3] # Using "COPY_RSP_to_RDI\t# Copy new base pointer\n"
    call emit_out               # Emit it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    cmp rax, 41                 # IF global_token->S[0] == ")"
    je function_call_gen_done   # Then no arguments to send

    # looks like we have arguments to collect
    call expression             # Collect the argument

    lea rax, [rip+function_call_string_4] # Using "PUSH_RAX\t#_process_expression1\n"
    call emit_out               # Emit it
    mov rsi, 1                  # PASSED = 1

function_call_gen_iter:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    cmp rax, 44                 # IF global_token->S[0] == ","
    jne function_call_gen_done  # Otherwise we are done

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    call expression             # Collect the argument

    lea rax, [rip+function_call_string_5] # Using "PUSH_RAX\t#_process_expression2\n"
    call emit_out               # Emit it
    add rsi, 1                  # PASSED = PASSED + 1
    jmp function_call_gen_iter  # Keep trying

function_call_gen_done:
    # All is collected
    lea rax, [rip+function_call_string_6] # Using "ERROR in process_expression_list\nNo ) was found\n"
    lea rbx, [rip+close_paren]  # Using ")"
    call require_match          # Make sure we have it

    cmp rdx, 0                  # IF(BOOL == TRUE)
    jne function_call_static    # Otherwise it is a static call

    # Deal with a passed function pointer
    lea rax, [rip+function_call_string_7] # Using "LOAD_BASE_ADDRESS_rax %"
    call emit_out               # Emit it

    mov rax, rcx                # Using S
    call emit_out               # Emit it

    lea rax, [rip+function_call_string_8] # Using "\nLOAD_INTEGER\n"
    call emit_out               # Emit it

    lea rax, [rip+function_call_string_9] # Using "COPY_rdi_to_rbp\n"
    call emit_out               # Emit it

    lea rax, [rip+function_call_string_10] # Using "CALL_rax\n"
    call emit_out               # Emit it

    lea rax, [rip+function_call_string_13] # Using "POP_RBX\t# _process_expression_locals\n"
    jmp function_call_cleanup   # Clean up

function_call_static:
    # Deal with fixed function name
    lea rax, [rip+function_call_string_9] # Using "COPY_rdi_to_rbp\n"
    call emit_out               # Emit it

    lea rax, [rip+function_call_string_11] # Using "CALL_IMMEDIATE %FUNCTION_"
    call emit_out               # Emit it

    mov rax, rcx                # Using S
    call emit_out               # Emit it

    lea rax, [rip+function_call_string_12] # Using "\n"
    call emit_out               # Emit it

    lea rax, [rip+function_call_string_13] # Using "POP_RBX\t# _process_expression_locals\n"

function_call_cleanup:
    cmp rsi, 0                  # IF PASSED > 0
    jle function_call_done      # Otherwise be done

    # The desired string is already in RAX
    call emit_out               # Emit it

    sub rsi, 1                  # PASSED = PASSED - 1
    jmp function_call_cleanup   # Keep going

function_call_done:
    lea rax, [rip+function_call_string_14] # Using "POP_rbp\t# Restore old base pointer\n"
    call emit_out               # Emit it

    lea rax, [rip+function_call_string_15] # Using "POP_rdi\t# Prevent overwrite\n"
    call emit_out               # Emit it

    pop rsi                     # Restore RSI
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

function_call_string_0: .asciz "ERROR in process_expression_list\nNo ( was found\n"
function_call_string_1: .asciz "PUSH_RDI\t# Prevent overwriting in recursion\n"
function_call_string_2: .asciz "PUSH_RBP\t# Protect the old base pointer\n"
function_call_string_3: .asciz "COPY_RSP_to_RDI\t# Copy new base pointer\n"
function_call_string_4: .asciz "PUSH_RAX\t#_process_expression1\n"
function_call_string_5: .asciz "PUSH_RAX\t#_process_expression2\n"
function_call_string_6: .asciz "ERROR in process_expression_list\nNo ) was found\n"
function_call_string_7: .asciz "LOAD_BASE_ADDRESS_rax %"
function_call_string_8: .asciz "\nLOAD_INTEGER\n"
function_call_string_9: .asciz "COPY_rdi_to_rbp\n"
function_call_string_10: .asciz "CALL_rax\n"
function_call_string_11: .asciz "CALL_IMMEDIATE %FUNCTION_"
function_call_string_12: .byte 10, 0
function_call_string_13: .asciz "POP_RBX\t# _process_expression_locals\n"
function_call_string_14: .asciz "POP_RBP\t# Restore old base pointer\n"
function_call_string_15: .asciz "POP_RDI\t# Prevent overwrite\n"


# variable_load function
# Receives struct token_list* A in RAX
# Returns nothing
# Updates output and current_target
# Uses RCX for A
variable_load:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rcx, rax                # Protect A

    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+open_paren]   # Using "("
    call match                  # IF global_token->S == "("
    cmp rax, 0                  # Then it might be a function
    jne variable_load_regular   # Otherwise it is regular

    mov rbx, [rcx+24]           # A->TYPE
    mov rbx, [rbx+48]           # A->TYPE->NAME
    lea rax, [rip+type_function_name] # Using "FUNCTION"
    call match                  # IF A->TYPE->NAME == "FUNCTION"
    cmp rax, 0                  # Then it must be a function
    jne variable_load_regular   # otherwise just another regular

    # deal with function
    mov rax, [rcx+32]           # A->DEPTH
    call numerate_number        # Convert to string
    mov rbx, 0                  # pass 0 for true
    call function_call          # Create the function call
    jmp variable_load_done      # Be done

variable_load_regular:
    mov rax, [rcx+24]           # A->TYPE
    mov [rip+current_target], rax # current_target = A->TYPE

    lea rax, [rip+variable_load_string_0] # Using "LOAD_BASE_ADDRESS_rax %"
    call emit_out               # Emit it

    mov rax, [rcx+32]           # A->DEPTH
    call numerate_number        # Convert to string
    call emit_out               # Emit it

    lea rax, [rip+variable_load_string_1] # Using "\n"
    call emit_out               # Emit it

    # Check for special case of assignment
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+equal]        # Using "="
    call match                  # IF global_token->S == "="
    cmp rax, 0                  # Then we skip loading
    je variable_load_done       # And be done

    # Deal with common case
    lea rax, [rip+variable_load_string_2] # Using "LOAD_INTEGER\n"
    call emit_out               # Emit it

variable_load_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

variable_load_string_0: .asciz "LOAD_BASE_ADDRESS_rax %"
variable_load_string_1: .byte 10, 0
variable_load_string_2: .asciz "LOAD_INTEGER\n"


# function_load function
# Receives struct token_list* a in RAX
# Returns nothing
# Uses RCX to hold A->S
function_load:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rax, [rax+16]           # A->S
    mov rcx, rax                # Protect A->S
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+open_paren]   # Using "("
    call match                  # IF global_token->S == "("
    cmp rax, 0                  # The we need to do a function call
    jne function_load_regular   # Otherwise just load it's address

    # Deal with function call
    mov rax, rcx                # Using A->S
    mov rbx, 1                  # Using FALSE
    call function_call          # Deal with it
    jmp function_load_done      # Be done

function_load_regular:
    lea rax, [rip+function_load_string_0] # Using "LOAD_IMMEDIATE_rax &FUNCTION_"
    call emit_out               # Emit it

    mov rax, rcx                # Using A->S
    call emit_out               # Emit it

    lea rax, [rip+function_load_string_1] # Using "\n"
    call emit_out               # Emit it

function_load_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

function_load_string_0: .asciz "LOAD_IMMEDIATE_rax &FUNCTION_"
function_load_string_1: .byte 10, 0


# global_load function
# Receives struct token_list* A in RAX
# Returns nothing
# Uses RBX to hold A->S
global_load:
    push rbx                    # Protect RBX
    mov rbx, rax                # Set as A
    mov rbx, [rbx+16]           # Set as A->S

    mov rax, [rax+24]           # A->TYPE
    mov [rip+current_target], rax # current_target = A->TYPE

    lea rax, [rip+global_load_string_0] # Using "LOAD_IMMEDIATE_rax &GLOBAL_"
    call emit_out               # Emit it

    mov rax, rbx                # Using A->S
    call emit_out               # Emit it

    lea rax, [rip+global_load_string_1] # Using "\n"
    call emit_out               # Emit it

    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+equal]        # "="
    call match                  # IF global_token->S == "="
    cmp rax, 0                  # We need to skip for assignment
    je global_load_done         # and be done

    # Otherwise we are loading the contents
    lea rax, [rip+global_load_string_2] # Using "LOAD_INTEGER\n"
    call emit_out               # Emit it

global_load_done:
    pop rbx                     # Restore RBX
    ret

global_load_string_0: .asciz "LOAD_IMMEDIATE_rax &GLOBAL_"
global_load_string_1: .byte 10, 0
global_load_string_2: .asciz "LOAD_INTEGER\n"


# sym_lookup function
# Receives char* S in RAX and struct token_list* symbol_list in RBX
# Uses I->S in RAX, S in RBX and I in RCX
# Returns match or NULL
sym_lookup:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rcx, rbx                # I = symbol_list
    mov rbx, rax                # Put S in the right place
sym_lookup_iter:
    cmp rcx, 0                  # IF NULL == I
    je sym_lookup_done          # We failed to find match

    mov rax, [rcx+16]           # Using I->S
    call match                  # IF I->S == S
    cmp rax, 0                  # then be done
    je sym_lookup_done          # Failed

    mov rcx, [rcx]              # I = I->NEXT
    jmp sym_lookup_iter         # otherwise keep looping

sym_lookup_done:
    mov rax, rcx                # Return I
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# primary_expr_number function
# Receives nothing
# Returns nothing
# Simply uses current global token to update output and then steps to next global_token
primary_expr_number:
    lea rax, [rip+primary_expr_number_string_0] # Using "LOAD_IMMEDIATE_rax %"
    call emit_out               # Emit it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    call emit_out               # Emit it

    lea rax, [rip+primary_expr_number_string_1] # Using "\n"
    call emit_out               # Emit it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT
    ret

primary_expr_number_string_0: .asciz "LOAD_IMMEDIATE_rax %"
primary_expr_number_string_1: .byte 10, 0


# primary_expr_string function
# receives nothing
# Returns nothing
# creates entries for string and calls to generate string output
# uses RCX for char* number_string
primary_expr_string:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rbx, [rip+current_count] # Using current_count
    mov rax, rbx                # And putting it in the right place
    call numerate_number        # Get the string
    mov rcx, rax                # protect number_string

    add rbx, 1                  # current_count + 1
    mov [rip+current_count], rbx # current_count = current_count + 1

    lea rax, [rip+primary_expr_string_string_0] # Using "LOAD_IMMEDIATE_rax &STRING_"
    call emit_out               # Emit it

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    mov rbx, rcx                # Put number_string in the right place
    call uniqueID_out           # Make it unique

    # Generate the target
    lea rax, [rip+primary_expr_string_string_1] # Using ":STRING_"
    mov rbx, [rip+strings_list] # Using strings_list
    call emit                   # Emit it
    mov rbx, rax                # put new strings_list in place

    mov rax, [rip+function]     # Using function
    mov rax, [rax+16]           # function->S
    call uniqueID               # Make it unique
    mov rbx, rax                # put new strings_list in place

    # Parse the string
    mov rax, [rip+global_token] # Using global token
    mov rax, [rax+16]           # global_token->S
    call parse_string           # convert to useful form
    call emit                   # Emit it
    mov [rip+strings_list], rax # Update Strings _list

    mov rax, [rip+global_token] # Using global token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

primary_expr_string_string_0: .asciz "LOAD_IMMEDIATE_rax &STRING_"
primary_expr_string_string_1: .asciz ":STRING_"


# primary_expr_char function
# Receives nothing
# Returns nothing
# Updates output_list using global_token
primary_expr_char:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    lea rax, [rip+primary_expr_char_string_0] # Using "LOAD_IMMEDIATE_rax %"
    call emit_out               # Emit it

    mov rax, [rip+global_token] # Using global token
    mov rax, [rax+16]           # global_token->S
    add rax, 1                  # global_token->S + 1
    call escape_lookup          # Get the char
    call numerate_number        # Convert to string
    call emit_out               # emit it

    lea rax, [rip+primary_expr_char_string_1] # Using "\n"
    call emit_out               # Emit it

    mov rax, [rip+global_token] # Using global token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

primary_expr_char_string_0: .asciz "LOAD_IMMEDIATE_rax %"
primary_expr_char_string_1: .byte 10, 0


# primary_expr_failure function
# Receives nothing
# Does not return but aborts hard
# Complains about the bad input
primary_expr_failure:
#    call line_error             # Get line of issue
    mov r14, 2                  # write to standard error
    lea rax, [rip+primary_expr_failure_string_0] # Using "Received "
    call File_Print             # Print it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    call File_Print             # Print it

    lea rax, [rip+primary_expr_failure_string_1] # Using " in primary_expr\n"
    call File_Print             # Print it
    jmp Exit_Failure            # Abort Hard

primary_expr_failure_string_0: .asciz "Received "
primary_expr_failure_string_1: .asciz " in primary_expr\n"


# general_recursion function
# Receives FUNCTION F in RAX, char* S in RBX, char* name in RCX and FUNCTION iterate in RDX
# Returns nothing
# Uses RCX for char* S, RDX for FUNCTION iterate and RBP for FUNCTION F
# But generally recurses a shitload
general_recursion:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    push rbp                    # Protect RBP
    mov rbp, rax                # Protect F
    mov rax, rcx                # Put name in the right place
    mov rcx, rbx                # Protect S

    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    call match                  # IF match(name, global_token->s)
    cmp rax, 0                  # If true we do
    jne general_recursion_done  # Otherwise skip it

    # Deal with the recursion
    mov rax, rbp                # Put F in the right place
    call common_recursion       # Recurse

    mov rax, rcx                # Put S in the right place
    call emit_out               # Emit it

    mov rax, rdx                # Put iterate in the right place
    call rax                    # Down the rabbit hole

general_recursion_done:
    pop rbp                     # Restore RBP
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# promote_type function
# Receives struct type* a in RAX and struct type* b in RBX
# Returns the most recent type in RAX
# Uses RAX for struct type* I, RCX for struct type* A and RDX for struct type* B
promote_type:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    cmp rbx, 0                  # IF NULL == B
    je promote_type_done        # Just return A

    mov rcx, rax                # Put A in place
    mov rdx, rbx                # Put B in place
    mov rax, rbx                # IF NULL == A
    cmp rcx, 0                  # Then we just return B
    je promote_type_done        # Be done

    # Looks like we need to walk the list
    mov rcx, [rcx+48]           # A->NAME
    mov rdx, [rdx+48]           # B->NAME
    mov rax, [rip+global_types] # I = global_types
promote_type_iter:
    cmp rax, 0                  # IF NULL == I
    je promote_type_done        # Just be done

    mov rbx, [rax+48]           # I->NAME
    cmp rbx, rcx                # IF(A->NAME == I->NAME)
    je promote_type_done        # Be done

    cmp rbx, rdx                # IF(B->NAME == I->NAME)
    je promote_type_done        # Be done

    mov rbx, [rax+24]           # I->INDIRECT
    mov rbx, [rbx+48]           # I->INDIRECT->NAME

    cmp rbx, rcx                # IF(A->NAME == I->INDIRECT->NAME)
    je promote_type_done        # Be done

    cmp rbx, rdx                # IF(B->NAME == I->INDIRECT->NAME)
    je promote_type_done        # Be done

    mov rax, [rax]              # I = I->NEXT
    jmp promote_type_iter       # Keep going

promote_type_done:
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# common_recursion function
# Receives FUNCTION F in RAX
# Returns Nothing
# Walks global_token list and update output_list
# Updates current_target
# Uses RBX to hold FUNCTION F and struct type* last_type
common_recursion:
    push rbx                    # Protect RBX
    mov rbx, rax                # Put FUNCTION F safely out of the way
    lea rax, [rip+common_recursion_string_0] # Using "PUSH_RAX\t#_common_recursion\n"
    call emit_out               # Emit it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    mov rax, rbx                # Prepare for function call
    mov rbx, [rip+current_target] # Get last type
    call rax                    # F();
    mov rax, [rip+current_target] # Get current_target
    call promote_type           # get the right type
    mov [rip+current_target], rax # Set new current_target

    lea rax, [rip+common_recursion_string_1] # Using "POP_RBX\t# _common_recursion\n"
    call emit_out               # Emit it
    pop rbx                     # Restore RBX
    ret

common_recursion_string_0: .asciz "PUSH_RAX\t#_common_recursion\n"
common_recursion_string_1: .asciz "POP_RBX\t# _common_recursion\n"


# require_match function
# Receives char* message in RAX and char* required in RBX
# Returns nothing
# Uses RCX to hold message and updates global_token
require_match:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rcx, rax                # put the message somewhere safe
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    call match                  # IF required == global_token->S
    cmp rax, 0                  # we are fine
    je require_match_good       # otherwise pain

    # Deal with bad times
#    call line_error             # Tell user what went wrong
    mov r14, 2                  # write to standard error
    mov rax, rcx                # using our message
    call File_Print             # Print it
    jmp Exit_Failure            # Abort HARD

require_match_good:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->next
    mov [rip+global_token], rax # global_token = global_token->next
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# uniqueID Function
# Receives char *S in RAX, struct token_list* l in RBX and char* num in RCX
# Returns updated struct token_list* L in RAX
uniqueID:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    call emit                   # emit(s, l)
    mov rbx, rax                # Put L in correct place
    lea rax, [rip+underline]    # Using "_"
    call emit                   # emit("_", l)
    mov rbx, rax                # Put L in correct place
    mov rax, rcx                # Put num in correct place
    call emit                   # emit(num, l)
    mov rbx, rax                # Put L in correct place
    lea rax, [rip+uniqueID_string_0] # Using "\n"
    call emit                   # emit("\n", l)
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

uniqueID_string_0: .byte 10, 0


# uniqueID_out function
# Receives char* S in RAX and char* num in RBX
# Returns nothing
uniqueID_out:
    push rax                    # Protect RAX
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rcx, rbx                # Put num in right spot
    mov rbx, [rip+output_list]  # Using output_list
    call uniqueID               # Get updated list
    mov [rip+output_list], rax  # output_list = uniqueID(s, output_list, num)
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    pop rax                     # Restore RAX
    ret


# emit_out function
# Receives char* S in RAX
# Returns nothing
# Updates output_list
# MUST NOT ALTER REGISTERS
emit_out:
    push rax                    # Protect RAX
    push rbx                    # Protect RBX
    mov rbx, [rip+output_list]  # Using output_list
    call emit                   # emit it
    mov [rip+output_list], rax  # update it
    pop rbx                     # Restore RBX
    pop rax                     # Restore RAX
    ret


# emit function
# Receives char *s in RAX and struct token_list* head in RBX
# Returns struct token_list* T in RAX
emit:
    push rcx                    # Protect RCX
    mov rcx, rax                # put S out of the way
    mov rax, 40                 # sizeof(struct token_list)
    call malloc                 # get T
    mov [rax], rbx              # t->next = head;
    mov [rax+16], rcx           # t->s = s;
    pop rcx                     # Restore RCX
    ret


# escape_lookup function
# Receives char* c in RAX
# Returns integer value of char in RAX
# Aborts hard if unknown escape is received
# Uses RCX to hold char* C
escape_lookup:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rcx, rax                # Put char* C in safe place
    mov al, [rcx]               # Load c[0]
    movzx rax, al               # make it useful
    cmp rax, 92                 # If '\\' != c[0]
    jne escape_lookup_done      # Be done

    mov rbx, rcx                # Prepare for walk
    add rbx, 1                  # increment
    mov bl, [rbx]               # load c[1]
    movzx rbx, bl               # make it useful

    cmp rbx, 120                # Check if \x??
    je escape_lookup_hex        # Deal with hex

    # Deal with \? escapes
    mov rax, 10                 # Guess "\n"
    cmp rbx, 110                # If n
    je escape_lookup_done       # Be done

    mov rax, 9                  # Guess "\t"
    cmp rbx, 116                # If t
    je escape_lookup_done       # Be done

    mov rax, rbx                # "\\", "'" and '"' all encode as themselves
    cmp rbx, 92                 # If "\\"
    je escape_lookup_done       # Be done
    cmp rbx, 39                 # IF "'"
    je escape_lookup_done       # Be done
    cmp rbx, 34                 # IF '"'
    je escape_lookup_done       # Be done

    mov rax, 13                 # Guess "\r"
    cmp rbx, 114                # IF r
    je escape_lookup_done       # Be done

    # Looks like we have no clue what we are doing
    # Aborting hard
    mov r14, 2                  # write to standard error
    lea rax, [rip+escape_lookup_string_0] # Using "Unknown escape received: "
    call File_Print             # Print it
    mov rax, rcx                # Using C
    call File_Print             # Print it
    lea rax, [rip+escape_lookup_string_1] # Using " Unable to process\n"
    call File_Print             # Print it
    jmp Exit_Failure            # Abort Hard

escape_lookup_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

escape_lookup_hex:
    # Give up on C and just assume they know what they are doing
    add rcx, 2                  # increment
    mov al, [rcx]               # c[2]
    movzx rax, al               # make it useful
    add rcx, 1                  # increment
    call char2hex               # Get the hex value
    sal rax, 4                  # c << 4
    mov bl, [rcx]               # c[3]
    movzx rbx, bl               # make it useful
    xchg rax, rbx               # protect c << 4
    call char2hex               # Get the hex value
    add rax, rbx                # hex(c[2]) << 4 + hex(c[3])
    jmp escape_lookup_done      # Be done

escape_lookup_string_0: .asciz "Unknown escape received: "
escape_lookup_string_1: .asciz " Unable to process\n"


# char2hex function
# Receives char in RAX
# Returns hex or aborts hard
char2hex:
    sub rax, 48                 # Try 0-9
    cmp rax, 10                 # Otherwise fun times
    jl char2hex_done            # Be done

    # Deal with A-F
    and rax, 0xDF               # Unset High bit turning a-f into A-F
    sub rax, 7                  # Shift down into position
    cmp rax, 10                 # Everything below A is bad
    jl char2hex_fail            # And should fail
    cmp rax, 16                 # Make sure we are below F
    jl char2hex_done            # If so be done

char2hex_fail:
    # Time to fail hard
    mov r14, 2                  # write to standard error
    lea rax, [rip+char2hex_string_0] # Using "Tried to print non-hex number\n"
    call File_Print             # Print it
    jmp Exit_Failure            # Abort Hard

char2hex_done:
    ret

char2hex_string_0: .asciz "Tried to print non-hex number\n"


# parse_string function
# Receives char* string in RAX
# Returns cleaned up string
# Protects char* string in RBX
parse_string:
    push rbx                    # Protect RBX
    mov rbx, rax                # Protect char* string
    call weird                  # Determine if we have a weird string
    cmp rax, 0                  # If weird
    je parse_string_weird       # Deal with it

    # Dealing with regular string
    mov rax, rbx                # Passing Char* string
    call collect_regular_string # Collect it
    jmp parse_string_done       # Be done

parse_string_weird:
    mov rax, rbx                # Passing Char* string
    call collect_weird_string   # Collect it

parse_string_done:
    pop rbx                     # Restore RBX
    ret


# weird function
# Receives char* string in RAX
# Returns true(0) or false(1) in RAX
# Uses RCX to hold char* string
weird:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rcx, rax                # Place string in safe place
    add rcx, 1                  # increment past the "
weird_reset:
    mov al, [rcx]               # Load a char
    movzx rax, al               # Make it useful
    cmp rax, 0                  # IF NULL == C
    je weird_false              # Nothing weird found

    cmp rax, 92                 # IF '\\'
    jne weird_escaped           # Deal with escaping

    # Deal with escape
    mov rax, rcx                # We are passing the string
    call escape_lookup          # to look it up

    add rcx, 1                  # string = string + 1
    mov bl, [rcx]               # get string[1]
    movzx rbx, bl               # make it useful
    cmp rbx, 120                # IF 'x' == string[1]
    jne weird_escaped           # otherwise skip the gap

    add rcx, 2                  # string = string + 2

weird_escaped:
    push rax                    # Protect C in case we need it
    lea rbx, [rip+weird_string_0] # Use "\t\n !#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"
    call In_Set                 # To find if weird
    cmp rax, 1                  # IF TRUE
    pop rax                     # Restore C
    jne weird_true              # Then not weird

    add rcx, 1                  # string = string + 1

    # Last chance for weird
    lea rbx, [rip+weird_string_1] # Use "\t\n\r "
    call In_Set                 # Check for special case
    cmp rax, 1                  # IF TRUE
    jne weird_reset             # Otherwise not in the special case

    # Deal with possible special case
    mov al, [rcx]               # Load string[1]
    movzx rax, al               # Make it useful
    cmp rax, 58                 # IF string[1] == ":"
    je weird_true               # Then we hit the special case
    jmp weird_reset             # Keep trying

weird_done:
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

weird_true:
    mov rax, 0                  # Return true
    jmp weird_done              # Be done

weird_false:
    mov rax, 1                  # Return false
    jmp weird_done              # Be done

weird_string_0: .byte 9, 10
.asciz " !#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"
weird_string_1: .byte 9, 10, 13, 32, 0


# collect_regular_string function
# Receives char* string in RAX
# Malloc and creates new string to return in RAX
# Uses RCX for return string and RDX for passed string
collect_regular_string:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    mov rdx, rax                # Protect our passed string
    mov rax, 256                # We need 256bytes of storage
    call malloc                 # Get our new pointer
    mov rcx, rax                # put it in place
    push rax                    # protect until done
collect_regular_string_reset:
    mov al, [rdx]               # string[0]
    movzx rax, al               # Make it useful
    cmp rax, 0                  # See if we hit the end
    je collect_regular_string_done # And be done

    cmp rax, 92                 # IF string[0] == '\\'
    je collect_regular_string_escaped # Deal with that mess

    # deal with boring char
    mov [rcx], al               # hold_string[index] = string[0]
    add rcx, 1                  # Increment it
    add rdx, 1                  # Increment it
    jmp collect_regular_string_reset # And keep going

collect_regular_string_escaped:
    mov rax, rdx                # Using string
    call escape_lookup          # Get the char
    mov [rcx], al               # hold_string[index] = escape_lookup(string)
    add rdx, 1                  # Increment it
    add rcx, 1                  # Increment it
    mov al, [rdx]               # string[0]
    movzx rax, al               # Make it useful
    add rdx, 1                  # Increment it
    cmp rax, 120                # IF 'x' == string[1]
    jne collect_regular_string_reset # Otherwise keep going

    add rdx, 2                  # Increment it
    jmp collect_regular_string_reset # Keep going

collect_regular_string_done:
    mov rax, 34                 # Using '"'
    mov [rcx], al               # hold_string[index] = '"'
    add rcx, 1                  # Increment it
    mov rax, 10                 # Using "\n"
    mov [rcx], al               # hold_string[index] = '\n'
    pop rax                     # Return our new string
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# collect_weird_string function
# Receives char* string in RAX
# Mallocs and returns char* hold in RAX
# Uses RCX for char* hold and RDX for char* string
collect_weird_string:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    mov rdx, rax                # Protect our passed string
    mov rax, 512                # We need 512bytes of storage
    call malloc                 # Get our new pointer
    mov rcx, rax                # put it in place
    push rax                    # protect until done

    mov rax, 39                 # Using "'"
    mov [rcx], al               # hold_string[index] = "'"
    add rcx, 1                  # Increment it
    add rdx, 1                  # Increment it
collect_weird_string_reset:
    mov al, [rdx]               # Read a byte
    movzx rax, al               # Make it useful
    cmp rax, 0                  # IF NULL == string[0]
    je collect_weird_string_done # Be done

    mov rax, 32                 # Using ' '
    mov [rcx], al               # hold_string[index] = ' '
    add rcx, 1                  # Increment it

    mov rax, rdx                # Using string
    call escape_lookup          # Get the char
    call hex8                   # Update RCX

    mov al, [rdx]               # Read a byte
    movzx rax, al               # Make it useful
    add rdx, 1                  # Increment it
    cmp rax, 92                 # IF string[0] == '\\'
    jne collect_weird_string_reset # Otherwise keep going

    mov al, [rdx]               # Read a byte
    movzx rax, al               # Make it useful
    add rdx, 1                  # Increment it
    cmp rax, 120                # IF 'x' == string[1]
    jne collect_weird_string_reset # Otherwise keep going

    add rdx, 2                  # Increment it
    jmp collect_weird_string_reset # Keep going

collect_weird_string_done:
    mov rax, 32                 # Using ' '
    mov [rcx], al               # hold_string[index] = ' '
    add rcx, 1                  # Increment it
    mov rax, 48                 # Using '0'
    mov [rcx], al               # hold_string[index] = '0'
    add rcx, 1                  # Increment it
    mov [rcx], al               # hold_string[index] = '0'
    add rcx, 1                  # Increment it
    mov rax, 39                 # Using "'"
    mov [rcx], al               # hold_string[index] = "'"
    add rcx, 1                  # Increment it
    mov rax, 10                 # Using "\n"
    mov [rcx], al               # hold_string[index] = '\n'
    pop rax                     # Return our new string
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# HEX to ascii routine
# Receives INT in RAX and CHAR* in RCX
# Stores ascii of INT in CHAR*
# Returns only modifying RAX and RCX
hex8:
    push rax                    # Protect bottom nibble
    shr rax, 4                  # do high nibble first
    call hex4                   # Store it
    pop rax                     # do low nibble
hex4:
    and rax, 0xf                # isolate nibble
    add al,'0'                  # convert to ascii
    cmp al,'9'                  # valid digit?
    jbe hex1                    # yes
    add al,7                    # use alpha range
hex1:
    mov [ecx], al               # store result
    add ecx, 1                  # next position
    ret


# type_name function
# Receives nothing
# Returns type_size in RAX
# Uses RCX for STRUCT TYPE* RET
type_name:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx+16]           # global_token->S
    lea rax, [rip+struct]       # Using "struct"
    call match                  # IF global_token->S == "struct"
    mov rcx, rax                # Protect structure
    cmp rax, 0                  # need to skip over "struct"
    jne type_name_native        # otherwise keep going

    # Deal with possible STRUCTs
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx]              # global_token->next
    mov [rip+global_token], rbx # global_token = global_token->next
    mov rax, [rbx+16]           # global_token->S
    mov rbx, [rip+global_types] # get all known types
    call lookup_type            # Find type if possible
    mov rcx, rax                # Set ret

    cmp rax, 0                  # IF NULL == ret
    jne type_name_common        # We have to create struct

    # Create a struct
    call create_struct          # Create a new struct
    mov rcx, 0                  # We wish to return NULL
    jmp type_name_done          # be done

type_name_native:
    # Deal only with native types
    mov rax, rbx                # Put global_token->S in the right place
    mov rbx, [rip+global_types] # get all known types
    call lookup_type            # Find the type if possible
    mov rcx, rax                # Set ret

    cmp rax, 0                  # IF NULL == ret
    jne type_name_common        # We need to abort hard

    # Aborting hard
    mov r14, 2                  # write to standard error
    lea rax, [rip+type_name_string_0] # Print header
    call File_Print             # Print it

    mov rax, [rip+global_token] # Using global token
    mov rax, [rax+16]           # global_token->S
    call File_Print             # Print it

    lea rax, [rip+type_name_string_1] # Print footer
    call File_Print             # Print it

#    call line_error             # Give details
    jmp Exit_Failure            # Abort

type_name_common:
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx]              # global_token->next
    mov [rip+global_token], rbx # global_token = global_token->next

type_name_iter:
    mov rax, [rbx+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # make it useful
    cmp rax, 42                 # IF global_token->S[0] == '*'
    jne type_name_done          # recurse

    # Deal with char**
    mov rcx, [rcx+24]           # ret = ret->indirect
    mov rbx, [rip+global_token] # Using global_token
    mov rbx, [rbx]              # global_token->next
    mov [rip+global_token], rbx # global_token = global_token->next
    jmp type_name_iter          # keep looping

type_name_done:
    mov rax, rcx                # put ret in the right place
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

type_name_string_0: .asciz "Unknown type "
type_name_string_1: .byte 10, 0


# lookup_type function
# Receives char* s in RAX and struct type* start in RBX
# Returns struct type* in RAX
# Uses RBX for S and RCX for I
lookup_type:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    mov rcx, rbx                # I = Start
    mov rbx, rax                # Put S in place
lookup_type_iter:
    cmp rcx, 0                  # Check if I == NULL
    je lookup_type_done         # return NULL

    mov rax, [rcx+48]           # I->NAME
    call match                  # Check if matching
    cmp rax, 0                  # IF I->NAME == S
    je lookup_type_done         # return it

    mov rcx, [rcx]              # Otherwise I = I->NEXT
    jmp lookup_type_iter        # And keep looping

lookup_type_done:
    mov rax, rcx                # return either I or NULL
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# create_struct function
# Receives nothing
# Returns nothing
# Uses global_token to malloc a struct's definition
# Uses RCX for int OFFSET, RDX for struct type* head, RBP for struct type* I,
# RDI for member_size (Which is passed) and RSI for LAST
# RAX and RBX are used for scratch
create_struct:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    push rbp                    # Protect RBP
    push rdi                    # Protect RDI
    push rsi                    # Protect RSI
    mov rcx, 0                  # OFFSET = 0
    mov rdi, 0                  # member_size = 0

    mov rax, 56                 # sizeof(struct type)
    call malloc                 # malloc(sizeof(struct type))
    mov rdx, rax                # Set HEAD

    mov rax, 56                 # sizeof(struct type)
    call malloc                 # malloc(sizeof(struct type))
    mov rbp, rax                # Set I

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov [rdx+48], rax           # HEAD->NAME = global_token->S
    mov [rbp+48], rax           # I->NAME = global_token->S

    mov [rdx+24], rbp           # HEAD->INDIRECT = I
    mov [rbp+24], rdx           # I->INDIRECT = HEAD

    mov rax, [rip+global_types] # Using global_types
    mov [rdx], rax              # HEAD->NEXT = global_types
    mov [rip+global_types], rdx # global_types = HEAD

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    mov rax, 8                  # Using register size
    mov [rbp+8], rax            # I->SIZE = register size

    lea rax, [rip+create_struct_string_0] # Using "ERROR in create_struct\n Missing {\n"
    lea rbx, [rip+open_curly_brace] # Using "{"
    call require_match          # Make sure we have it

    mov rsi, 0                  # LAST = NULL

create_struct_iter:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # Make it useful
    cmp rax, 125                # IF global_token->S[0] == "}"
    je create_struct_done       # be done

    # Looks like we are adding members
    # Lets see if it is a union
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    lea rbx, [rip+union]        # Using "union"
    call match                  # IF match(global_token->s, "union")
    cmp rax, 0                  # Deal with union
    jne create_struct_single    # Otherwise deal with singles

    # Deal with union
    mov rax, rsi                # Put last in right place
    mov rbx, rcx                # put offset in right place
    call build_union            # ASSEMBLE
    mov rsi, rax                # last = build_union(last, offset)
    add rcx, rdi                # offset = offset + member_size

    lea rax, [rip+create_struct_string_1] # Using "ERROR in create_struct\n Missing ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it
    jmp create_struct_iter      # keep going

create_struct_single:
    # deal with singles
    mov rax, rsi                # Put last in right place
    mov rbx, rcx                # put offset in right place
    call build_member           # ASSEMBLE
    mov rsi, rax                # last = build_union(last, offset)
    add rcx, rdi                # offset = offset + member_size

    lea rax, [rip+create_struct_string_1] # Using "ERROR in create_struct\n Missing ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it
    jmp create_struct_iter      # keep going

create_struct_done:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    lea rax, [rip+create_struct_string_1] # Using "ERROR in create_struct\n Missing ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it

    mov [rdx+8], rcx            # HEAD->SIZE = OFFSET
    mov [rdx+32], rsi           # HEAD->MEMBERS = LAST
    mov [rbp+32], rsi           # I->MEMBERS = LAST

    pop rsi                     # Restore RSI
    pop rdi                     # Restore RDI
    pop rbp                     # Restore RBP
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

create_struct_string_0: .asciz "ERROR in create_struct\n Missing {\n"
create_struct_string_1: .asciz "ERROR in create_struct\n Missing ;\n"


# lookup_member function
# Receives struct type* parent in RAX and char* name in RBX
# Returns struct type* I in RAX
# Uses char* NAME in RBX, RCX for struct type* I and RDX to hold parent for errors
# Aborts hard if not found
lookup_member:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    mov rdx, rax                # Protect Parent
    mov rcx, [rax+32]           # struct type* I = parent->MEMBERS
lookup_member_iter:
    cmp rcx, 0                  # IF I == NULL
    je lookup_member_fail       # Abort HARD

    mov rax, [rcx+48]           # Using I->NAME
    call match                  # IF I->NAME == NAME
    cmp rax, 0                  # Then we have found the member
    mov rax, rcx                # Prepare for return
    mov rcx, [rcx+32]           # Prepare for loop I = I->MEMBERS
    jne lookup_member_iter      # Looks like we are looping

    # I is already in RAX
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

lookup_member_fail:
    mov r14, 2                  # write to standard error
    lea rax, [rip+lookup_member_string_0] # Using "ERROR in lookup_member "
    call File_Print             # print it

    mov rax, [rdx+48]           # PARENT->NAME
    call File_Print             # print it

    lea rax, [rip+arrow_string] # Using "->"
    call File_Print             # print it

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    call File_Print             # print it

    lea rax, [rip+lookup_member_string_1] # Using " does not exist\n"
    call File_Print             # print it

#    call line_error             # Write useful debug info

    lea rax, [rip+lookup_member_string_2] # Using "\n"
    call File_Print             # print it
    jmp Exit_Failure            # Abort Hard

lookup_member_string_0: .asciz "ERROR in lookup_member "
lookup_member_string_1: .asciz " does not exist\n"
lookup_member_string_2: .byte 10, 0


# build_member function
# Receives struct type* last in RAX, int offset in RBX and global member_size in RDI
# Updates member_size in RDI and returns struct type* I in RAX
# Uses RCX for struct type* member_type and RDX for struct type* I
build_member:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    mov rdx, rax                # Put last out of the way
    mov rax, 56                 # Allocate type
    call malloc                 # Get I
    mov [rax+32], rdx           # I->MEMBERS = LAST
    mov [rax+16], rbx           # I->OFFSET = OFFSET
    mov rdx, rax                # Put I in place

    call type_name              # Get member_type
    mov rcx, rax                # Put in place
    mov [rdx+40], rcx           # I->TYPE = MEMBER_TYPE
    mov rax, [rip+global_token] # Using global_token
    mov rbx, [rax+16]           # global_token->S
    mov [rdx+48], rbx           # I->NAME = global_token->S
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    # Check if we have an array
    mov rbx, [rax+16]           # global_token->S
    lea rax, [rip+open_bracket] # Using "["
    call match                  # IF global_token->S == "["
    cmp rax, 0                  # Then we have to deal with arrays in our structs
    je build_member_array       # So deal with that pain

    # Deal with non-array case
    mov rax, [rcx+8]            # member_type->SIZE
    mov [rdx+8], rax            # I->SIZE = member_type->SIZE
    jmp build_member_done       # Be done

build_member_array:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    mov rax, [rax+16]           # global_token->S
    call numerate_string        # convert number
    mov rbx, [rcx+40]           # member_type->TYPE
    mov rbx, [rbx+8]            # member_type->TYPE->SIZE
    imul rax, rbx               # member_type->type->size * numerate_string(global_token->s)
    mov [rdx+8], rax            # I->SIZE = member_type->type->size * numerate_string(global_token->s)

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    lea rax, [rip+build_member_string_0] # Using "Struct only supports [num] form\n"
    lea rbx, [rip+close_bracket] # Using "]"
    call require_match          # Make sure we have it

build_member_done:
    mov rdi, [rdx+8]            # MEMBER_SIZE = I->SIZE
    mov [rdx+40], rcx           # I->TYPE = MEMBER_TYPE
    mov rax, rdx                # Return I

    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

build_member_string_0: .asciz "Struct only supports [num] form\n"


# build_union function
# Receives struct type* last in RAX, int offset in RBX and global member_size in RDI
# Updates member_size in RDI and returns struct type* LAST in RAX
# Uses RCX for struct type* last, RDX for int offset, RSI for int size and RDI for int member_size
build_union:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    push rsi                    # Protect RSI
    mov rcx, rax                # Put LAST in right spot
    mov rdx, rbx                # Put OFFSET in right spot
    mov rsi, 0                  # SIZE = 0

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    lea rax, [rip+build_union_string_0] # Using "ERROR in build_union\nMissing {\n"
    lea rbx, [rip+open_curly_brace] # Using "{"
    call require_match          # Make sure we have it

build_union_iter:
    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax+16]           # global_token->S
    mov al, [rax]               # global_token->S[0]
    movzx rax, al               # make it useful
    cmp rax, 125                # IF global_token->S[0] == "}"
    je build_union_done         # Be done

    # Collect union member
    mov rax, rcx                # Passing LAST
    mov rbx, rdx                # Passing offset
    call build_member           # build_member(last, offset)
    mov rcx, rax                # last = build_member(last, offset)

    cmp rsi, rdi                # IF member_size > size
    jg build_union_size         # Then update size

    # deal with member_size > size
    mov rsi, rdi                # SIZE = MEMBER_SIZE

build_union_size:
    lea rax, [rip+build_union_string_1] # Using "ERROR in build_union\nMissing ;\n"
    lea rbx, [rip+semicolon]    # Using ";"
    call require_match          # Make sure we have it
    jmp build_union_iter        # Keep going

build_union_done:
    mov rdi, rsi                # MEMBER_SIZE = SIZE

    mov rax, [rip+global_token] # Using global_token
    mov rax, [rax]              # global_token->NEXT
    mov [rip+global_token], rax # global_token = global_token->NEXT

    mov rax, rcx                # Return last

    pop rsi                     # Restore RSI
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

build_union_string_0: .asciz "ERROR in build_union\nMissing {\n"
build_union_string_1: .asciz "ERROR in build_union\nMissing ;\n"


# sym_declare function
# Receives char *s in RAX, struct type* t in RBX, and struct token_list* list in RCX
# Returns struct token_list* in RAX
# Uses RAX for A
sym_declare:
    push rdx                    # Protect RDX
    mov rdx, rax                # Get char *S safely out of the way
    mov rax, 40                 # Using sizeof(struct token_list)
    call malloc                 # Get pointer to A
    mov [rax], rcx              # A->NEXT = LIST
    mov [rax+16], rdx           # A->S = S
    mov [rax+24], rbx           # A->TYPE = T
    pop rdx                     # Restore RDX
    ret


# match function
# Receives CHAR* in RAX and CHAR* in RBX
# Returns 0 (TRUE) or 1 (FALSE) in RAX
match:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    mov rcx, rax                # S1 in place
    mov rdx, rbx                # S2 in place
match_Loop:
    mov al, [rcx]               # S1[0]
    movzx rax, al               # Make it useful
    mov bl, [rdx]               # S2[0]
    movzx rbx, bl               # Make it useful
    cmp rax, rbx                # See if they match
    jne match_False             # If not

    add rcx, 1                  # S1 = S1 + 1
    add rdx, 1                  # S2 = S2 + 1
    cmp rax, 0                  # If reached end of string
    je match_Done               # Perfect match
    jmp match_Loop              # Otherwise keep looping

match_False:
    mov rax, 1                  # Return false
match_Done:
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# numerate_number function
# Receives an INT A in RAX
# Returns char* result in RAX
# Allocates 16 bytes of memory
# Behaves badly when given a negative number too large
# Uses RAX for temp, RBX for DIVISOR, RDX for mod/0, RSI for result[i] and RBP for A
numerate_number:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    push rsi                    # Protect RSI
    push rbp                    # Protect RBP
    mov rbp, rax                # Protect A

    mov rax, 16                 # 16bytes
    call malloc                 # Get our pointer
    push rax                    # Protect our pointer
    mov rsi, rax                # put pointer in right place
    mov rbx, 0x3B9ACA00         # Set divisor to largest positive number that fits in 32bits

    cmp rbp, 0                  # Deal with 0 case
    je numerate_number_ZERO     # If it is
    jg numerate_number_positive # If it is positive

    # Deal with negative case
    mov rax, 45                 # Using "-"
    mov [rsi], al               # Write it
    add rsi, 1                  # increment
    imul rbp, -1                # A = A * -1

numerate_number_positive:
    mov rdx, 0                  # Set top to 0
    mov rax, rbp                # Using A as bottom
    idiv rbx                    # rdx:rax % rbx -> rdx + rdx:rax / rbx -> rax [Even if we don't want it]
    cmp rax, 0                  # IF 0 == (a / divisor)
    jne numerate_number_iter    # Clean up those leading Zeros

    mov rdx, 0                  # Set top to 0
    mov rax, rbx                # Using Divisor for bottom
    mov rbx, 10                 # Make this shit work because idiv 10 doesn't work
    idiv rbx                    # rdx:rax % 10 -> rdx + rdx:rax / 10 -> rax [Even if we don't want it]
    mov rbx, rax                # Update divisor
    jmp numerate_number_positive # Keep collecting

numerate_number_iter:
    cmp rbx, 0                  # IF DIVISOR < 0
    jle numerate_number_done    # Be done

    mov rdx, 0                  # Set top to 0
    mov rax, rbp                # Using A as bottom
    idiv rbx                    # rdx:rax % rbx -> rdx + rdx:rax / rbx -> rax [Even if we don't want it]
    add rax, 48                 # ((a / divisor) + 48)
    mov [rsi], al               # Write it
    mov rbp, rdx                # a = a % divisor

    mov rdx, 0                  # Set top to 0
    mov rax, rbx                # Using Divisor for bottom
    mov rbx, 10                 # Make this shit work because idiv 10 doesn't work
    idiv rbx                    # rdx:rax % 10 -> rdx + rdx:rax / 10 -> rax [Even if we don't want it]
    mov rbx, rax                # Update divisor

    add rsi, 1                  # increment
    jmp numerate_number_iter    # Keep going

numerate_number_done:
    pop rax                     # Restore our result
    pop rbp                     # Restore RBP
    pop rsi                     # Restore RSI
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret

numerate_number_ZERO:
    mov rax, 48                 # Using '0'
    mov [rsi], al               # Write it
    add rsi, 1                  # increment
    jmp numerate_number_done    # Be done


# numerate_string function
# Receives CHAR* in RAX
# Returns value of CHAR* in RAX
# Uses RAX for VALUE, RBX for S, RCX for CH and RSI for NEGATIVE?
numerate_string:
    push rbx                    # Protect RBX
    push rcx                    # Protect RCX
    push rdx                    # Protect RDX
    push rsi                    # Protect RSI
    mov rbx, rax                # put S in correct place
    mov rax, 0                  # Initialize to Zero
numerate_string_loop:
    mov cl, [rbx+1]             # S[1]
    movzx rcx, cl               # make it useful
    cmp rcx, 120                # IF 'x' == S[1]
    je numerate_hex             # Deal with hex input

    # Assume decimal input
    mov rsi, 0                  # Assume no negation
    mov cl, [rbx]               # S[0]
    movzx rcx, cl               # make it useful
    cmp rcx, 45                 # IF '-' == S[0]
    jne numerate_decimal        # Skip negation

    mov rsi, 1                  # Set FLAG
    add rbx, 1                  # S = S + 1

numerate_decimal:
    mov cl, [rbx]               # S[0]
    movzx rcx, cl               # make it useful
    cmp rcx, 0                  # IF NULL == S[0]
    je numerate_decimal_done    # We are done

    imul rax, 10                # VALUE = VALUE * 10
    sub rcx, 48                 # CH = CH - '0'
    cmp rcx, 9                  # Check for illegal
    jg numerate_string_fail     # If CH > '9'
    cmp rcx, 0                  # Check for illegal
    jl numerate_string_fail     # IF CH < 0
    add rax, rcx                # VALUE = VALUE + CH
    add rbx, 1                  # S = S + 1
    jmp numerate_decimal        # Keep looping

numerate_decimal_done:
    cmp rsi, 1                  # Check if need to negate
    jne numerate_string_done    # Nope

    imul rax, -1                # VALUE = VALUE * -1
    jmp numerate_string_done    # Done

numerate_hex:
    add rbx, 2                  # S = S + 2
numerate_hex_loop:
    mov cl, [rbx]               # S[0]
    movzx rcx, cl               # make it useful
    cmp rcx, 0                  # IF NULL == S[0]
    je numerate_string_done     # We are done

    shl rax, 4                  # VALUE = VALUE << 4
    sub rcx, 48                 # CH = CH - '0'
    cmp rcx, 10                 # IF 10 >= CH
    jl numerate_hex_digit       # NO
    sub rcx, 7                  # Push A-F into range
numerate_hex_digit:
    cmp rcx, 15                 # Check for illegal
    jg numerate_string_fail     # If CH > 'F'
    cmp rcx, 0                  # Check for illegal
    jl numerate_string_fail     # IF CH < 0
    add rax, rcx                # VALUE = VALUE + CH
    add rbx, 1                  # S = S + 1
    jmp numerate_hex_loop       # Keep looping

numerate_string_fail:
    mov rax, 0                  # return ZERO

numerate_string_done:
    pop rsi                     # Restore RSI
    pop rdx                     # Restore RDX
    pop rcx                     # Restore RCX
    pop rbx                     # Restore RBX
    ret


# Exit_Failure function
# Receives nothing
# And aborts hard
# Does NOT return
Exit_Failure:
    mov rdi, 1                  # All is wrong
    mov rax, 0x3c               # put the exit syscall number in eax
    syscall                     # Call it a bad day

# debug_list function
# Receives struct token_list* in RAX
# Prints contents of list and exits
# Does NOT return
debug_list:
    mov r12, rax                # Protect the list pointer
    mov r14, 2                  # write to standard error

debug_list_iter:
    # Header
    lea rax, [rip+debug_list_string0] # Using our first string
    call File_Print             # Print it
    mov rax, r12                # Use address of pointer
    call numerate_number        # Convert it into string
    call File_Print             # Print it

    # NEXT
    lea rax, [rip+debug_list_string1] # Using our second string
    call File_Print             # Print it
    mov rax, [r12]              # Use address of pointer
    call numerate_number        # Convert it into string
    call File_Print             # Print it

    # PREV
    lea rax, [rip+debug_list_string2] # Using our third string
    call File_Print             # Print it
    mov rax, [r12+8]            # Use address of pointer
    call numerate_number        # Convert it into string
    call File_Print             # Print it

    # S
    lea rax, [rip+debug_list_string3] # Using our fourth string
    call File_Print             # Print it
    mov rax, [r12+16]           # Use address of pointer
    call numerate_number        # Convert it into string
    call File_Print             # Print it

    # S Contents
    lea rax, [rip+debug_list_string4] # Using our fifth string
    call File_Print             # Print it
    mov rax, [r12+16]           # Use address of string
    cmp rax, 0                  # IF NULL Pointer
    jne debug_list_null         # otherwise display
    lea rax, [rip+debug_list_string_null] # Give meaningful message instead
debug_list_null:
    call File_Print             # Print it

    # TYPE
    lea rax, [rip+debug_list_string5] # Using our sixth string
    call File_Print             # Print it
    mov rax, [r12+24]           # Use address of pointer
    call numerate_number        # Convert it into string
    call File_Print             # Print it

    # ARGS/DEPTH
    lea rax, [rip+debug_list_string6] # Using our seventh string
    call File_Print             # Print it
    mov rax, [r12+32]           # Use address of pointer
    call numerate_number        # Convert it into string
    call File_Print             # Print it

    mov rax, 10                 # Add "\n"
    call fputc                  # print it
    call fputc                  # print it

    mov r12, [r12]              # TOKEN = TOKEN->NEXT
    cmp r12, 0                  # Check if NULL
    jne debug_list_iter         # iterate otherwise

    mov rdi, 666                # All is HELL
    mov rax, 0x3c               # put the exit syscall number in eax
    syscall                     # Call it a bad day

.data

debug_list_string0: .asciz "Token_list node at address: "
debug_list_string1: .asciz "\nNEXT address: "
debug_list_string2: .asciz "\nPREV address: "
debug_list_string3: .asciz "\nS address: "
debug_list_string4: .asciz "\nThe contents of S are: "
debug_list_string5: .asciz "\nTYPE address: "
debug_list_string6: .asciz "\nARGUMENTS address: "
debug_list_string_null: .asciz ">::<NULL>::<"

# Keywords
union: .asciz "union"
struct: .asciz "struct"
constant: .asciz "CONSTANT"
main_string: .asciz "main"
argc_string: .asciz "argc"
argv_string: .asciz "argv"
if_string: .asciz "if"
else_string: .asciz "else"
do_string: .asciz "do"
while_string: .asciz "while"
for_string: .asciz "for"
asm_string: .asciz "asm"
goto_string: .asciz "goto"
return_string: .asciz "return"
break_string: .asciz "break"
continue_string: .asciz "continue"
sizeof_string: .asciz "sizeof"
plus_string: .asciz "+"
minus_string: .asciz "-"
multiply_string: .asciz "*"
divide_string: .asciz "/"
modulus_string: .asciz "%"
left_shift_string: .asciz "<<"
right_shift_string: .asciz ">>"
less_than_string: .asciz "<"
less_than_equal_string: .asciz "<="
greater_than_equal_string: .asciz ">="
greater_than_string: .asciz ">"
equal_to_string: .asciz "=="
not_equal_string: .asciz "!="
bitwise_and: .asciz "&"
logical_and: .asciz "&&"
bitwise_or: .asciz "|"
logical_or: .asciz "||"
bitwise_xor: .asciz "^"
arrow_string: .asciz "->"


# Frequently Used strings
# Generally used by require_match
open_curly_brace: .asciz "{"
close_curly_brace: .asciz "}"
open_paren: .asciz "("
close_paren: .asciz ")"
open_bracket: .asciz "["
close_bracket: .asciz "]"
comma: .asciz ","
semicolon: .asciz ";"
equal: .asciz "="
percent: .asciz "%"
newline: .asciz "\n"
underline: .asciz "_"


prim_types:
type_void:
    .quad type_int - type_void           # NEXT
    .quad 8                              # SIZE
    .quad 0                              # OFFSET
    .quad type_void - type_void          # INDIRECT
    .quad 0                              # MEMBERS
    .quad type_void - type_void          # TYPE
    .quad type_void_name - type_void     # NAME

type_int:
    .quad type_char - type_int           # NEXT
    .quad 8                              # SIZE
    .quad 0                              # OFFSET
    .quad type_int - type_int            # INDIRECT
    .quad 0                              # MEMBERS
    .quad type_int - type_int            # TYPE
    .quad type_int_name - type_int       # NAME

type_char:
    .quad type_file - type_char          # NEXT
    .quad 1                              # SIZE
    .quad 0                              # OFFSET
    .quad type_char_indirect - type_char # INDIRECT
    .quad 0                              # MEMBERS
    .quad type_char - type_char          # TYPE
    .quad type_char_name - type_char     # NAME

type_char_indirect:
    .quad type_file - type_char_indirect                 # NEXT
    .quad 8                                              # SIZE
    .quad 0                                              # OFFSET
    .quad type_char_double_indirect - type_char_indirect # INDIRECT
    .quad 0                                              # MEMBERS
    .quad type_char_indirect - type_char_indirect        # TYPE
    .quad type_char_indirect_name - type_char_indirect   # NAME

type_char_double_indirect:
    .quad type_file - type_char_double_indirect                      # NEXT
    .quad 8                                                          # SIZE
    .quad 0                                                          # OFFSET
    .quad type_char_double_indirect - type_char_double_indirect      # INDIRECT
    .quad 0                                                          # MEMBERS
    .quad type_char_indirect - type_char_double_indirect             # TYPE
    .quad type_char_double_indirect_name - type_char_double_indirect # NAME

type_file:
    .quad type_function - type_file          # NEXT
    .quad 8                                  # SIZE
    .quad 0                                  # OFFSET
    .quad type_file - type_file              # INDIRECT
    .quad 0                                  # MEMBERS
    .quad type_file - type_file              # TYPE
    .quad type_file_name - type_file         # NAME

type_function:
    .quad type_unsigned - type_function      # NEXT
    .quad 8                                  # SIZE
    .quad 0                                  # OFFSET
    .quad type_function - type_function      # INDIRECT
    .quad 0                                  # MEMBERS
    .quad type_function - type_function      # TYPE
    .quad type_function_name - type_function # NAME

type_unsigned:
    .quad type_long - type_unsigned          # NEXT
    .quad 8                                  # SIZE
    .quad 0                                  # OFFSET
    .quad type_unsigned - type_unsigned      # INDIRECT
    .quad 0                                  # MEMBERS
    .quad type_unsigned - type_unsigned      # TYPE
    .quad type_unsigned_name - type_unsigned # NAME

type_long:
    .quad 0                                  # NEXT
    .quad 8                                  # SIZE
    .quad 0                                  # OFFSET
    .quad type_long - type_long              # INDIRECT
    .quad 0                                  # MEMBERS
    .quad type_long - type_long              # TYPE
    .quad type_long_name - type_long         # NAME

type_void_name: .asciz "void"
type_int_name: .asciz "int"
type_char_name: .asciz "char"
type_char_indirect_name: .asciz "char*"
type_char_double_indirect_name: .asciz "char**"
type_file_name: .asciz "FILE"
type_function_name: .asciz "FUNCTION"
type_unsigned_name: .asciz "unsigned"
type_long_name: .asciz "long"

Address_of: .quad 0
C: .quad 0
Token: .quad 0
break_frame: .quad 0
break_target_func: .quad 0
break_target_head: .quad 0
break_target_num: .quad 0
current_count: .quad 0
current_target: .quad 0
function: .quad 0
global_constant_list: .quad 0
global_function_list: .quad 0
global_symbol_list: .quad 0
global_token: .quad 0
global_types: .quad 0
globals_list: .quad 0
output_list: .quad 0
string_index: .quad 0
strings_list: .quad 0
