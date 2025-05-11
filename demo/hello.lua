-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

-- For `zb build demo/hello.lua#hello`,
-- zb will first look for `_G.hello`, and if that's nil,
-- then it will look for `_G[system].hello`, where system is the current system triple.

-- Unix build targets.
local unixSystems <const> = {
  "aarch64-apple-macos",
  "aarch64-unknown-linux",
  "x86_64-unknown-linux",
}
for _, system in ipairs(unixSystems) do
  _G[system] = {
    hello = derivation {
      name = "hello.txt";
      ["in"] = path "hello.txt";
      builder = "/bin/sh";
      system = system;
      args = { "-c", "while read line; do echo \"$line\"; done < $in > $out" };
    };
  }
end

-- Windows build target.
_G["x86_64-pc-windows"] = {
  hello = derivation {
    name = "hello.txt";
    ["in"] = path "hello.txt";
    builder = [[C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe]];
    system = "x86_64-pc-windows";
    args = {"-Command", "Copy-Item ${env:in} ${env:out}"};
  };
}
