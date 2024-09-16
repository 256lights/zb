-- Copyright 2024 The zb Authors
-- SPDX-License-Identifier: MIT

local drv1 = dofile("hello.lua")

local drv2 = derivation {
  name = "hello2";
  ["in"] = drv1.out;
  builder = "/bin/sh";
  system = "x86_64-linux";
  args = {"-c", [[
while read line; do
  echo "$line"
done < $in > $out
while read line; do
  echo "$line"
done < $in >> $out
]]};
}

return drv2
