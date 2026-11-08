// Package differences holds the shapes where the port and x/tools' sigchanyzer disagree, each with the port's
// finding and fix recorded on its lines, and the shapes beside them where the two agree. The original resolves a name
// through the parser, within the file that uses it, and so misses a channel, or a variable holding signal.Notify,
// declared at package level in another file of the package, declarations.go here. The port resolves it through the
// type checker, across the package's files, and the misuse is the same wherever the channel was declared: a signal
// that arrives while no one is receiving is dropped. Its fix edits the declaring file.
package differences

import (
	"os"
	"os/signal"
)

// A channel declared in another file.
func channelElsewhere() {
	signal.Notify(unbufferedElsewhere, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify
	signal.Notify(bufferedElsewhere, os.Interrupt)
	signal.Notify(fromCallElsewhere, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify
}

// Notify reached through a variable declared in another file.
func notifyThroughElsewhere() {
	unbuffered := make(chan os.Signal)        // port fixes: make(chan os.Signal, 1)
	notifyElsewhere(unbuffered, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify
	buffered := make(chan os.Signal, 1)
	notifyElsewhere(buffered, os.Interrupt)
	notifyElsewhere(make(chan os.Signal), os.Interrupt)
}

// Within one file the two agree, so both find this.
func sameFile() {
	unbuffered := make(chan os.Signal)
	signal.Notify(unbuffered, os.Interrupt)
}
