// Package parity holds channels made with and without a buffer, in the shapes staticcheck's representation builds a
// channel or a call in more than one way.
package parity

import (
	"os"
	"os/signal"
	"syscall"
)

const none = 0

type signals chan os.Signal

var global = make(chan os.Signal)

func init() {
	signal.Notify(make(chan os.Signal))
}

func channelShapes(size int, condition bool, given chan os.Signal) {
	signal.Notify(make(chan os.Signal), os.Interrupt)
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)
	signal.Notify(make(chan os.Signal, 0), syscall.SIGTERM)
	signal.Notify(make(chan os.Signal, none))
	signal.Notify(make(chan os.Signal, 1-1))
	signal.Notify(make(chan os.Signal, size))
	signal.Notify(make(signals))
	signal.Notify(make(chan<- os.Signal))
	signal.Notify(given)
	signal.Notify(global)
	local := make(chan os.Signal)
	signal.Notify(local)
	sendOnly := (chan<- os.Signal)(make(chan os.Signal))
	signal.Notify(sendOnly)
	chosen := make(chan os.Signal)
	if condition {
		chosen = make(chan os.Signal)
	}
	signal.Notify(chosen)
	signal.Stop(make(chan os.Signal))
	signal.Ignore(os.Interrupt)
}

func callShapes() {
	defer signal.Notify(make(chan os.Signal))
	go signal.Notify(make(chan os.Signal))
	func() {
		signal.Notify(make(chan os.Signal))
	}()
	notify := signal.Notify
	notify(make(chan os.Signal))
	signal.Notify(
		make(chan os.Signal),
		os.Interrupt,
	)
}

func generic[T any](value T) T {
	signal.Notify(make(chan os.Signal))
	return value
}
