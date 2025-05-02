// Copyright 2025 The zb Authors
// SPDX-License-Identifier: MIT

package system

// Architecture is the name of an instruction set architecture of a [System].
// The empty string is treated the same as [Unknown].
type Architecture string

// IsUnknown reports whether arch is the empty string or [Unknown].
func (arch Architecture) IsUnknown() bool {
	return arch == "" || arch == Unknown
}

// String returns string(arch) or [Unknown] if arch is the empty string.
func (arch Architecture) String() string {
	if arch == "" {
		return Unknown
	}
	return string(arch)
}

func (arch Architecture) pointerBitWidth() (int, bool) {
	switch {
	case arch.isX8632() || arch.isARM32() || arch.isRISCV32():
		return 32, true
	case arch.isX8664() || arch.isARM64() || arch.isRISCV64():
		return 64, true
	default:
		return 0, false
	}
}

// Is32Bit reports whether pointers on the architecture are 32 bits wide.
func (arch Architecture) Is32Bit() bool {
	w, _ := arch.pointerBitWidth()
	return w == 32
}

// Is64Bit reports whether pointers on the architecture are 64 bits wide.
func (arch Architecture) Is64Bit() bool {
	w, _ := arch.pointerBitWidth()
	return w == 64
}

// IsX86 reports whether sys uses the x86 family of instruction set architectures,
// including both 32-bit and 64-bit x86.
func (arch Architecture) IsX86() bool {
	return arch.isX8632() || arch.isX8664()
}

// isX8632 reports whether sys uses a 32-bit Intel-based instruction set.
func (arch Architecture) isX8632() bool {
	return arch == "i386" ||
		arch == "i486" ||
		arch == "i586" ||
		arch == "i686" ||
		arch == "i786" ||
		arch == "i886" ||
		arch == "i986"
}

// isX8664 reports whether sys uses a 64-bit Intel-based instruction set.
func (arch Architecture) isX8664() bool {
	return arch == "x86_64" ||
		arch == "amd64" ||
		arch == "x86_64h"
}

// IsARM reports whether sys uses an ARM-based instruction set.
func (arch Architecture) IsARM() bool {
	return arch.isARM32() || arch.isARM64()
}

// isARM32 reports whether sys uses a 32-bit ARM-based instruction set.
func (arch Architecture) isARM32() bool {
	return arch == "arm"
}

// isARM64 reports whether sys uses a 64-bit ARM-based instruction set.
func (arch Architecture) isARM64() bool {
	return arch == "aarch64" || arch == "arm64"
}

// IsRISCV reports whether sys uses a RISC-V instruction set.
func (arch Architecture) IsRISCV() bool {
	return arch.isRISCV32() || arch.isRISCV64()
}

// isRISCV32 reports whether sys uses a 32-bit RISC-V instruction set.
func (arch Architecture) isRISCV32() bool {
	return arch == "riscv32"
}

// isRISCV64 reports whether sys uses a 64-bit RISC-V instruction set.
func (arch Architecture) isRISCV64() bool {
	return arch == "riscv64"
}
