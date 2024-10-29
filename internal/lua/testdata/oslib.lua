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

-- Test suite for this package's os library implementation.

-- os.date
assert(os.date() == "Sun Sep 24 13:58:07 2023")
assert(os.date("!%c") == "Sun Sep 24 20:58:07 2023")
local timeTable = os.date("*t")
assert(timeTable.year == 2023)
assert(timeTable.month == 9)
assert(timeTable.day == 24)
assert(timeTable.hour == 13)
assert(timeTable.min == 58)
assert(timeTable.sec == 7)
assert(timeTable.wday == 1)
assert(timeTable.yday == 267)
assert(timeTable.isdst == true or timeTable.isdist == nil)

-- os.execute
local success, exitType, exitStatus = os.execute("true")
assert(success)
assert(exitType == "exit")
assert(exitStatus == 0)
success, exitType, exitStatus = os.execute("false")
assert(not success)
assert(exitType == "exit")
assert(exitStatus == 1)

-- os.getenv
assert(os.getenv("FOO") == "BAR")
assert(os.getenv("EMPTY") == "")
assert(not os.getenv("BORK"))

-- os.remove
assert(os.remove("foo.txt"))
assert(not os.remove("doesnotexist"))

-- os.rename
assert(os.rename("old", "new"))
assert(not os.rename("doesnotexist", "foo"))

-- os.time and os.difftime
local t1 = os.time()
assert(type(t1) == "number")
local t2 = os.time{
  year = 2023, month = 9, day = 24,
  hour = 13, min = 1, sec = 0,
}
assert(type(t2) == "number")
local dt = os.difftime(t1, t2)
assert(dt == 3427)

assert(os.date(nil, t2) == "Sun Sep 24 13:01:00 2023")
