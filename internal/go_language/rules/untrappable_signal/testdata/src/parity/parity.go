// Package parity holds every way the original accepts or refuses a signal: each of the three functions named
// directly, through parentheses and through a variable, each untrappable signal plain, converted to os.Signal and
// parenthesized, calls that already trap SIGTERM and so lose the fix that would add it, several untrappable signals
// in one call, signals the original never judges, a local value spelled like syscall and os/signal's NotifyContext,
// which the pattern does not name.
package parity

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

type kind = os.Signal

func functions(channel chan os.Signal) {
	signal.Notify(channel, os.Kill)
	signal.Ignore(os.Kill)
	signal.Reset(os.Kill)
	(signal.Notify)(channel, os.Kill)
	notify := signal.Notify
	notify(channel, os.Kill)
	signal.Stop(channel)
	signal.Ignored(os.Kill)
}

func signals(channel chan os.Signal) {
	signal.Notify(channel, syscall.SIGKILL)
	signal.Notify(channel, syscall.SIGSTOP)
	signal.Notify(channel, os.Signal(os.Kill))
	signal.Notify(channel, os.Signal(syscall.SIGKILL))
	signal.Notify(channel, os.Signal(syscall.SIGSTOP))
	signal.Notify(channel, kind(syscall.SIGKILL))
	signal.Notify(channel, (os.Kill))
	signal.Notify(channel, (os.Signal)(syscall.SIGKILL))
	signal.Notify(channel, os.Signal((syscall.SIGKILL)))
	signal.Notify(channel, os.Interrupt, syscall.SIGHUP)
	signal.Notify(channel)
}

func withTerminate(channel chan os.Signal) {
	signal.Notify(channel, syscall.SIGTERM, os.Kill)
	signal.Notify(channel, os.Kill, syscall.SIGTERM)
	signal.Notify(channel, os.Signal(syscall.SIGTERM), syscall.SIGKILL)
	signal.Notify(channel, (syscall.SIGTERM), os.Kill)
	signal.Ignore(os.Kill, syscall.SIGTERM, syscall.SIGSTOP)
}

func several(channel chan os.Signal) {
	signal.Notify(channel, os.Kill, syscall.SIGKILL, syscall.SIGSTOP)
	signal.Ignore(syscall.SIGSTOP, syscall.SIGSTOP)
	signals := []os.Signal{os.Kill}
	signal.Notify(channel, signals...)
}

func generic[T any](channel chan os.Signal, value T) T {
	signal.Reset(syscall.SIGKILL)
	return value
}

func lookalikes(channel chan os.Signal, background context.Context) {
	syscall := struct{ SIGKILL, SIGSTOP os.Signal }{os.Kill, os.Kill}
	signal.Notify(channel, syscall.SIGKILL)
	signal.Notify(channel, syscall.SIGSTOP)
	signal.NotifyContext(background, os.Kill)
	signal.Notify(channel, func() os.Signal { return os.Kill }())
}
