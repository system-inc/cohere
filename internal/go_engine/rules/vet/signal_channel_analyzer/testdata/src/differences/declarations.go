package differences

import (
	"os"
	"os/signal"
)

// Declared at package level here and used in differences.go, where the parser resolves neither name.
var unbufferedElsewhere = make(chan os.Signal) // port fixes: make(chan os.Signal, 1)

var bufferedElsewhere = make(chan os.Signal, 1)

var notifyElsewhere = signal.Notify

var fromCallElsewhere = newChannel(2) // port fixes: newChannel(2, 1)

func newChannel(size int) chan os.Signal { return make(chan os.Signal, size) }
