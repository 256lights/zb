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

The master repository for this work is located at:
https://savannah.nongnu.org/projects/mescc-tools

# If you wish to contribute:
pull requests can be made at https://github.com/oriansj/mescc-tools
and https://gitlab.com/janneke/mescc-tools
or patches/diffs can be sent via email to Jeremiah (at) pdp10 [dot] guru
or join us on libera.chat's #bootstrappable

These are a collection of tools written for use in bootstrapping

# blood-elf
A tool for generating ELF debug tables in M1-macro format from M1-macro assembly files

# exec_enable
A tool for marking files as executable, for systems that don't have chmod

# get_machine
A tool for identifying what hardware architecture you are running on

# kaem
A minimal shell script build tool that can be used for running shell scripts on
systems that lack any shells.

# hex2_linker
The trivially bootstrappable linker that is designed to be introspectable by
humans and should you so desire assemble hex programs that you write.

# M1-macro
The universal Macro assembler that can target any reasonable hardware architecture.


With these tools on your system, you can always bootstrap real programs; even in
the most catastrophic of situations, provided you keep your cool.
