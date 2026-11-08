// Package differences holds the calls where walking every call in place of the type index's calls could part from the
// original, and where the fixes' printing matters: a call spread over lines, a call through parentheses, a variadic
// call whose list is spread, and a call whose untrappable signal sits between two others. Both find the same here,
// fixes included, so the fixture records no difference.
package differences

import (
	"os"
	"os/signal"
	"syscall"
)

func shapes(channel chan os.Signal, more []os.Signal) {
	signal.Notify(
		channel,
		os.Interrupt,
		os.Kill,
	)
	(signal.Ignore)(syscall.SIGSTOP)
	signal.Notify(channel, append(more, os.Kill)...)
	signal.Notify(channel, os.Interrupt, syscall.SIGKILL, syscall.SIGHUP)
}
