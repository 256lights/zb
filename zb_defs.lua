---@meta

---@class derivation: userdata
---@field name string
---@field system string
---@field builder string
---@field args string[]
---@field drvPath string
---@field out string
---@field [string] string|number|boolean|(string|number|boolean)[]
---@operator concat:string

---@param args { name: string, system: string, builder: string, args: string[], [string]: string|number|boolean|(string|number|boolean)[] }
---@return derivation
function derivation(args) end

--- @param p (string|{path: string, name: string?})
--- @return string
function path(p) end
