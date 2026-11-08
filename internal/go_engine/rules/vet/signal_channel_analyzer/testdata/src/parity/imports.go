package parity

import (
	operating "os"
	. "os/signal"
	notifying "os/signal"
)

// os and os/signal under names of their own, and os/signal dot-imported, whose Notify is an identifier neither
// resolution gives a declaration, since it is declared in os/signal, so neither judges the call.
func renamedImports() {
	unbuffered := make(chan operating.Signal)
	notifying.Notify(unbuffered, operating.Interrupt) // finding
	buffered := make(chan operating.Signal, 1)
	notifying.Notify(buffered, operating.Interrupt)
	renamed := notifying.Notify
	renamed(unbuffered, operating.Interrupt) // finding
	Notify(unbuffered, operating.Interrupt)
	dotted := Notify
	dotted(unbuffered, operating.Interrupt)
}
