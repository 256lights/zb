-- Copyright 2023 Roxy Light
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

do
  local f = assert(io.popen("true"))
  local data = assert(f:read("a"))
  assert(data == "")
  assert(f:close())
end

do
  local f = assert(io.popen("sed -e \"/foo/d\" > foo.txt", "w"))
  assert(f:write("hi\nfoo\nbye\n"))
  assert(f:close())

  f = assert(io.open("foo.txt"))
  local data = assert(f:read("a"))
  assert(data == "hi\nbye\n", "content:\n"..data)
end

do
  local f = assert(io.popen("cat /dev/zero"))
  local data = assert(f:read(8))
  assert(data == "\x00\x00\x00\x00\x00\x00\x00\x00")
  assert(not f:close())
end
