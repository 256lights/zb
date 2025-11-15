# Backend implementation notes

## Build Algorithm

1. Load the transitive closure of derivations requested into memory.
   These should all exist on disk because the edges are store references.

1. **Gather existing realizations.**
   Walk the dependency graph,
   selecting realizations and hashing derivations.
   Fixed-output derivations can have realizations generated for them on the fly regardless of reuse policy,
   since their derivation hash is purely based on their output hash.
   New realizations may be downloaded from the fallback store
   if an output does not have any trusted realizations
   or an output does not have any local store paths.
   Realizations whose output store object does not exist are permitted.
   This step produces a set of realizations
   and a map of derivations to derivation hashes.

1. **Obtain missing store objects.**
   Walk the dependency graph in breadth-first order.
   If we encounter an output that has a realization recorded in the previous step
   whose output store object does not exist in the store,
   attempt to download it from the fallback store.

    - If the download succeeds, don't walk any dependencies.
    - If the download fails, then ignore the realizations we collected for this derivation
      and any realizations that transitively depend on the derivation.
      (We do this for the full derivation to avoid complexities with multi-output derivations.)
      We also ignore any derivation hash of any derivation
      that transitively depends on the derivation.
      Then we walk the dependencies of the derivation
      whose output store object does not exist.

1. **Build what remains.**
   For each derivation that must be built to satisfy the build request
   whose build inputs have recorded realizations:

    1. Find any realizations whose output store objects exist in the store
       and are compatible with existing realizations.
       (This accounts for any concurrent builds.)
       If the derivation has multiple outputs that are needed for the build,
       then all of the derivation's outputs (not just the ones requested)
       must have acceptable realizations and be present in the store.
    2. If there is an acceptable realization, then use it.
       Otherwise, run the builder and record the realization(s) on success.

## Store Concurrency

- The backend assumes at most one backend is running per-store-directory.
- The backend generally assumes that no other process will write to the store directory.
  (However, it is a bit more defensive about testing this assumption:
  if a store object is obviously missing or corrupted, it will complain.)
- The backend assumes that if and only if a store object is present in the store directory
  will a corresponding row exist in the `objects` table.
- The `inProgress` lock for a store path is acquired before accessing any store object.
  The lock is held while importing or accessing a store object.
  During access, the lock is released once it has been determined that the object exists.
  (Code may assume that if a store object exists at this time,
  it is fully constructed because of the previous bullet.)
  During import of a store object, the lock is released once it has been written to the filesystem
  and the row has been written to the `objects` table.
