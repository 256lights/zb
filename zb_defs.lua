---@meta

---@class derivation: userdata
---@field name string
---@field system string
---@field builder string
---@field args string[]
---@field drvPath string
---@field out string
---@field [string] string|number|boolean|derivation|(string|number|boolean|derivation)[]
---@operator concat:string

---@param args { name: string, system: string, builder: string, args: string[], [string]: string|number|boolean|(string|number|boolean)[] }
---@return derivation
function derivation(args) end

---@param p (string|{path: string, name: string?})
---@return string
function path(p) end

---@param name string
---@param s string File contents
---@return string # store path
function toFile(name, s) end

--- baseNameOf returns the last element of path.
--- Trailing slashes are removed before extracting the last element.
--- If the path is empty, baseNameOf returns "".
--- If the path consists entirely of slashes, baseNameOf returns "/".
---@param path string slash-separated path
---@return string
function baseNameOf(path) end

---@param args {url: string, hash: string, name: string?, executable: boolean?}
---@return derivation
function fetchurl(args) end

---Apply the function f to each element in list.
---@generic T, U
---@param f fun(T): U
---@param list T[]
---@return U[]
function table.map(f, list) end

---Reports whether a value equal to x occurs in list xs.
---@generic T
---@param x T
---@param xs T[]
---@return boolean
function table.elem(x, xs) end

---Concatenate lists into a single list.
---@generic T
---@param ... T[]
---@return T[]
function table.concatLists(...) end
