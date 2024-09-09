-- Copyright 2024 Roxy Light
-- SPDX-License-Identifier: MIT

return derivation {
  name = "hello.txt";
  ["in"] = path "hello.txt";
  builder = "/bin/sh";
  system = "x86_64-linux";
  args = {"-c", "while read line; do echo \"$line\"; done < $in > $out"};
}
