-- Copyright 2024 The zb Authors
-- SPDX-License-Identifier: MIT

local system = "x86_64-linux"
local shell = "/bin/sh"
local src = path "AMD64"

local stage0 = {}
stage0.hex0 = path "bootstrap-seeds/POSIX/AMD64/hex0-seed"

---@param args {name: string, command: string, [string]: string|number|boolean|(string|number|boolean)[]}
local function shDerivation(args)
  ---@type table<string, any>
  local drvArgs = { system = system }
  for k, v in pairs(args) do
    drvArgs[k] = v
  end
  -- TODO(someday): Use a kaem shell modified to support $out.
  drvArgs.builder = shell
  drvArgs.args = { "-c", args.command }
  return derivation(drvArgs)
end

stage0.hex1 = shDerivation {
  name = "hex1";
  command = stage0.hex0.." "..src.."/hex1_AMD64.hex0 $out";
}

stage0["hex2-0"] = shDerivation {
  name = "hex2-0";
  command = stage0.hex1.." "..src.."/hex2_AMD64.hex1 $out";
}

---@param name string
---@param src string|derivation
local function hex2_0(name, src)
  return shDerivation {
    name = name;
    command = stage0["hex2-0"].." "..src.." $out";
  }
end

stage0.catm = hex2_0("catm", src.."/catm_AMD64.hex2")

---@param name string
---@param ... string|derivation
local function catm(name, ...)
  local command = stage0.catm.." $out"
  for i = 1, select("#", ...) do
    command = command.." "..select(i, ...)
  end
  return shDerivation {
    name = name;
    command = command;
  }
end

local elfHeader = src.."/ELF-amd64.hex2"
stage0.M0 = hex2_0("M0", catm("M0.hex2", elfHeader, src.."/M0_AMD64.hex2"))

---@param name string
---@param src string|derivation
local function M0(name, src)
  return shDerivation {
    name = name;
    command = stage0.M0.." "..src.." $out";
  }
end

stage0.cc_amd64 = hex2_0("cc_amd64", catm("cc_amd64-0.hex2",
  elfHeader,
  M0("cc_amd64.hex2", src.."/cc_amd64.M1")
))

---@param name string
---@param src string|derivation
local function cc(name, src)
  return shDerivation {
    name = name;
    command = stage0.cc_amd64.." "..src.." $out";
  }
end

-- Phase-5 Build M2-Planet from cc_amd64
stage0.M2libc = path "M2libc"
local m2_planet_src = path "M2-Planet"
local m2_0_m1 = cc("M2-0.M1", catm("M2-0.c",
  stage0.M2libc.."/amd64/linux/bootstrap.c",
  m2_planet_src.."/cc.h",
  stage0.M2libc.."/bootstrappable.c",
  m2_planet_src.."/cc_globals.c",
  m2_planet_src.."/cc_reader.c",
  m2_planet_src.."/cc_strings.c",
  m2_planet_src.."/cc_types.c",
  m2_planet_src.."/cc_core.c",
  m2_planet_src.."/cc_macro.c",
  m2_planet_src.."/cc.c"
))
local m2_0_hex2 = M0("M2-0.hex2", catm("M2-0-0.M1",
  src.."/amd64_defs.M1",
  src.."/libc-core.M1",
  m2_0_m1
))
stage0.M2 = hex2_0("M2", catm("M2-0-0.hex2", elfHeader, m2_0_hex2))

---@param args {name: string, srcs: string[], debug: boolean?, bootstrap: boolean?}
local function M2(args)
  local command = stage0.M2.." --architecture amd64"
  for _, f in pairs(args.srcs) do
    command = command.." -f "..f
  end
  if args.bootstrap then
    command = command.." --bootstrap-mode"
  end
  if args.debug then
    command = command.." --debug"
  end
  command = command.." -o $out"
  return shDerivation {
    name = args.name;
    command = command;
  }
end

-- Phase-6 Build blood-elf-0 from C sources
local mescc_tools_src = path "mescc-tools"
local blood_elf_0_m1 = M2 {
  name = "blood-elf-0.M1";
  srcs = {
    stage0.M2libc.."/amd64/linux/bootstrap.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_src.."/stringify.c",
    mescc_tools_src.."/blood-elf.c",
  };
  bootstrap = true;
}
local blood_elf_0_hex2 = M0("blood-elf-0.hex2", catm("blood-elf-0-0.M1",
  src.."/amd64_defs.M1",
  src.."/libc-core.M1",
  blood_elf_0_m1
))
stage0["blood-elf-0"] = hex2_0("blood-elf-0", catm("blood-elf-0-0.hex2",
  stage0.M2libc.."/amd64/ELF-amd64.hex2",
  blood_elf_0_hex2
))

