package parity

import (
	. "os"
	. "os/signal"
	"syscall"
)

func dotted(channel chan Signal) {
	Notify(channel, Kill)
	Notify(channel, syscall.SIGKILL)
	Ignore(syscall.SIGSTOP)
	Reset(Signal(syscall.SIGKILL))
}
