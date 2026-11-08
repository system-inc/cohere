package differences

import (
	"os"
	. "os/signal"
)

// Notify dot-imported and named bare, called and held in a variable, which the original, knowing Notify only as a
// selector, does not judge.
func dotImported() {
	bare := make(chan os.Signal) // port fixes: make(chan os.Signal, 1)
	Notify(bare, os.Interrupt)   // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify
	held := make(chan os.Signal) // port fixes: make(chan os.Signal, 1)
	listen := Notify
	listen(held, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify
	buffered := make(chan os.Signal, 1)
	Notify(buffered, os.Interrupt)
	listen(buffered, os.Interrupt)
	Notify(make(chan os.Signal), os.Interrupt)
}
