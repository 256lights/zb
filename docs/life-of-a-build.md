# Life of a Build

**TODO(someday):** This is an outline of a guide.
Tracking progress in [#101](https://github.com/256lights/zb/issues/101).

- Lua files produce `.drv` files and store sources

## Store

- Managed by build server
- One directory + metadata
- Path must be the same on each machine in order to share artifacts
- Each store object is:
  - A filesystem object
  - A name
  - References
  - Type (part of CA)
  - `.drv` files are store objects too
- Realization = process of turning `.drv` files into more store objects
- Store may hold realization signatures, which establishes mapping of `.drv` file to store object outputs
