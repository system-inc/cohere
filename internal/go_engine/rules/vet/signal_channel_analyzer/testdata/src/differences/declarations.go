package differences

import (
	"os"
	"os/signal"
)

// Declared at package level here and used in differences.go, where the parser resolves none of these names.
var unbufferedElsewhere = make(chan os.Signal) // fix: make(chan os.Signal, 1)

var bufferedElsewhere = make(chan os.Signal, 1)

var notifyElsewhere = signal.Notify

// A helper's channel, which the original cannot reach from another file and the port does not judge.
var fromCallElsewhere = newChannel(2)

func newChannel(size int) chan os.Signal { return make(chan os.Signal, size) }

func newGeneric[T any](size int) chan T { return make(chan T, size) }

func variadicChannel(sizes ...int) chan os.Signal { return make(chan os.Signal, len(sizes)) }
