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
	xor esi, esi                # prepare read_only, rsi = 0
	push 2                      # prepare syscall number
	pop rax                     # the syscall number for open()
	syscall                     # Now open that damn file
	mov r9, rax                 # Preserve the file pointer we were given

	pop rdi                     # Get the actual output name
	mov si, 577                 # Prepare file as O_WRONLY|O_CREAT|O_TRUNC
	mov dx, 448                 # Prepare file as RWX for owner only (700 in octal)
	push 2                      # prepare syscall number
	pop rax                     # the syscall number for open()
	syscall                     # Now open that damn file
	mov r10, rax                # Preserve the file pointer we were given

	# Our flag for byte processing
	push -1
	pop rbx                     # rbx = -1

	# tmp storage for the sum
	xor rbp, rbp                # rbp = 0

loop:
	# Read a byte
	call Read_byte

	# process byte
	call hex

	# Deal with -1 values
	test eax, eax
	jl loop

	# deal with toggle
	test ebx, ebx               # jump if ebx >= 0
	jge print

	# process first byte of pair
	mov ebp, eax
	xor ebx, ebx                # rbx = 0
	jmp loop

# process second byte of pair
print:
	# update the sum and store in output
	shl ebp, 4
	add eax, ebp

	# flip the toggle
	dec ebx                     # rbx = -1

	call write_byte

	jmp loop

hex:
	# Purge Comment Lines (#)
	cmp al, 35
	je purge_comment

	# Purge Comment Lines (;)
	cmp al, 59
	je purge_comment

	# deal all ascii less than 0
	cmp al, 48
	jl ascii_other

	# deal with 0-9
	cmp al, 58
	jl ascii_num

	# deal with all ascii less than A
	cmp al, 65
	jl ascii_other

	# deal with A-F
	cmp al, 71
	jl ascii_high

	# deal with all ascii less than a
	cmp al, 97
	jl ascii_other

	# deal with a-f
	cmp al, 103
	jl ascii_low

	# The rest that remains needs to be ignored
	jmp ascii_other

purge_comment:
	# Read a byte
	call Read_byte

	# Loop if not LF
	cmp al, 10
	jne purge_comment

	# Otherwise return -1

ascii_other:
	push -1
	pop rax                     # return -1
	ret

ascii_num:
	sub al, 48
	ret

ascii_low:
	sub al, 32

ascii_high:
	sub al, 55
	ret

# Writes byte stored in al
write_byte:
	# Print our Hex
	push 1                      # prepare to set rdx to 1
	pop rdx                     # set the size of chars we want
	push rax                    # Move output to stack
	mov rsi, rsp                # What we are writing
	mov rdi, r10                # Where are we writing to
	push 1                      # prepare syscall number for write
	pop rax                     # get the syscall number for write
	syscall                     # call the Kernel
	pop rdi                     # deallocate stack
	ret

Read_byte:
	# Attempt to read 1 byte from STDIN
	push 1                      # prepare to set rdx to 1
	pop rdx                     # set the size of chars we want
	push rbp                    # allocate stack
	mov rsi, rsp                # Where to put it
	mov rdi, r9                 # Where are we reading from
	xor eax, eax                # the syscall number for read, rax = 0
	syscall                     # call the Kernel

	test eax, eax               # check what we got
	je Done                     # Got EOF call it done

	# load byte
	pop rax                     # load char
	ret

Done:
	# program completed Successfully
	xor edi, edi                # All is well, rdi = 0
	push 60                     # syscall number for exit is 60
	pop rax                     # put the exit syscall number in eax
	syscall                     # Call it a good day

ELF_end:
