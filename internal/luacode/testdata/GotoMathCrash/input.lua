do
  local function aux (x1, x2)     -- test random for small intervals
    local mark = {}; local count = 0   -- to check that all values appeared
    while true do
      local t = random(x1, x2)
      assert(x1 <= t and t <= x2)
      if not mark[t] then  -- new value
        mark[t] = true
        count = count + 1
        if count == x2 - x1 + 1 then   -- all values appeared; OK
          goto ok
        end
      end
    end
   ::ok::
  end

  aux(-10,0)
  aux(1, 6)
  aux(1, 2)
  aux(1, 13)
  aux(1, 31)
  aux(1, 32)
  aux(1, 33)
  aux(-10, 10)
  aux(-10,-10)   -- unit set
  aux(minint, minint)   -- unit set
  aux(maxint, maxint)   -- unit set
  aux(minint, minint + 9)
  aux(maxint - 3, maxint)
end
