# `system` values specification

Values for a derivation's `system` field are in the format:

```ebnf
system = arch, "-", os
       | arch, "-linux-gnu"    (* translates to os=linux, abi=gnu *)
       | arch, "-windows"      (* translates to os=windows, abi=msvc *)
       | arch, "-cygwin"       (* translates to os=windows, abi=cygnus *)
       ;

os = ? constant defined under Operating Systems ? ;
arch = ? constant defined under Architectures ? ;
abi = ? constant defined under ABIs ? ;
```

## Operating Systems

| zb name   | Nix name  | Go name   | Description |
| :-------- | :-------- | :-------- | :---------- |
| `linux`   | `linux`   | `linux`   | Linux       |
| `linux`   | `linux`   | `android` | Android     |
| `macos`   | `darwin`  | `darwin`  | macOS       |
| `ios`     | `ios`     | `ios`     | iOS         |
| `windows` | `windows` | `windows` | Windows     |

## Architectures

| zb name   | Nix name                   | Go name   | Description              |
| :-------- | :------------------------- | :-------- | :----------------------- |
| `i686`    | `i386`/`i486`/`i586``i686` | `386`     | Intel 32-bit             |
| `x86_64`  | `x86_64`                   | `amd64`   | Intel 64-bit             |
| `arm`     | `arm`/`armv6l`/`armv7l`    | `arm`     | ARM 32-bit Little Endian |
| `aarch64` | `aarch64`                  | `arm64`   | ARM 64-bit Little Endian |
| `riscv32` | `riscv32`                  | `riscv`   | RISC-V 32-bit            |
| `riscv64` | `riscv64`                  | `riscv64` | RISC-V 64-bit            |

## ABIs

| zb name       | Nix name      | Description                  |
| :------------ | :------------ | :--------------------------- |
| `android`     | `android`     | Android                      |
| `androideabi` | `androideabi` | Android ARM 32-bit           |
| `gnu`         | `gnu`         | GNU C Library                |
| `musl`        | `musl`        | musl                         |
| `msvc`        | `msvc`        | Microsoft Visual C++ Runtime |
| `cygnus`      | `cygnus`      | Cygwin                       |
| `unknown`     | `unknown`     | Placeholder                  |
