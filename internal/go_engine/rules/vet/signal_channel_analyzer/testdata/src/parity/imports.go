package parity

import (
	operating "os"
	notifying "os/signal"
)

// os and os/signal under names of their own. A dot import of os/signal, whose Notify only the port judges, is in the
// differences fixture.
func renamedImports() {
	unbuffered := make(chan operating.Signal)
	notifying.Notify(unbuffered, operating.Interrupt) // finding
	buffered := make(chan operating.Signal, 1)
	notifying.Notify(buffered, operating.Interrupt)
	renamed := notifying.Notify
	renamed(unbuffered, operating.Interrupt) // finding
}
