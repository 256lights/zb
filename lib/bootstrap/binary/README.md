# Binary bootstrap

Files in this directory are used to build the bootstrap C compiler and shell for each platform.
These intentionally use the system's toolchain to produce,
so you will need to either run `zb serve` with `--sandbox=0`,
or include the appropriate tools using `--sandbox-path`.
