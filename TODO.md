# Shortcuts Taken

This is a list of shortcuts taken for this prototype.

- The Lua `pairs` function should return sorted pairs.
- Paths should be a distinct type. (Maybe?)
- No way to load other Lua files.
- Haven't gone through and sanitized the `_G` functions.
- No other built-in libraries.
  `string.format` specifically would be good,
  but would require some fancy work for contexts.
