-- Copyright 2023 Ross Light
--
-- Permission is hereby granted, free of charge, to any person obtaining a copy of
-- this software and associated documentation files (the “Software”), to deal in
-- the Software without restriction, including without limitation the rights to
-- use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
-- the Software, and to permit persons to whom the Software is furnished to do so,
-- subject to the following conditions:
--
-- The above copyright notice and this permission notice shall be included in all
-- copies or substantial portions of the Software.
--
-- THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
-- IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
-- FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
-- COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
-- IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
-- CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
--
-- SPDX-License-Identifier: MIT

-- Writing
do
  local f = assert(io.open("foo.txt", "w"))
  local wresult = assert(f:write("Hello, ", 42, "!\n"))
  assert(wresult == f, "write result is "..tostring(wresult))
  wresult = assert(f:write("second line\n"))
  assert(wresult == f, "write result is "..tostring(wresult))
  assert(f:close())
end

-- Reading
local lines = {
  "Hello, 42!",
  "second line",
}
do
  local f = assert(io.open("foo.txt"))
  local line1 = assert(f:read())
  assert(line1 == lines[1])
  local rest = assert(f:read("a"))
  assert(rest == lines[2].."\n")
  assert(f:close())
end

-- Lines
do
  local i = 1
  for line in io.lines("foo.txt") do
    assert(i <= #lines, "Too many lines")
    assert(lines[i] == line, "line "..i..": "..line)
    i = i + 1
  end
end

-- Seeking
do
  local f = assert(io.open("foo.txt", "r+"))
  local firstBytes = assert(f:read(2))
  assert(firstBytes == "He")
  local pos = assert(f:seek("cur", #lines[1] + 1 - #firstBytes))
  assert(pos == (#lines[1] + 1))
  lines[2] = "this is a very long line"
  assert(f:write(lines[2].."\n"))
  pos = assert(f:seek("set"))
  assert(pos == 0)
  local result = assert(f:read("a"))
  local want = lines[1].."\n"..lines[2].."\n"
  assert(result == want)
  pos = assert(f:seek("cur"))
  assert(pos == #want)
end
