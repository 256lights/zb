# How to Contribute

zb is still in an early stage of development.
If you have ideas or feedback, feel free to [start a discussion](https://github.com/256lights/zb/discussions).
If you like zb, consider giving the repository a star
and/or [sponsoring @zombiezen](https://github.com/sponsors/zombiezen).

Eventually, I would like to accept external code contributions.
([#58](https://github.com/256lights/zb/issues/58) tracks improving this document.)
Until then, I'm using this file to document some arcana around development workflow.

## Local development tips

### Using an alternative store directory

On both client and the server, set the following environment variables:

```shell
# Set zb_store_prefix to whatever you want, then run:
export ZB_STORE_DIR="${zb_store_prefix?}/store"
export ZB_STORE_SOCKET="${zb_store_prefix?}/var/zb/server.sock"`
```

and when running the server, use the `--db` flag:

```shell
zb serve --db "${zb_store_prefix?}/var/zb/db.sqlite"
```

### Using sandboxing

Linux sandboxing is mostly ready, but requires some non-trivial setup,
so I'm not recommending it for early testers to use yet.
(See [#56](https://github.com/256lights/zb/issues/56) for creating an installer to automate this.)
For my own purposes, I'm reusing my Nix installation to establish the sandbox.
Here's an example command:

```shell
nix build --out-link result-busybox 'nixpkgs#busybox-sandbox-shell' &&
sudo zb serve \
  --sandbox \
  --sandbox-path "/bin/sh=$(readlink result-busybox)/bin/busybox" \
  --build-users-group nixbld
```
