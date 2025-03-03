## Copyright (C) 2016 Jeremiah Orians
## Copyright (C) 2020 fosslinux
## This file is part of mescc-tools.
##
## mescc-tools is free software: you an redistribute it and/or modify
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
## along with mescc-tools. If not, see <http://www.gnu.org/licenses/>.

# Prevent rebuilding

all: kaem

CC=gcc
CFLAGS:=$(CFLAGS) -D_GNU_SOURCE -std=c99 -ggdb

kaem: ../bin/kaem
../bin/kaem: kaem.c kaem_globals.c variable.c | bin
	$(CC) $(CFLAGS) kaem.c \
	kaem_globals.c \
	variable.c  \
	../M2libc/bootstrappable.c \
	-o $@

# Always run the tests
.PHONY: test
test: kaem | results
	./test.sh

# Generate test answers
.PHONY: Generate-test-answers
Generate-test-answers:
	sha256sum test/results/* >| test/test.answers

# Clean up after ourselves
.PHONY: clean kaem
clean:
	rm -rf ../bin/
	rm -rf test/results/

results:
	mkdir -p test/results

bin:
	mkdir -p ../bin

DESTDIR:=
PREFIX:=/usr/local
bindir:=$(DESTDIR)$(PREFIX)/bin
.PHONY: install
install: ../bin/kaem
	mkdir -p $(bindir)
	cp $^ $(bindir)
