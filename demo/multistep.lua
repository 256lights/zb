-- Copyright 2024 The zb Authors
-- SPDX-License-Identifier: MIT

hello = import("hello_linux.lua")

hello2 = derivation {
  name = "hello2";
  ["in"] = hello.out;
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
