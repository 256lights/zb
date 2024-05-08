This folder contains the AMD64 specific parts of stage0-posix and was separated out to make integration in other projects in easier.

To use this in your project:
1) add it as a git submodule (or just extract into a folder) named AMD64
2) create a kaem.amd64 file (if you are using bootstrap-seeds)
3) create an after.kaem file to hook your tools you wish to have built after these

The master location of this code is: https://github.com/oriansj/stage0-posix-amd64
