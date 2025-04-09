-- Copyright 2025 The zb Authors
-- SPDX-License-Identifier: MIT

local version <const> = "4.14.336"
local module <const> = { version = version }
local getters <const> = {}

local builderScript <const> = [=[#!/usr/bin/env bash
set -e
mkdir "${TEMPDIR}/src"
cd "${TEMPDIR}/src"
tar -xJf "$src" \
  "linux-$version/scripts" \
  "linux-$version/include" \
  "linux-$version/arch/arm/include" \
  "linux-$version/arch/x86/include" \
  "linux-$version/arch/x86/entry"
cd "linux-$version"

rm include/uapi/linux/pktcdvd.h \
    include/uapi/linux/hw_breakpoint.h \
    include/uapi/linux/eventpoll.h \
    include/uapi/linux/atmdev.h \
    include/uapi/asm-generic/fcntl.h \
    arch/x86/include/uapi/asm/mman.h \
    arch/x86/include/uapi/asm/auxvec.h

"${CC:-gcc}" ${CFLAGS:-} ${LDFLAGS:-} -o scripts/unifdef scripts/unifdef.c

case "$system" in
  i686-linux|x86_64-linux)
    arch_dir=x86
    ;;
  aarch64-linux)
    arch_dir=arm
    ;;
esac

base_dir="${PWD}"
for d in include/uapi arch/${arch_dir?}/include/uapi; do
  cd "${d}"
  find . -type d -exec mkdir "$out/include/{}" -p \;
  headers="$(find . -type f -name "*.h")"
  cd "${base_dir}"
  for h in ${headers}; do
    path="$(dirname "${h}")"
    scripts/headers_install.sh "$out/include/${path}" "${d}/${path}" "$(basename "${h}")"
  done
done

for i in types ioctl termios termbits ioctls sockios socket param; do
  cp "$out/include/asm-generic/${i}.h" "$out/include/asm/${i}.h"
done

case "$system" in
  i686-linux|x86_64-linux)
    bash arch/x86/entry/syscalls/syscallhdr.sh \
      arch/x86/entry/syscalls/syscall_32.tbl \
      "$out/include/asm/unistd_32.h" \
      i386
    bash arch/x86/entry/syscalls/syscallhdr.sh \
      arch/x86/entry/syscalls/syscall_64.tbl \
      "$out/include/asm/unistd_64.h" \
      common,64
    ;;
esac

# Generate linux/version.h
# Rules are from makefile
VERSION=4
PATCHLEVEL=14
SUBLEVEL=336
VERSION_CODE="$((VERSION * 65536 + PATCHLEVEL * 256 + SUBLEVEL))"
echo '#define LINUX_VERSION_CODE '"${VERSION_CODE}" \
    > "$out/include/linux/version.h"
echo '#define KERNEL_VERSION(a,b,c) (((a) << 16) + ((b) << 8) + ((c) > 255 ? 255 : (c)))' \
    >> "$out/include/linux/version.h"
]=]

setmetatable(module, {
  __index = function(_, k)
    local g = getters[k]
    if g then return g() end
  end;

  __call = function(_, args)
    local pname <const> = "linux-headers"
    local version <const> = module.version
    local builder = args.builder
    if not builder then
      if args.bash then
        builder = args.bash.."/bin/bash"
      else
        builder = "/usr/bin/bash"
      end
    end

    return derivation {
      name = pname.."-"..version;
      pname = pname;
      version = version;

      system = args.system;
      builder = builder;
      args = { toFile(pname.."-"..version.."-builder.sh", builderScript) };

      src = module.tarball;

      PATH = args.PATH;
      C_INCLUDE_PATH = args.C_INCLUDE_PATH;
      LIBRARY_PATH = args.LIBRARY_PATH;
      CFLAGS = args.CFLAGS;
      LDFLAGS = args.LDFLAGS;

      SOURCE_DATE_EPOCH = args.SOURCE_DATE_EPOCH or 0;
      KBUILD_BUILD_TIMESTAMP = args.KBUILD_BUILD_TIMESTAMP or "@0";
    }
  end;
})

function getters.tarball()
  return fetchurl {
    url = "https://cdn.kernel.org/pub/linux/kernel/v4.x/linux-4.14.336.tar.xz";
    hash = "sha256:0820fdb7971c6974338081c11fbf2dc869870501e7bdcac4d0ed58ba1f57b61c";
  }
end

return module
