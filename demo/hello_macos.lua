-- Copyright 2024 The zb Authors
-- SPDX-License-Identifier: MIT

return derivation {
  name = "hello.txt";
  ["in"] = path "hello.txt";
  builder = "/bin/sh";
  system = "aarch64-macos";
  args = {"-c", "while read line; do echo \"$line\"; done < $in > $out"};
}