---@param name string
---@param src string|derivation
---@param bloodelf derivation?
local function bloodelf(name, src, bloodelf)
  bloodelf = bloodelf or stage0["blood-elf-0"]
  return shDerivation {
    name = name;
    command = bloodelf.." \z
      --64 --little-endian \z
      -f "..src.." \z
      -o $out";
  }
end

-- Phase-7 Build M1-0 from C sources
local m1_macro_0_m1 = M2 {
  name = "M1-macro-0.M1";
  srcs = {
    stage0.M2libc.."/amd64/linux/bootstrap.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_src.."/stringify.c",
    mescc_tools_src.."/M1-macro.c",
  };
  bootstrap = true;
  debug = true;
}
local m1_macro_0_hex2 = M0("M1-macro-0.hex2", catm("M1-macro-0-0.M1",
  stage0.M2libc.."/amd64/amd64_defs.M1",
  stage0.M2libc.."/amd64/libc-core.M1",
  m1_macro_0_m1,
  bloodelf("m1-macro-0-footer.m1", m1_macro_0_m1)
))
stage0["M1-0"] = hex2_0("M1-0", catm("M1-macro-0-0.hex2",
  stage0.M2libc.."/amd64/ELF-amd64-debug.hex2",
  m1_macro_0_hex2
))

---@param args {name: string, srcs: (string|derivation)[], M1: derivation?}
local function M1(args)
  local M1 = stage0["M1-0"] or M1
  local command = M1.." --architecture amd64 --little-endian"
  for _, f in pairs(args.srcs) do
    command = command.." -f "..f
  end
  command = command.." -o $out"
  return shDerivation {
    name = args.name;
    command = command;
  }
end

-- Phase-8 Build hex2-1 from C sources
local hex2_linker_1_m1 = M2 {
  name = "hex2_linker-1.M1";
  debug = true;
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/amd64/linux/sys/stat.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_src.."/hex2.h",
    mescc_tools_src.."/hex2_linker.c",
    mescc_tools_src.."/hex2_word.c",
    mescc_tools_src.."/hex2.c",
  };
}
local hex2_linker_1_hex2 = M1 {
  name = "hex2_linker-1.hex2";
  srcs = {
    stage0.M2libc.."/amd64/amd64_defs.M1",
    stage0.M2libc.."/amd64/libc-full.M1",
    hex2_linker_1_m1,
    bloodelf("hex2_linker-1-footer.M1", hex2_linker_1_m1),
  };
}
stage0["hex2-1"] = hex2_0("hex2-1", catm("hex2_linker-1-0.hex2",
  stage0.M2libc.."/amd64/ELF-amd64-debug.hex2",
  hex2_linker_1_hex2
))

---@param args {name: string, srcs: (string|derivation)[], hex2: derivation?}
local function hex2(args)
  local hex2 = args.hex2 or stage0["hex2-1"]
  local command = hex2.." --architecture amd64 --little-endian --base-address 0x00600000"
  for _, f in pairs(args.srcs) do
    command = command.." -f "..f
  end
  command = command.." -o $out"
  return shDerivation {
    name = args.name;
    command = command;
  }
end

---@param args {name: string, iname: string, srcs: (string|derivation)[], M1: derivation?, hex2: derivation?, bloodelf: derivation?}
local function cprogram(args)
  local asm = M2 {
    name = args.iname..".M1";
    debug = true;
    srcs = args.srcs;
  }
  local obj = M1 {
    name = args.iname..".hex2";
    M1 = args.M1;
    srcs = {
      stage0.M2libc.."/amd64/amd64_defs.M1",
      stage0.M2libc.."/amd64/libc-full.M1",
      asm,
      bloodelf(args.iname.."-footer.M1", asm, args.bloodelf),
    };
  }
  return hex2 {
    name = args.name;
    hex2 = args.hex2;
    srcs = {
      stage0.M2libc.."/amd64/ELF-amd64-debug.hex2",
      obj,
    };
  }
end

-- Phase-9 Build M1 from C sources
stage0.M1 = cprogram {
  name = "M1";
  iname = "M1-macro-1";
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/string.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_src.."/stringify.c",
    mescc_tools_src.."/M1-macro.c",
  };
}

