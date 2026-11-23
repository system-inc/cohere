package fork

import "unsafe"

// F forks by reference: N differs by platform, and so do N and N * 3.
func F() int {
	return N * 3
}

// Untouched holds no fork.
func Untouched() int {
	return 7
}

// WordSize forks locally: a pointer is 8 bytes on amd64 and arm64 and 4 on 386, and nothing it uses differs.
func WordSize() uintptr {
	return unsafe.Sizeof(uintptr(0))
}

// IFMT is one untyped constant on every platform, as syscall.S_IFMT is between linux and darwin.
const IFMT = 0xf000

// IsPipe forks by reference through its context: Mode differs, so IFMT and 0 take a uint16 on windows, though neither
// uses anything that differs.
func IsPipe(stat Stat) bool {
	return stat.Mode&IFMT != 0
}

// Signal holds no fork: Kill's parameters are named differently on linux, and its type is the same.
func Signal() error {
	return Kill(1, 9)
}
