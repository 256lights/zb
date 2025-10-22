# Backend Database Schema Design

This directory contains the schema for the SQLite database used by the backend.
The database is used to store the following:

- Store object metadata, including:
  - Its content address (which allows for integrity checks)
  - References to other store objects
- [Realizations][] (a mapping from derivation outputs to store objects).
  Realizations are recorded for each build performed by this backend,
  as well as for realizations imported from other stores.
- [Realization signatures][].
  Similarly, signatures are typically recorded for each build by this backend,
  as well as for realizations imported from other stores.
- Ongoing and finished builds.
  The backend RPC interface gives the ability to query for these.
  The backend process holds additional in-memory state for ongoing builds.
  If the database has a record of a build that has not finished
  but the backend process does not have a record of such a build,
  then the backend process assumes that such a build is orphaned from a previous backend process (likely a crash).

This document's purpose is to describe the scope of the backend database
and how its data is structured.

[Realizations]: https://zb.256lights.llc/binary-cache/realizations
[Realization signatures]: https://zb.256lights.llc/binary-cache/realizations#signatures

## String Tables

There are a few tables that exist solely to save space on disk.
These tables hold a potentially arbitrary-sized or repeated string
and assign an integer ID to them.
The current list of such tables are:

- `paths`
- `drv_hashes`
- `signature_public_keys`

## The `objects` Table

The `objects` table is used to provide a listing of all store objects that are in the store
along with their metadata.
However, the `objects` table does not include the store object itself:
that is stored in the local filesystem.
Because of this split nature,
there are two sources that must be consulted to answer the question,
"does this store object exist in the store?":
the store directory in the local filesystem and the database.
The store object must exist in both sources for the store object to exist.
Despite the dual nature of a store object,
each individual piece of information about a store object has a single source of truth.
The store directory is the source of truth of store object contents,
although its integrity is verified by the content address stored in the database.
If a store object's files in the store directory do not match the content address stored in the database,
then the object is considered corrupt
and may be removed during maintenance tasks.
The database is the source of truth for any information about the store object other than the file contents.

## Derivation Hashes

[Derivation hashes][] are stored in the database as a portion of the primary key for realizations.
However, derivation hashes for the [`.drv` files][] in the store
are not stored inside the database.
The backend process generates derivation hashes on the fly
because derivation hashes are dependent on derivation outputs selected during a realization.

[Derivation hashes]: https://zb.256lights.llc/binary-cache/realizations#derivation-hashes
[`.drv` files]: https://zb.256lights.llc/derivations

## Lessons Learned

### Logs

An early version of zb would store logs in the database.

- CPU overhead of SQLite during I/O
- Write contention
- Large database file
- More user visible. Pro and con: they can manage, but also we have to consider whether the user deleted or moved files around.
