# zb Administrator Guide

This document provides an overview of how the zb server operates.

## Architecture

zb uses a client/server architecture for all builds.
When a user runs `zb build` (or similar invocations),
the zb program acts as a client that connects to a store server — the `zb serve` command.
Typically, a machine runs a single store server.
The store server manages a single store directory, which contains build artifacts.
By default, this store directory is located at:

- `/opt/zb/store` on Linux and macOS
- `C:\zb\store` on Windows

The store directory can be overridden with the `ZB_STORE_DIR` environment variable,
but changing this is discouraged.
Build artifacts can only be shared among store servers with the same store directory path
because build artifacts can contain references to other build artifacts.
For example, a program in one build artifact
may depend on a shared library in another build artifact.
A consistent store directory setting is critical for build reuse.

A zb client communicates with the store server using an [RPC protocol][].
By default, it expects a store server running on the local machine on a Unix domain socket.
The default path of this socket is:

- `/opt/zb/var/zb/server.sock` on Linux and macOS
- `C:\zb\var\zb\server.sock` on Windows

The socket used can be overridden with the `ZB_STORE_SOCKET` environment variable.

[RPC protocol]: ../internal/zbstorerpc/README.md

## Storage

As mentioned previously, a store server manages a single store directory.
Along with the build artifacts themselves,
zb must maintain metadata about the build artifacts and their relationships.
If this metadata is lost, zb is unable to use the store artifacts.
Such metadata is stored in a [SQLite][] database.
The default path of this database is:

- `/opt/zb/var/zb/db.sqlite` on Linux and macOS
- `C:\zb\var\zb\db.sqlite` on Windows

The database used can be overridden with the `zb serve --db` flag.
The exact schema of this database and its contents is considered internal
and may change from release to release.

zb also stores builder logs inside its database.
These logs are periodically deleted to reclaim space.
The exact interval can be configured using the `zb serve --build-log-retention` flag.

[SQLite]: https://www.sqlite.org/

## Sandboxing and Permissions

**TODO(soon):** This part of zb is still under construction
and it is expected that the details will change.

## Graphical User Interface

A zb server can optionally run a web server that provides a graphical user interface (GUI).
This GUI allows viewing the status of running builds and inspection of the store.
Administrators can enable the GUI by passing a flag like `--ui=localhost:8080` to `zb serve`.
Once the server has started, the user can view the GUI by visiting `http://localhost:8080`
in a web browser on the same machine as `zb serve`.

For security reasons, connections are only permitted from the local machine by default,
even if the address given to the `zb serve --ui` flag specifies an external interface.
The `zb serve --allow-remote-ui` disables this protection.
