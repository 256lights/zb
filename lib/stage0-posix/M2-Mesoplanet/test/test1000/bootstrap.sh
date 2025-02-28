#! /bin/sh
## Copyright (C) 2021 Jeremiah Orians
## This file is part of M2-Planet.
##
## M2-Planet is free software: you can redistribute it and/or modify
## it under the terms of the GNU General Public License as published by
## the Free Software Foundation, either version 3 of the License, or
## (at your option) any later version.
##
## M2-Planet is distributed in the hope that it will be useful,
## but WITHOUT ANY WARRANTY; without even the implied warranty of
## MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
## GNU General Public License for more details.
##
## You should have received a copy of the GNU General Public License
## along with M2-Planet.  If not, see <http://www.gnu.org/licenses/>.

# Set the folling variables:
# BUILDDIR for where the tempfiles are to go
# BINDIR for where the final binary is to go.
# BLOOD_FLAG for any arguments blood-elf might need
# ARCH for the system architecture
# ENDIAN_FLAG for the system endianness
# BASE_ADDRESS for the system base address
# TOOLS for where the tools needed to build are located
# M2LIBC for where M2libc files are located

# Bootstrap build
${TOOLS}/M2-Planet --architecture ${ARCH} \
	-f ${M2LIBC}/sys/types.h \
	-f ${M2LIBC}/stddef.h \
	-f ${M2LIBC}/${ARCH}/Linux/unistd.h \
	-f ${M2LIBC}/${ARCH}/Linux/fcntl.h \
	-f ${M2LIBC}/stdlib.c \
	-f ${M2LIBC}/stdio.c \
	-f ${M2LIBC}/bootstrappable.c \
	-f ${M2LIBC}/string.c \
	-f cc.h \
	-f cc_globals.c \
	-f cc_reader.c \
	-f cc_core.c \
	-f cc_macro.c \
	-f cc_env.c \
	-f cc_spawn.c \
	-f cc.c \
	--debug \
	-o ${BUILDDIR}/M2-Mesoplanet.M1

${TOOLS}/blood-elf \
	-f ${BUILDDIR}/M2-Mesoplanet.M1 \
	${ENDIAN_FLAG} \
	--entry _start \
	-o ${BUILDDIR}/M2-Mesoplanet-footer.M1 ${BLOOD_FLAG}

${TOOLS}/M1 \
	-f ${M2LIBC}/${ARCH}/${ARCH}_defs.M1 \
	-f ${M2LIBC}/${ARCH}/libc-full.M1 \
	-f ${BUILDDIR}/M2-Mesoplanet.M1 \
	-f ${BUILDDIR}/M2-Mesoplanet-footer.M1 \
	${ENDIAN_FLAG} \
	--architecture ${ARCH} \
	-o ${BUILDDIR}/M2-Mesoplanet.hex2

${TOOLS}/hex2 \
	-f ${M2LIBC}/${ARCH}/ELF-${ARCH}-debug.hex2 \
	-f ${BUILDDIR}/M2-Mesoplanet.hex2 \
	${ENDIAN_FLAG} \
	--architecture ${ARCH} \
	--base-address ${BASE_ADDRESS} \
	-o ${BINDIR}/M2-Mesoplanet
