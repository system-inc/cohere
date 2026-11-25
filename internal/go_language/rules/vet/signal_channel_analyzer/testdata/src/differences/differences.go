// Package differences holds the shapes where the port and x/tools' sigchanyzer disagree, each recorded on its lines,
// with the shapes beside them where the two agree.
//
// The port finds what the original misses, each a true finding, since a signal that arrives while no one is
// receiving at an unbuffered channel is dropped wherever the channel was declared and however Notify was named. The
// original resolves a name through the parser, within the file that uses it, so it misses a channel, or a variable
// holding signal.Notify, declared at package level in another file, declarations.go here; the port resolves names
// through the type checker, across the package's files, and fixes the declaring file. The original knows Notify only
// as a selector, so it misses Notify dot-imported and named bare, dot_import.go. And it reads no parentheses.
//
// The port drops what the original reports wrongly: a channel made by a call other than the builtin make with one
// argument, which the original treats as unbuffered (golang/go#45043). Every such channel here is buffered, so every
// such finding is wrong, and its fix, a size appended to the call's arguments, does not compile.
package differences

import (
	"os"
	"os/signal"
)

// A channel declared in another file.
func channelElsewhere() {
	signal.Notify(unbufferedElsewhere, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify // port fixes at differences/declarations.go:9: make(chan os.Signal, 1)
	signal.Notify(bufferedElsewhere, os.Interrupt)
	signal.Notify(fromCallElsewhere, os.Interrupt)
}

// Notify reached through a variable declared in another file.
func notifyThroughElsewhere() {
	unbuffered := make(chan os.Signal)
	notifyElsewhere(unbuffered, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify // port fixes at differences/differences.go:30: make(chan os.Signal, 1)
	buffered := make(chan os.Signal, 1)
	notifyElsewhere(buffered, os.Interrupt)
	notifyElsewhere(make(chan os.Signal), os.Interrupt)
}

// Parentheses around Notify, around a variable holding it, around the channel passed and around a declared make.
func parenthesized() {
	aroundNotify := make(chan os.Signal)
	(signal.Notify)(aroundNotify, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify // port fixes at differences/differences.go:39: make(chan os.Signal, 1)
	aroundHeld := make(chan os.Signal)
	listen := (signal.Notify)
	listen(aroundHeld, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify // port fixes at differences/differences.go:41: make(chan os.Signal, 1)
	aroundChannel := make(chan os.Signal)
	signal.Notify((aroundChannel), os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify // port fixes at differences/differences.go:44: make(chan os.Signal, 1)
	aroundDeclared := (make(chan os.Signal))
	signal.Notify(aroundDeclared, os.Interrupt) // port finds: misuse of unbuffered os.Signal channel as argument to signal.Notify // port fixes at differences/differences.go:46: make(chan os.Signal, 1)
	// make parenthesized and declared is the builtin, unbuffered, which both find, with the same fix.
	aroundMake := (make)(chan os.Signal)
	signal.Notify(aroundMake, os.Interrupt)
	buffered := (make(chan os.Signal, 1))
	signal.Notify((buffered), os.Interrupt)
}

// Channels made by calls other than the builtin make with one argument, passed straight or declared.
func otherCalls() {
	// Nothing proves newChannel's channel unbuffered without reading into newChannel, and it holds four.
	signal.Notify(newChannel(4), os.Interrupt) // port drops: misuse of unbuffered os.Signal channel as argument to signal.Notify
	// Nothing proves a conversion's channel unbuffered without reading what it converts, which holds one.
	signal.Notify((chan os.Signal)(bufferedElsewhere), os.Interrupt) // port drops: misuse of unbuffered os.Signal channel as argument to signal.Notify
	// Nothing proves newGeneric's channel unbuffered without reading into newGeneric, and it holds two.
	signal.Notify(newGeneric[os.Signal](2), os.Interrupt) // port drops: misuse of unbuffered os.Signal channel as argument to signal.Notify
	// Nothing proves variadicChannel's channel unbuffered without reading into it, and it holds one.
	signal.Notify(variadicChannel([]int{1}...), os.Interrupt) // port drops: misuse of unbuffered os.Signal channel as argument to signal.Notify
	// Nothing proves newChannel's channel unbuffered without reading into newChannel, and it holds two.
	signal.Notify(newChannel(newChannelSize(2)), os.Interrupt) // port drops: misuse of unbuffered os.Signal channel as argument to signal.Notify
	// A fresh make written straight into the call is exempt by the original's own rule, parenthesized or not: no one else holds the channel, so no receive can miss a signal.
	signal.Notify((make)(chan os.Signal), os.Interrupt) // port drops: misuse of unbuffered os.Signal channel as argument to signal.Notify
	fromCall := newChannel(3)
	// Nothing proves fromCall's buffer without reading into newChannel, and it holds three.
	signal.Notify(fromCall, os.Interrupt) // port drops: misuse of unbuffered os.Signal channel as argument to signal.Notify
	converted := (chan os.Signal)(bufferedElsewhere)
	// Nothing proves converted's buffer without reading what it converts, which holds one.
	signal.Notify(converted, os.Interrupt) // port drops: misuse of unbuffered os.Signal channel as argument to signal.Notify
	make := newChannel
	shadowed := make(5)
	// make is a local function here, not the builtin, so nothing proves its channel unbuffered, and it holds five.
	signal.Notify(shadowed, os.Interrupt) // port drops: misuse of unbuffered os.Signal channel as argument to signal.Notify
}

// Within one file, unparenthesized, and made by make, the two agree, so both find this.
func sameFile() {
	unbuffered := make(chan os.Signal)
	signal.Notify(unbuffered, os.Interrupt)
}

func newChannelSize(size int) int { return size }
