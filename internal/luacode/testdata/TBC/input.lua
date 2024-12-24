local meta = {
__close = function (tab, e)
emit(tab.x)
end,
}
local function newThing(x)
return setmetatable({x = x}, meta)
end
local v1 <close> = newThing(1)
local v2 <close> = newThing(2)
local v3 <close> = newThing(3)
