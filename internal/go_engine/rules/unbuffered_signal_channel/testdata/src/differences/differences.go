// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, a variadic call given its
// signals as a slice, and Notify held in a variable, whose callee is known only once the variable is lifted, or not
// at all when two functions meet. The port and the original find the same here, so the fixture records no
// difference.
package differences

import (
	"os"
	"os/signal"
)

func statements() {
	defer signal.Notify(make(chan os.Signal))
	go signal.Notify(make(chan os.Signal), os.Interrupt)
	defer func() {
		signal.Notify(make(chan os.Signal), os.Kill)
	}()
}

func variadic(signals []os.Signal) {
	signal.Notify(make(chan os.Signal), signals...)
}

func lifted(condition bool) {
	notify := signal.Notify
	if condition {
		notify = signal.Notify
	}
	notify(make(chan os.Signal))
	choose := signal.Notify
	if condition {
		choose = func(channel chan<- os.Signal, signals ...os.Signal) {}
	}
	choose(make(chan os.Signal))
}
