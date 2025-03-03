# SPDX-FileCopyrightText: 2020 Jeremiah Orians <jeremiah@pdp10.guru>
# SPDX-FileCopyrightText: 2023 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

.intel_syntax noprefix
.global _start
.text

# Where the ELF Header is going to hit
# Simply jump to _start
# Our main function

# Register usage
# * R13 script
# * R14 MALLOC
# * R15 command_done

_start:
	pop rax                     # Get the number of arguments
	pop rdi                     # Get the program name
	pop rdi                     # Get the actual input name
	test edi, edi               # Check for missing output
	jne _start_out              # Have real input
	push rax                    # Need to adjust stack
	mov edi, OFFSET default_file # Use "kaem.amd64"

_start_out:
	xor esi, esi                # prepare read_only
	push 2
	pop rax                     # the syscall number for open()
	syscall                     # Now open that damn file

	test eax, eax               # IF NULL We couldn't open the file
	jle Exit_Failure            # Abort hard
	mov r13, rax                # Set input pointer

	pop rax                     # Get start of envp
	mov rbp, rsp                # Protect envp

	push 12
	pop rax                     # the Syscall # for SYS_BRK
	xor edi, edi                # Get current brk
	syscall                     # Let the kernel do the work
	mov r14, rax                # Set our malloc pointer

	# Where the main work gets done
	# Using RDI for tokens and RSI for tokens[i]
main_loop:
	xor eax, eax                # Zero RAX
	mov ah, 0x8                 # Using 256 char* of space (0x800)
	call malloc                 # get it
	mov rdi, rax                # set tokens

	mov rsi, rdi                # I = 0
	xor r15, r15                # command_done = 0

	# Using RAX for result and RDI for tokens[i]
collect_command:
	call collect_token          # Get another token
	test eax, eax               # if NULL == result
	je collect_command_comment  # It is a comment, don't store

	mov [rsi], rax              # tokens[i] = result
	add rsi, 8                  # i = i + 1 (adjusted for char* size)

collect_command_comment:
	test r15, r15               # IF 0 == command_done
	je collect_command          # keep looping

	# Deal with line comments
	cmp rsi, rdi                # if 0 < i
	je main_loop                # It was a comment

	call print_command          # print the command
	mov rax, [rdi]              # program = tokens[0]
	test eax, eax               # IF NULL == program
	je Exit_Failure             # Some shit went down, abort

	push rdi                    # Protect Tokens
	push 57
	pop rax                     # FORKing
	syscall                     # int f = FORK()
	pop rdi                     # Restore Tokens

	test eax, eax               # Check fork
	jl Exit_Failure             # IF f == -1 abort hard
	jne collect_command_parent  # IF f == 0 it is child

	# Deal with child case
	push 59
	pop rax                     # EXECVE
	mov rdx, rbp                # third arg = envp
	mov rsi, rdi                # second arg = tokens
	mov rdi, [rdi]              # program = tokens[0]
	syscall                     # execve(program, tokens, envp);

# Exit_Failure function
# Receives nothing
# And aborts hard
# DOES NOT RETURN
Exit_Failure:
	push 1
	pop rdi                     # All is wrong
	jmp Exit                    # Exit

collect_command_parent:
	mov rdi, rax                # first arg = f
	push rax                    # allocate stack for status
	mov rsi, rsp                # second arg = &status
	xor edx, edx                # third arg = NULL
	push 61
	pop rax                     # WAITPID
	syscall                     # wait4pid(f, &status, 0, 0); [r10 = 0]

	pop rax                     # Using status
	test eax, eax               # IF 0 == status
	je main_loop                # Loop forever

	# Deal with very unhappy case
	mov eax, OFFSET hard        # Using "Subprocess error\n"
	call File_Print             # Print it
	jmp Exit_Failure            # return error

Done:
	# program completed Successfully
	xor edi, edi                # All is well
Exit:
	push 60                     # SYS_exit
	pop rax                     # put the exit syscall number in rax
	syscall                     # Call it a day

# print_command function
# Receives tokens[j] in RDI and tokens[i] in RSI
# Modifies RAX
print_command:
	push rdi                    # Protect RDI
	mov eax, OFFSET prefix      # using " +> "
	call File_Print             # print it
print_command_loop:
	mov rax, [rdi]              # using tokens[j]
	call File_Print             # print it
	add rdi, 8                  # j = j + 1
	push 32
	pop rax                     # using ' '
	call fputc                  # print it
	cmp rdi, rsi                # IF j < i
	jne print_command_loop      # otherwise keep looping

	push 10
	pop rax                     # using '\n'
	call fputc                  # print it
	pop rdi                     # Restore RDI
	ret


# collect_token function
# Receives nothing
# Overwrites RAX
# Uses RAX as C, RDI as token and RSI as token[i]
collect_token:
	push rdi                    # Protect RDI
	push rsi                    # Protect RSI
	xor eax, eax                # Zero RAX
	mov ah, 0x10                # max_string = 4096 * sizeof(char)
	call malloc                 # allocate space
	mov rdi, rax                # token = malloc(max_string);
	mov rsi, rax                # i = 0; set token[i]

collect_token_loop:
	call fgetc                  # c = fgetc(input);
	cmp al, -4                  # if C == EOF
	je Done                     # We are done

	cmp al, 32                  # IF C == ' '
	je collect_token_done       # Space terminates token

	cmp al, 9                   # IF C == '\t'
	je collect_token_done       # tab terminates token

	cmp al, 10                  # IF C == '\n'
	jne collect_token_string    # otherwise check next

	# It is a newline
	jmp collect_token_set_command_done # Set command_done = TRUE

