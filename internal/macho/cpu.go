// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

//go:generate go tool stringer -type=CPUType -linecomment -output=cpu_string.go

package macho

// CPUType is an enumeration of instruction set architectures.
type CPUType uint32

// [CPUType] values defined by Mach-O file format.
const (
	CPUTypeVAX       CPUType = 0x00000001 // CPU_TYPE_VAX
	CPUTypeMC680x0   CPUType = 0x00000006 // CPU_TYPE_MC680x0
	CPUTypeI386      CPUType = 0x00000007 // CPU_TYPE_X86
	CPUTypeX86_64    CPUType = 0x01000007 // CPU_TYPE_X86_64
	CPUTypeMC98000   CPUType = 0x0000000a // CPU_TYPE_MC98000
	CPUTypeHPPA      CPUType = 0x0000000b // CPU_TYPE_HPPA
	CPUTypeARM       CPUType = 0x0000000c // CPU_TYPE_ARM
	CPUTypeARM64     CPUType = 0x0100000c // CPU_TYPE_ARM64
	CPUTypeARM64_32  CPUType = 0x0200000c // CPU_TYPE_ARM64_32
	CPUTypeMC88000   CPUType = 0x0000000d // CPU_TYPE_MC88000
	CPUTypeSPARC     CPUType = 0x0000000e // CPU_TYPE_SPARC
	CPUTypeI860      CPUType = 0x0000000f // CPU_TYPE_I860
	CPUTypePowerPC   CPUType = 0x00000012 // CPU_TYPE_POWERPC
	CPUTypePowerPC64 CPUType = 0x01000012 // CPU_TYPE_POWERPC64
)

// Is64Bit reports whether the CPU type has a 64-bit address width.
func (ct CPUType) Is64Bit() bool {
	return ct&0x01000000 != 0
}
