## Copyright (C) 2017 Jeremiah Orians
## Copyright (C) 2020-2021 deesix <deesix@tuta.io>
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

# Prevent rebuilding
PACKAGE = m2-planet

# C compiler settings
CC?=gcc
CFLAGS:=$(CFLAGS) -D_GNU_SOURCE -O0 -std=c99 -ggdb

all: bin/M2-Planet
.NOTPARALLEL:
M2-Planet: bin/M2-Planet

bin/M2-Planet: bin test/results cc.h cc_reader.c cc_strings.c cc_types.c cc_core.c cc.c cc_globals.c cc_globals.h cc_macro.c | bin
	$(CC) $(CFLAGS) \
	M2libc/bootstrappable.c \
	cc_reader.c \
	cc_strings.c \
	cc_types.c \
	cc_core.c \
	cc_macro.c \
	cc.c \
	cc.h \
	cc_globals.c \
	gcc_req.h \
	-o $@

M2-minimal: bin/M2-minimal

bin/M2-minimal: bin test/results cc.h cc_reader.c cc_strings.c cc_types.c cc_core.c cc-minimal.c
	$(CC) $(CFLAGS) \
	M2libc/bootstrappable.c \
	cc_reader.c \
	cc_strings.c \
	cc_types.c \
	cc_core.c \
	cc-minimal.c \
	cc.h \
	cc_globals.c \
	gcc_req.h \
	-o $@

# Clean up after ourselves
.PHONY: clean M2-Planet M2-minimal
clean:
	rm -rf bin/ test/results/
	./test/cleanup_test.sh 0000
	./test/cleanup_test.sh 0001
	./test/cleanup_test.sh 0002
	./test/cleanup_test.sh 0003
	./test/cleanup_test.sh 0004
	./test/cleanup_test.sh 0005
	./test/cleanup_test.sh 0006
	./test/cleanup_test.sh 0007
	./test/cleanup_test.sh 0008
	./test/cleanup_test.sh 0009
	./test/cleanup_test.sh 0010
	./test/cleanup_test.sh 0011
	./test/cleanup_test.sh 0012
	./test/cleanup_test.sh 0013
	./test/cleanup_test.sh 0014
	./test/cleanup_test.sh 0015
	./test/cleanup_test.sh 0016
	./test/cleanup_test.sh 0017
	./test/cleanup_test.sh 0018
	./test/cleanup_test.sh 0019
	./test/cleanup_test.sh 0020
	./test/cleanup_test.sh 0021
	./test/cleanup_test.sh 0022
	./test/cleanup_test.sh 0023
	./test/cleanup_test.sh 0024
	./test/cleanup_test.sh 0025
	./test/cleanup_test.sh 0026
	./test/cleanup_test.sh 0027
	./test/cleanup_test.sh 0028
	./test/cleanup_test.sh 0029
	./test/cleanup_test.sh 0030
	./test/cleanup_test.sh 0100
	./test/cleanup_test.sh 0101
	./test/cleanup_test.sh 0102
	./test/cleanup_test.sh 0103
	./test/cleanup_test.sh 0104
	./test/cleanup_test.sh 0105
	./test/cleanup_test.sh 0106
	./test/cleanup_test.sh 1000

# Directories
bin:
	mkdir -p bin

test/results:
	mkdir -p test/results

# tests
test: bin/M2-Planet | bin test/results
	+make -f makefile-tests --output-sync
	sha256sum -c test/test.answers

# Generate test answers
.PHONY: Generate-test-answers
Generate-test-answers:
	sha256sum test/results/* >| test/test.answers

DESTDIR:=
PREFIX:=/usr/local
bindir:=$(DESTDIR)$(PREFIX)/bin
.PHONY: install
install: bin/M2-Planet
	mkdir -p $(bindir)
	cp $^ $(bindir)

### dist
.PHONY: dist

COMMIT=$(shell git describe --dirty)
TARBALL_VERSION=$(COMMIT:Release_%=%)
TARBALL_DIR:=$(PACKAGE)-$(TARBALL_VERSION)
TARBALL=$(TARBALL_DIR).tar.gz
# Be friendly to Debian; avoid using EPOCH
MTIME=$(shell git show HEAD --format=%ct --no-patch)
# Reproducible tarball
TAR_FLAGS=--sort=name --mtime=@$(MTIME) --owner=0 --group=0 --numeric-owner --mode=go=rX,u+rw,a-s

$(TARBALL):
	(git ls-files					\
	    --exclude=$(TARBALL_DIR);			\
	    echo $^ | tr ' ' '\n')			\
	    | tar $(TAR_FLAGS)				\
	    --transform=s,^,$(TARBALL_DIR)/,S -T- -cf-	\
	    | gzip -c --no-name > $@

dist: $(TARBALL)
