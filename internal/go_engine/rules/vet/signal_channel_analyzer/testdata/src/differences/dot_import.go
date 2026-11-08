package differences

import (
	"os"
	. "os/signal"
)

// Notify dot-imported and named bare, called and held in a variable, which the original, knowing Notify only as a
// selector, does not judge.
func dotImported() {
	bare := make(chan os.Signal)
	Notify(bare, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify // port fixes at differences/dot_import.go:11: make(chan os.Signal, 1)
	held := make(chan os.Signal)
	listen := Notify
	listen(held, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify // port fixes at differences/dot_import.go:13: make(chan os.Signal, 1)
	buffered := make(chan os.Signal, 1)
	Notify(buffered, os.Interrupt)
	listen(buffered, os.Interrupt)
	Notify(make(chan os.Signal), os.Interrupt)
}
