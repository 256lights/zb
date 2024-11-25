local n = 10
repeat
  print(n.."...")
  local next = n - 1
  n = next
until next == 0
print("Blastoff!")