-- Phase-10 Build hex2 from C sources
stage0.hex2 = cprogram {
  name = "hex2";
  iname = "hex2_linker-2";
  M1 = stage0.M1;
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/amd64/linux/sys/stat.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_src.."/hex2.h",
    mescc_tools_src.."/hex2_linker.c",
    mescc_tools_src.."/hex2_word.c",
    mescc_tools_src.."/hex2.c",
  };
}

-- Phase-11 Build kaem from C sources
stage0.kaem = cprogram {
  name = "kaem";
  iname = "kaem";
  M1 = stage0.M1;
  hex2 = stage0.hex2;
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/string.c",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_src.."/Kaem/kaem.h",
    mescc_tools_src.."/Kaem/variable.c",
    mescc_tools_src.."/Kaem/kaem_globals.c",
    mescc_tools_src.."/Kaem/kaem.c",
  };
}

---@param args {name: string, script: string, [string]: boolean|number|string|derivation|(boolean|number|string|derivation)[]}
local function kaem(args)
  ---@type table<string, any>
  local drvArgs = { system = system }
  for k, v in pairs(args) do
    if k ~= "script" then
      drvArgs[k] = v
    end
  end
  drvArgs.builder = shell
  ---@diagnostic disable-next-line: param-type-mismatch
  drvArgs.args = { "-f", toFile(args.name.."-builder.sh", args.script) }
  return derivation(drvArgs)
end

-- Phase-12 Build M2-Mesoplanet from M2-Planet
local m2_mesoplanet_src = path "M2-Mesoplanet"
stage0["M2-Mesoplanet"] = cprogram {
  name = "M2-Mesoplanet";
  iname = "m2-Mesoplanet-1";
  M1 = stage0.M1;
  hex2 = stage0.hex2;
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/amd64/linux/sys/stat.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/string.c",
    stage0.M2libc.."/bootstrappable.c",
    m2_mesoplanet_src.."/cc.h",
    m2_mesoplanet_src.."/cc_globals.c",
    m2_mesoplanet_src.."/cc_env.c",
    m2_mesoplanet_src.."/cc_reader.c",
    m2_mesoplanet_src.."/cc_spawn.c",
    m2_mesoplanet_src.."/cc_core.c",
    m2_mesoplanet_src.."/cc_macro.c",
    m2_mesoplanet_src.."/cc.c",
  };
}

-- Phase-13 Build final blood-elf from C sources
stage0["blood-elf"] = cprogram {
  name = "blood-elf";
  iname = "blood-elf-1";
  M1 = stage0.M1;
  hex2 = stage0.hex2;
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_src.."/stringify.c",
    mescc_tools_src.."/blood-elf.c",
  };
}

-- Phase-14 Build get_machine from C sources
stage0.get_machine = cprogram {
  name = "get_machine";
  iname = "get_machine";
  M1 = stage0.M1;
  hex2 = stage0.hex2;
  bloodelf = stage0["blood-elf"];
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_src.."/get_machine.c",
  };
}

-- Phase-15 Build M2-Planet from M2-Planet
stage0["M2-Planet"] = cprogram {
  name = "M2-Planet";
  iname = "M2-1";
  M1 = stage0.M1;
  hex2 = stage0.hex2;
  bloodelf = stage0["blood-elf"];
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/bootstrappable.c",
    m2_planet_src.."/cc.h",
    m2_planet_src.."/cc_globals.c",
    m2_planet_src.."/cc_reader.c",
    m2_planet_src.."/cc_strings.c",
    m2_planet_src.."/cc_types.c",
    m2_planet_src.."/cc_core.c",
    m2_planet_src.."/cc_macro.c",
    m2_planet_src.."/cc.c",
  };
}

-- Remaining phases are programs in mescc-tools-extra.
-- All are intended to be built with M2-Mesoplanet.

-- We go a little off-script to start,
-- since M2-Mesoplanet expects that the tools are in a PATH.
-- We need some basic file utilities before that's possible.
local mescc_tools_extra_src = path "mescc-tools-extra"

local mkdir = cprogram {
  name = "mkdir-0";
  iname = "mkdir-0";
  M1 = stage0.M1;
  hex2 = stage0.hex2;
  bloodelf = stage0["blood-elf"];
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/string.c",
    stage0.M2libc.."/sys/stat.h",
    stage0.M2libc.."/amd64/linux/sys/stat.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_extra_src.."/mkdir.c",
  };
}

local cp = cprogram {
  name = "cp-0";
  iname = "cp-0";
  M1 = stage0.M1;
  hex2 = stage0.hex2;
  bloodelf = stage0["blood-elf"];
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/string.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_extra_src.."/cp.c",
  };
}

