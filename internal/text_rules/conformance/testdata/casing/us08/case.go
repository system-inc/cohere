package us08

import (
	"syscall"
)

const O_RDONLY_MODE = syscall.O_RDONLY

func readMode() int {
	return O_RDONLY_MODE
}
