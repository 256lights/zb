-- Copyright 2024 The zb Authors
-- SPDX-License-Identifier: MIT

---@meta

loadfile = nil
dofile = nil

---@class derivation: userdata
---@field name string
---@field system string
---@field builder string
---@field args string[]
---@field drvPath string
---@field out string
---@field [string] string|number|boolean|derivation|(string|number|boolean|derivation)[]
---@operator concat:string

---Create a derivation (a buildable target).
---@param args { name: string, system: string, builder: string, args: string[], [string]: string|number|boolean|(string|number|boolean)[] }
---@return derivation
function derivation(args) end

--- Force a module to load.
--- @param x (any)
--- @return any
function await(x) end

--- Import a Lua file.
--- @param path (string)
--- @return any
function import(path) end

---Make a file or directory available to a derivation.
---@param p (string|{path: string, name: string?, filter: (fun(name: string, type: "regular"|"directory"|"symlink"): boolean)?}) path to import, relative to the source file that called `path`
---@return string # store path of the copied file or directory
function path(p) end

---Adds a dependency on an existing store path.
---If the store object named by the path does not exist in the store,
---storePath raises an error.
---@param path string
---@return string
function storePath(path) end

---Store a plain file in the store.
---@param name string
---@param s string File contents
---@return string # store path
function toFile(name, s) end

---Create a derivation that downloads a URL.
---@param args {url: string, hash: string, name: string?, executable: boolean?}
---@return derivation
function fetchurl(args) end

---Create a derivation that extracts an archive.
---The source must be one of the following formats:
--- - .tar
--- - .tar.gz
--- - .tar.bz2
--- - .zip
---
---If stripFirstComponent is true (the default),
---then the root directory is stripped during extraction.
---@param args string|{src: string, name: string?, stripFirstComponent: boolean?}
---@return derivation
function extract(args) end

---Create a derivation that extracts an archive from a URL.
---This is a convenience wrapper around fetchurl and extract.
---@param args {url: string, hash: string, name: string?, stripFirstComponent: boolean?}
---@return derivation
function fetchArchive(args) end

os = {}

---Returns the value of the process environment variable `varname`
---or `nil` if the variable is not defined.
---@param varname string
---@return string|nil
function os.getenv(varname) end
