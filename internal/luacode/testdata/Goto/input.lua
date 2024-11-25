:: beginning ::
while true do
  if x == "foo" then
    -- If then break special case.
    break
  elseif x == "bar" then
    goto beginning
  elseif x == "goforward" then
    goto forward
  end

  print("Do a thing")

  :: forward ::
  if y == "stop" then
    -- General break.
    print("I was told to stop.")
    break
  end
end
