local function factorial(n, acc)
  acc = acc or 1
  if n == 0 then return acc end
  return factorial(n - 1, acc * n)
end

return factorial(3)