collect_token_string:
	cmp al, 34                  # IF C == '"'
	jne collect_token_comment   # otherwise check next

	# It is a RAW STRING
	call collect_string         # Get the rest of the string
	jmp collect_token_done      # Be done

collect_token_comment:
	cmp al, 35                  # IF C == '#'
	jne collect_token_escape    # otherwise check next

	# It is a line comment
	call collect_comment        # Read it all
collect_token_set_command_done:
	push 1
	pop r15                     # Set command_done = TRUE
	jmp collect_token_done      # Be done

collect_token_escape:
	cmp al, 92                  # IF C == '\\'
	jne collect_token_other     # otherwise just store it

	# It is an escape char
	call fgetc                  # Read the char to drop
	jmp collect_token_done      # Be done

collect_token_other:
	mov [rsi], al               # token[i] = C
	add rsi, 1                  # i = i + 1
	jmp collect_token_loop      # Keep going

collect_token_done:
	cmp rsi, rdi                # IF i == 0
	jne collect_token_good      # otherwise return the token
	xor edi, edi                # token = NULL

collect_token_good:
	mov rax, rdi                # Return token
	pop rsi                     # Restore RSI
	pop rdi                     # Restore RDI
	ret


# collect_string function
# Receives target[index] in RSI
# Modifies RAX
# Uses RAX as C
collect_string:
	call fgetc                  # C = fgetc(input)
	cmp al, -4                  # if C == EOF
	je Exit_Failure             # Something went horribly wrong

	cmp al, 34                  # IF C == '"'
	je collect_string_done      # be done

	# deal with inside of string
	mov [rsi], al               # target[index] = C
	add rsi, 1                  # index = index + 1
	jmp collect_string          # Keep going

collect_string_done:
	ret


# collect_comment function
# Receives nothing
# Modifies RAX
# uses RAX as Int C
# Just throws away everything it reads
collect_comment:
	call fgetc                  # C = fgetc(input)
	cmp al, -4                  # IF C == EOF
	je Exit_Failure             # abort hard

	cmp al, 10                  # IF C == '\n'
	jne collect_comment         # otherwise keep looping
	ret


# Malloc isn't actually required if the program being built fits in the initial memory
# However, it doesn't take much to add it.
# Requires [MALLOC] to be initialized and RAX to have the number of desired bytes
malloc:
	push rdi                    # Protect RDI
	push rsi                    # Protect RSI
	push rdx                    # Protect RDX
	mov rdi, r14                # Using the current pointer
	add rdi, rax                # Request the number of desired bytes
	push 12
	pop rax                     # the Syscall # for SYS_BRK
	syscall                     # call the Kernel
	mov rax, r14                # Return pointer
	mov r14, rdi                # Update pointer
	pop rdx                     # Restore RDX
	pop rsi                     # Restore RSI
	pop rdi                     # Restore RDI
	ret


# fgetc function
# Loads FILE* from [script]
# Returns -4 (EOF) or char in AL
fgetc:
	push rdi                    # Protect RDI
	push rsi                    # Protect RSI
	push rdx                    # Protect RDX
	push -4
	pop rax                     # Put EOF in RAX
	push rax                    # Assume bad (If nothing read, value will remain EOF)
	lea rsi, [rsp]              # Get stack address
	mov rdi, r13                # Where are we reading from
	xor eax, eax                # the syscall number for read
	push 1
	pop rdx                     # set the size of chars we want
	syscall                     # call the Kernel
	pop rax                     # Get either char or EOF
	cmp al, -4                  # Check for EOF
	je fgetc_done               # Return as is
fgetc_done:
	pop rdx                     # Restore RDX
	pop rsi                     # Restore RSI
	pop rdi                     # Restore RDI
	ret


# File_Print function
# Receives CHAR* in RAX
# calls fputc for every non-null char
File_Print:
	push rdi                    # Protect RDI
	push rsi                    # Protect RSI
	mov rdi, rax                # Protect S
	test eax, eax               # Protect against nulls
	je File_Print_Done          # Simply don't try to print them
File_Print_Loop:
	xor eax, eax                # Zero RAX
	mov al, [rdi]               # Read byte
	test eax, eax               # Check for NULL
	je File_Print_Done          # Stop at NULL

	call fputc                  # write it
	add rdi, 1                  # S = S + 1
	jmp File_Print_Loop         # Keep going

File_Print_Done:
	pop rsi                     # Restore RSI
	pop rdi                     # Restore RDI
	ret


# fputc function
# receives CHAR in RAX and load FILE* from stdout
# writes char and returns
fputc:
	push rdi                    # Protect RDI
	push rsi                    # Protect RSI
	push rdx                    # Protect RDX
	push rax                    # We are writing rax
	lea rsi, [rsp]              # Get stack address
	push 1
	pop rdi                     # Write to target file
	mov rax, rdi                # the syscall number for write
	mov rdx, rdi                # set the size of chars we want
	syscall                     # call the Kernel
	pop rax                     # Restore stack
	pop rdx                     # Restore RDX
	pop rsi                     # Restore RSI
	pop rdi                     # Restore RDI
	ret


.data
default_file: .asciz "kaem.amd64"
prefix: .asciz " +> "
hard: .asciz "Subprocess error\n"
