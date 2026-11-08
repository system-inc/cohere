package parity

import (
	operating "os"
	trap "os/signal"
	system "syscall"
)

func renamed(channel chan operating.Signal) {
	trap.Notify(channel, operating.Kill)
	trap.Notify(channel, system.SIGKILL)
	trap.Reset(operating.Signal(system.SIGSTOP))
	trap.Notify(channel, system.SIGTERM, operating.Kill)
}
