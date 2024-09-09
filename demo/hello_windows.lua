-- Copyright 2024 Roxy Light
-- SPDX-License-Identifier: MIT

return derivation {
  name = "hello.txt";
  ["in"] = path "hello.txt";
  builder = [[C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe]];
  system = "x86_64-windows";
  args = {"-Command", "Copy-Item ${env:in} ${env:out}"};
}

