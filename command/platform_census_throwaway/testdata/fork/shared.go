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