local chmod = cprogram {
  name = "chmod-0";
  iname = "chmod-0";
  M1 = stage0.M1;
  hex2 = stage0.hex2;
  bloodelf = stage0["blood-elf"];
  srcs = {
    stage0.M2libc.."/sys/types.h",
    stage0.M2libc.."/stddef.h",
    stage0.M2libc.."/sys/utsname.h",
    stage0.M2libc.."/amd64/linux/unistd.c",
    stage0.M2libc.."/amd64/linux/fcntl.c",
    stage0.M2libc.."/fcntl.c",
    stage0.M2libc.."/stdlib.c",
    stage0.M2libc.."/stdio.h",
    stage0.M2libc.."/stdio.c",
    stage0.M2libc.."/string.c",
    stage0.M2libc.."/sys/stat.h",
    stage0.M2libc.."/amd64/linux/sys/stat.c",
    stage0.M2libc.."/bootstrappable.c",
    mescc_tools_extra_src.."/chmod.c",
  };
}

local bindir_tool_names = {
  "M1",
  "hex2",
  "blood-elf",
  "kaem",
  "M2-Mesoplanet",
  "get_machine",
  "M2-Planet",
}
local bindir_chmod_args = "555"
for _, name in ipairs(bindir_tool_names) do
  bindir_chmod_args = bindir_chmod_args.." $out/bin/"..name
end
local bindir = kaem {
  name = "stage0-bin";

  cp = cp;
  chmod = chmod;
  mkdir = mkdir;
  M1 = stage0.M1;
  hex2 = stage0.hex2;
  bloodelf = stage0["blood-elf"];
  kaem = stage0.kaem;
  M2_Mesoplanet = stage0["M2-Mesoplanet"];
  M2_Planet = stage0["M2-Planet"];
  get_machine = stage0.get_machine;

  script = [[
$mkdir $out $out/bin
$cp $M1 $out/bin/M1
$cp $hex2 $out/bin/hex2
$cp $bloodelf $out/bin/blood-elf
$cp $kaem $out/bin/kaem
$cp $M2_Mesoplanet $out/bin/M2-Mesoplanet
$cp $get_machine $out/bin/get_machine
$cp $M2_Planet $out/bin/M2-Planet

$chmod ]]..bindir_chmod_args.."\n";
}

-- Okay, back on script.
local bindir_cp = ""
for _, name in ipairs(bindir_tool_names) do
  bindir_cp = bindir_cp.."${out}/bin/cp ${PATH}/"..name.." ${out}/bin/"..name.."\n"
end

stage0.stage0 = kaem {
  name = "stage0";

  OPERATING_SYSTEM = "Linux";
  ARCH = "x86"; -- stage0-posix unconditionally sets this.
  PATH = bindir.."/bin";
  M2LIBC_PATH = stage0.M2libc;
  EXE_SUFFIX = "";

  extra = mescc_tools_extra_src;
  mkdir = mkdir;

  script = [[
alias CC="${PATH}/M2-Mesoplanet${EXE_SUFFIX} --operating-system ${OPERATING_SYSTEM} --architecture ${ARCH} -f"

$mkdir ${out} ${out}/bin

CC ${extra}/sha256sum.c -o ${out}/bin/sha256sum${EXE_SUFFIX}
CC ${extra}/match.c -o ${out}/bin/match${EXE_SUFFIX}
CC ${extra}/mkdir.c -o ${out}/bin/mkdir${EXE_SUFFIX}
CC ${extra}/untar.c -o ${out}/bin/untar${EXE_SUFFIX}
CC ${extra}/ungz.c -o ${out}/bin/ungz${EXE_SUFFIX}
CC ${extra}/unbz2.c -o ${out}/bin/unbz2${EXE_SUFFIX}
CC ${extra}/unxz.c -o ${out}/bin/unxz${EXE_SUFFIX}
CC ${extra}/catm.c -o ${out}/bin/catm${EXE_SUFFIX}
CC ${extra}/cp.c -o ${out}/bin/cp${EXE_SUFFIX}
CC ${extra}/chmod.c -o ${out}/bin/chmod${EXE_SUFFIX}
CC ${extra}/rm.c -o ${out}/bin/rm${EXE_SUFFIX}
CC ${extra}/replace.c -o ${out}/bin/replace${EXE_SUFFIX}
CC ${extra}/wrap.c -o ${out}/bin/wrap${EXE_SUFFIX}

]]..bindir_cp.."\n${out}/bin/chmod "..bindir_chmod_args.."\n";
}

return stage0
