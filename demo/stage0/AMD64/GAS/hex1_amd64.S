# SPDX-FileCopyrightText: 2017 Jeremiah Orians <jeremiah@pdp10.guru>
# SPDX-FileCopyrightText: 2023 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

.intel_syntax noprefix
.global _start
.text

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
	mov r9, rax                 # Preserve the file pointer we were given

	pop rdi                     # Get the actual output name
	mov rsi, 577                # Prepare file as O_WRONLY|O_CREAT|O_TRUNC
	mov rdx, 448                # Prepare file as RWX for owner only (700 in octal)
	mov rax, 2                  # the syscall number for open()
	syscall                     # Now open that damn file
	mov r10, rax                # Preserve the file pointer we were given

	mov r15, -1                 # Our flag for byte processing
	mov r14, 0                  # temp storage for the sum
	mov r13, 0                  # Our starting IP
	call First_pass             # Process it

	# rewind input file
	mov rdi, r9                 # Using our input file
	mov rsi, 0                  # Offset Zero
	mov rdx, 0                  # Whence Zero
	mov rax, 8                  # lseek
	syscall

	mov r15, -1                 # Our flag for byte processing
	mov r14, 0                  # temp storage for the sum
	mov r13, 0                  # Our starting IP
	call Second_pass            # Process it

	jmp Done

First_pass:
	call Read_byte

	# Deal with EOF
	cmp al, -4
	je First_pass_done

	# Check for :
	cmp al, 0x3a
	jne First_pass_0

	# Deal with label
	call StoreLabel

First_pass_0:
	# Check for %
	cmp al, 0x25
	je First_pass_pointer

	# Deal with everything else
	call hex                    # Process our char

	# Deal with EOF
	cmp al, -4
	je First_pass_done

	# deal with -1 values
	cmp al, 0
	jl First_pass

	# deal with toggle
	cmp r15, 0
	je First_pass_1
	add r13, 1                  # Increment IP

First_pass_1:
	not r15
	jmp First_pass

First_pass_pointer:
	# Deal with Pointer to label
	call Read_byte              # Drop the char
	add r13, 4                  # Increment IP
	jmp First_pass              # Loop again

First_pass_done:
	ret

hex:
	# deal with EOF
	cmp al, -4
	je EOF
	# deal with line comments starting with #
	cmp al, 0x23
	je ascii_comment
	# deal with line comments starting with ;
	cmp al, 0x3b
	je ascii_comment
	# deal all ascii less than 0
	cmp al, 0x30
	jl ascii_other
	# deal with 0-9
	cmp al, 0x3a
	jl ascii_num
	# deal with all ascii less than A
	cmp al, 0x41
	jl ascii_other
	# deal with A-F
	cmp al, 0x47
	jl ascii_high
	# deal with all ascii less than a
	cmp al, 0x61
	jl ascii_other
	# deal with a-f
	cmp al, 0x67
	jl ascii_low
	# The rest that remains needs to be ignored
	jmp ascii_other

Second_pass:
	call Read_byte

	# Deal with EOF
	cmp al, -4
	je Second_pass_done

	# Simply drop the label
	cmp al, 0x3a
	jne Second_pass_0

	call Read_byte
	jmp Second_pass

Second_pass_0:
	# Deal with  pointer
	cmp al, 0x25
	jne Second_pass_1

	call StorePointer
	jmp Second_pass

Second_pass_1:
	# Deal with everything else
	call hex                    # Process our char

	# Deal with EOF
	cmp al, -4
	je Second_pass_done

	# deal with -1 values
	cmp al, 0
	jl Second_pass

	# deal with toggle
	cmp r15, 0
	je print

	# process first byte of pair
	mov r14, rax
	mov r15, 0
	jmp Second_pass

Second_pass_done:
EOF:
	ret
ascii_num:
	sub al, 0x30
	ret
ascii_low:
	sub al, 0x57
	ret
ascii_high:
	sub al, 0x37
	ret
ascii_other:
	mov al, -1
	ret
ascii_comment:
	call Read_byte
	cmp al, 0xd
	je ascii_comment_cr
	cmp al, 0xa
	jne ascii_comment
ascii_comment_cr:
	mov al, -1
	ret

# process second byte of pair
print:
	# update the sum and store in output
	shl r14, 4
	add rax, r14
	mov [rip+table], al

	# flip the toggle
	not r15

	# Print our first Hex
	mov rdx, 1                  # set the size of chars we want
	call print_chars

	add r13, 1                  # Increment IP
	jmp Second_pass

Done:
	# program completed Successfully
	mov rdi, 0                  # All is well
	mov rax, 0x3c               # put the exit syscall number in rax
	syscall                     # Call it a good day

Read_byte:
	# Attempt to read 1 byte from STDIN
	mov rdx, 1                  # set the size of chars we want
	lea rsi, [rip+table]        # Where to put it
	mov rdi, r9                 # Where are we reading from
	mov rax, 0                  # the syscall number for read
	syscall                     # call the Kernel

	test rax, rax               # check what we got
	je Read_byte_1              # Got EOF call it done

	# load byte
	mov al, [rip+table]         # load char
	movzx rax, al               # We have to zero extend it to use it
	ret

# Deal with EOF
Read_byte_1:
	mov al, -4                  # Put EOF in rax
	ret

print_chars:
	lea rsi, [rip+table]        # What we are writing
	mov rdi, r10                # Write to target file
	mov rax, 1                  # the syscall number for write
	syscall                     # call the Kernel
	ret

Get_table_target:
	call Read_byte              # Get single char label
	shl rax, 3                  # Each label in table takes 8 bytes to store
	lea rcx, [rip+table]        # Get table
	add rax, rcx                # Calculate offset
	ret

StoreLabel:
	call Get_table_target
	mov [rax], r13              # Write out pointer to table
	xor eax, eax                # wipe higher bits of rax, so that cmp al works
	ret

StorePointer:
	add r13, 4                  # Increment IP
	call Get_table_target       # Get address of pointer
	mov rax, [rax]              # Get pointer
	sub rax, r13                # target - ip
	mov [rip+table], rax        # put value in output
	mov rdx, 4                  # set the size of chars we want
	call print_chars
	ret


.data

table:
	.quad 0

ELF_end:
