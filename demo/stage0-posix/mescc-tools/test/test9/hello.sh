#! /bin/sh
## Copyright (C) 2017 Jeremiah Orians
## This file is part of mescc-tools.
##
## mescc-tools is free software: you can redistribute it and/or modify
## it under the terms of the GNU General Public License as published by
## the Free Software Foundation, either version 3 of the License, or
## (at your option) any later version.
##
## mescc-tools is distributed in the hope that it will be useful,
## but WITHOUT ANY WARRANTY; without even the implied warranty of
## MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
## GNU General Public License for more details.
##
## You should have received a copy of the GNU General Public License
## along with mescc-tools.  If not, see <http://www.gnu.org/licenses/>.

set -ex
./bin/blood-elf -f test/test9/M1.M1 \
	--entry exit \
	--entry read \
	--entry write \
	--entry open \
	--entry chmod \
	--entry access \
	--entry brk \
	--entry fsync \
	--entry strlen \
	--entry eputc \
	--entry eputs \
	--entry fputs \
	--entry puts \
	--entry putchar \
	--entry fputc \
	--entry getchar \
	--entry fgetc \
	--entry free \
	--entry ungetc \
	--entry strcmp \
	--entry strcpy \
	--entry itoa \
	--entry isdigit \
	--entry isxdigit \
	--entry isnumber \
	--entry atoi \
	--entry malloc \
	--entry memcpy \
	--entry realloc \
	--entry strncmp \
	--entry getenv \
	--entry vprintf \
	--entry printf \
	--entry vsprintf \
	--entry sprintf \
	--entry getopt \
	--entry close \
	--entry unlink \
	--entry lseek \
	--entry getcwd \
	--entry dlclose \
	--entry dlopen \
	--entry execvp \
	--entry fclose \
	--entry fdopen \
	--entry ferror \
	--entry fflush \
	--entry fopen \
	--entry fprintf \
	--entry fread \
	--entry fseek \
	--entry ftell \
	--entry fwrite \
	--entry gettimeofday \
	--entry localtime \
	--entry longjmp \
	--entry memmove \
	--entry memset \
	--entry memcmp \
	--entry mprotect \
	--entry qsort \
	--entry remove \
	--entry setjmp \
	--entry sigaction \
	--entry sigemptyset \
	--entry snprintf \
	--entry sscanf \
	--entry strcat \
	--entry strchr \
	--entry strrchr \
	--entry strstr \
	--entry strtol \
	--entry strtoll \
	--entry strtoul \
	--entry strtoull \
	--entry time \
	--entry vsnprintf \
	--entry calloc \
	--entry vfprintf \
	--entry buf \
	--entry optarg \
	--entry optind \
	--entry opterr \
	--entry optarg \
	--entry optind \
	--entry nextchar \
	--entry opterr \
	--entry errno \
	--entry _start \
	--little-endian \
	-o test/test9/footer.M1
./bin/M1 --little-endian --architecture x86 -f test/test9/x86.M1 -f test/test9/M1.M1 -f test/test9/footer.M1 -o test/test9/M1.hex2
./bin/hex2 --little-endian --architecture x86 --base-address 0x1000000 -f elf_headers/elf32-debug.hex2 -f test/test9/crt1.hex2 -f test/test9/libc-mes+tcc.hex2 -f test/test9/M1.hex2 -o test/results/test9-binary
exit 0
