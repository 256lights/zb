local system = "x86_64-linux"
local shell = "/bin/sh"
local src = path "AMD64";

local stage0 = {}
stage0.hex0 = path "bootstrap-seeds/POSIX/AMD64/hex0-seed"

---@param args {command: string, [string]: string|number|boolean|(string|number|boolean)[]}
local function shDerivation(args)
  ---@type table<string, any>
  local drvArgs = { system = system; }
  for k, v in pairs(args) do
    drvArgs[k] = v
  end
  -- TODO(someday): Use a kaem shell modified to support $out.
  drvArgs.builder = shell;
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
  local command = stage0.catm.." $out";
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
local m2_libc_src = path "M2libc";
local m2_planet_src = path "M2-Planet";
local m2_0_m1 = cc("M2-0.M1", catm("M2-0.c",
  m2_libc_src.."/amd64/linux/bootstrap.c",
	m2_planet_src.."/cc.h",
	m2_libc_src.."/bootstrappable.c",
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
    m2_libc_src.."/amd64/linux/bootstrap.c",
    m2_libc_src.."/bootstrappable.c",
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
  m2_libc_src.."/amd64/ELF-amd64.hex2",
  blood_elf_0_hex2
))

---@param name string
---@param src string|derivation
local function bloodelf(name, src)
  return shDerivation {
    name = name;
    command = stage0["blood-elf-0"].." \z
      --64 --little-endian \z
      -f "..src.." \z
      -o $out";
  }
end

