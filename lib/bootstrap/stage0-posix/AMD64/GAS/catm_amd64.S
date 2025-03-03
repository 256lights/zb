# SPDX-FileCopyrightText: 2019 Jeremiah Orians <jeremiah@pdp10.guru>
# SPDX-FileCopyrightText: 2023 Andrius Štikonas <andrius@stikonas.eu>
#
# SPDX-License-Identifier: GPL-3.0-or-later

.intel_syntax noprefix
.global _start
.text

    # Register usage:
    # R15 => OUTPUT
    # R14 => Buffer
    # R13 => INPUT

# Where the ELF Header is going to hit
# Simply jump to _start
# Our main function
_start:
    pop rax                     # Get the number of arguments
    pop rdi                     # Get the program name
    pop rdi                     # Get the actual output name
    mov rsi, 577                # Prepare file as O_WRONLY|O_CREAT|O_TRUNC
    mov rdx, 384                # Prepare file as RW for owner only (600 in octal)
    mov rax, 2                  # the syscall number for open()
    syscall                     # Now open that file
    mov r15, rax                # Preserve the file pointer we were given

    mov rax, 12                 # the Syscall # for SYS_BRK
    mov rdi, 0                  # Get current brk
    syscall                     # Let the kernel do the work
    mov r14, rax                # Set our malloc pointer

    mov rax, 12                 # the Syscall # for SYS_BRK
    mov rdi, r14                # Using current pointer
    add rdi, 0x100000           # Allocate 1MB
    syscall                     # Let the kernel do the work

core:
    pop rdi                     # Get the actual input name
    cmp rdi, 0                  # Check for null string
    je done                     # Hit null be done

    mov rsi, 0                  # prepare read_only
    mov rdx, 0                  # prevent any interactions
    mov rax, 2                  # the syscall number for open()
    syscall                     # Now open that damn file
    mov r13, rax                # Protect INPUT
keep:
    mov rdx, 0x100000           # set the size of chars we want
    mov rsi, r14                # Where to put it
    mov rdi, r13                # Where are we reading from
    mov rax, 0                  # the syscall number for read
    syscall                     # call the Kernel
    push rax                    # Protect the number of bytes read

    mov rdx, rax                # Number of bytes to write
    mov rsi, r14                # What we are writing
    mov rdi, r15                # Write to target file
    mov rax, 1                  # the syscall number for write
    syscall                     # call the Kernel

    pop rax                     # Get bytes read
    cmp rax, 0x100000           # Check if buffer was fully used
    je keep                     # Keep looping if was full
    jmp core                    # Otherwise move to next file

done:
    # program completed Successfully
    mov rdi, 0                  # All is well
    mov rax, 0x3c               # put the exit syscall number in eax
    syscall                     # Call it a good day