-- Phase-7 Build M1-0 from C sources
local m1_macro_0_m1 = M2 {
  name = "M1-macro-0.M1";
  srcs = {
    m2_libc_src.."/amd64/linux/bootstrap.c",
    m2_libc_src.."/bootstrappable.c",
    mescc_tools_src.."/stringify.c",
    mescc_tools_src.."/M1-macro.c",
  };
  bootstrap = true;
  debug = true;
}
local m1_macro_0_hex2 = M0("M1-macro-0.hex2", catm("M1-macro-0-0.M1",
  m2_libc_src.."/amd64/amd64_defs.M1",
  m2_libc_src.."/amd64/libc-core.M1",
  m1_macro_0_m1,
  bloodelf("m1-macro-0-footer.m1", m1_macro_0_m1)
))
stage0["M1-0"] = hex2_0("M1-0", catm("M1-macro-0-0.hex2",
  m2_libc_src.."/amd64/ELF-amd64-debug.hex2",
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
    m2_libc_src.."/sys/types.h",
    m2_libc_src.."/stddef.h",
    m2_libc_src.."/amd64/linux/fcntl.c",
    m2_libc_src.."/fcntl.c",
    m2_libc_src.."/sys/utsname.h",
    m2_libc_src.."/amd64/linux/unistd.c",
    m2_libc_src.."/amd64/linux/sys/stat.c",
    m2_libc_src.."/stdlib.c",
    m2_libc_src.."/stdio.h",
    m2_libc_src.."/stdio.c",
    m2_libc_src.."/bootstrappable.c",
    mescc_tools_src.."/hex2.h",
    mescc_tools_src.."/hex2_linker.c",
    mescc_tools_src.."/hex2_word.c",
    mescc_tools_src.."/hex2.c",
  };
}
local hex2_linker_1_hex2 = M1 {
  name = "hex2_linker-1.hex2";
  srcs = {
    m2_libc_src.."/amd64/amd64_defs.M1",
    m2_libc_src.."/amd64/libc-full.M1",
    hex2_linker_1_m1,
    bloodelf("hex2_linker-1-footer.M1", hex2_linker_1_m1),
  };
}
stage0["hex2-1"] = hex2_0("hex2-1", catm("hex2_linker-1-0.hex2",
  m2_libc_src.."/amd64/ELF-amd64-debug.hex2",
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

-- Phase-9 Build M1 from C sources
local m1_macro_1_M1 = M2 {
  name = "M1-macro-1.M1";
  debug = true;
  srcs = {
	  m2_libc_src.."/sys/types.h",
	  m2_libc_src.."/stddef.h",
	  m2_libc_src.."/amd64/linux/fcntl.c",
	  m2_libc_src.."/fcntl.c",
	  m2_libc_src.."/sys/utsname.h",
	  m2_libc_src.."/amd64/linux/unistd.c",
	  m2_libc_src.."/string.c",
	  m2_libc_src.."/stdlib.c",
	  m2_libc_src.."/stdio.h",
	  m2_libc_src.."/stdio.c",
	  m2_libc_src.."/bootstrappable.c",
	  mescc_tools_src.."/stringify.c",
	  mescc_tools_src.."/M1-macro.c",
  };
}
local m1_macro_1_hex2 = M1 {
  name = "M1-macro-1.hex2";
  srcs = {
    m2_libc_src.."/amd64/amd64_defs.M1",
    m2_libc_src.."/amd64/libc-full.M1",
    m1_macro_1_M1,
    bloodelf("M1-macro-1-footer.M1", m1_macro_1_M1),
  };
}
stage0.M1 = hex2 {
  name = "M1";
  srcs = {
    m2_libc_src.."/amd64/ELF-amd64-debug.hex2",
    m1_macro_1_hex2,
  };
}

-- Phase-10 Build hex2 from C sources
local hex2_linker_2_M1 = M2 {
  name = "hex2_linker-2.M1";
  debug = true;
  srcs = {
	  m2_libc_src.."/sys/types.h",
	  m2_libc_src.."/stddef.h",
	  m2_libc_src.."/amd64/linux/fcntl.c",
	  m2_libc_src.."/fcntl.c",
	  m2_libc_src.."/sys/utsname.h",
	  m2_libc_src.."/amd64/linux/unistd.c",
	  m2_libc_src.."/amd64/linux/sys/stat.c",
	  m2_libc_src.."/stdlib.c",
	  m2_libc_src.."/stdio.h",
	  m2_libc_src.."/stdio.c",
	  m2_libc_src.."/bootstrappable.c",
	  mescc_tools_src.."/hex2.h",
	  mescc_tools_src.."/hex2_linker.c",
	  mescc_tools_src.."/hex2_word.c",
	  mescc_tools_src.."/hex2.c",
  };
}
local hex2_linker_2_hex2 = M1 {
  name = "hex2_linker-2.hex2";
  M1 = stage0.M1;
  srcs = {
    m2_libc_src.."/amd64/amd64_defs.M1",
    m2_libc_src.."/amd64/libc-full.M1",
    hex2_linker_2_M1,
    bloodelf("hex2_linker-2-footer.M1", hex2_linker_2_M1),
  };
}
stage0.hex2 = hex2 {
  name = "hex2";
  srcs = {
    m2_libc_src.."/amd64/ELF-amd64-debug.hex2",
    hex2_linker_2_hex2,
  };
}

-- Phase-11 Build kaem from C sources
local kaem_M1 = M2 {
  name = "kaem.M1";
  debug = true;
  srcs = {
	  m2_libc_src.."/sys/types.h",
	  m2_libc_src.."/stddef.h",
	  m2_libc_src.."/string.c",
	  m2_libc_src.."/amd64/linux/fcntl.c",
	  m2_libc_src.."/fcntl.c",
	  m2_libc_src.."/sys/utsname.h",
	  m2_libc_src.."/amd64/linux/unistd.c",
	  m2_libc_src.."/stdlib.c",
	  m2_libc_src.."/stdio.h",
	  m2_libc_src.."/stdio.c",
	  m2_libc_src.."/bootstrappable.c",
	  mescc_tools_src.."/Kaem/kaem.h",
	  mescc_tools_src.."/Kaem/variable.c",
	  mescc_tools_src.."/Kaem/kaem_globals.c",
	  mescc_tools_src.."/Kaem/kaem.c",
  };
}
local kaem_hex2 = M1 {
  name = "kaem.hex2";
  M1 = stage0.M1;
  srcs = {
    m2_libc_src.."/amd64/amd64_defs.M1",
    m2_libc_src.."/amd64/libc-full.M1",
    kaem_M1,
    bloodelf("kaem-footer.M1", kaem_M1),
  };
}
stage0.kaem = hex2 {
  name = "hex2";
  hex2 = stage0.hex2;
  srcs = {
    m2_libc_src.."/amd64/ELF-amd64-debug.hex2",
    kaem_hex2,
  };
}

return stage0
