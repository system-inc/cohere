// Package parity holds every branch of sigchanyzer's judgment and of the fix it offers, with clean cases beside each,
// and the shapes most likely to differ between the port and x/tools' sigchanyzer. The differential test runs both
// over each and compares their findings and fixes. Every channel and every variable holding signal.Notify is declared
// in the file that uses it, where the two resolutions agree, and every channel judged is made by the builtin make or
// by no call at all; the differences fixture holds the other files, the other calls and the parenthesized shapes.
package parity

import (
	"os"
	"os/signal"
	"syscall"
)

// Package-level channels and a package-level variable holding Notify, declared in this file.
var unbufferedPackage = make(chan os.Signal)

var bufferedPackage = make(chan os.Signal, 1)

var notifyPackage = signal.Notify

// A channel declared by `:=` and by `var`, unbuffered and buffered.
func declared() {
	unbuffered := make(chan os.Signal)
	signal.Notify(unbuffered, os.Interrupt) // finding
	buffered := make(chan os.Signal, 1)
	signal.Notify(buffered, os.Interrupt)
	var unbufferedVariable = make(chan os.Signal)
	signal.Notify(unbufferedVariable, os.Interrupt, syscall.SIGTERM) // finding
	var bufferedVariable = make(chan os.Signal, 1)
	signal.Notify(bufferedVariable)
	var typedVariable chan os.Signal = make(chan os.Signal)
	signal.Notify(typedVariable) // finding
	// A size of zero is a second argument, so the channel is judged sized.
	zero := make(chan os.Signal, 0)
	signal.Notify(zero, os.Interrupt)
}

// Package-level declarations in this file, which both resolutions reach.
func packageLevel() {
	signal.Notify(unbufferedPackage, os.Interrupt) // finding
	signal.Notify(bufferedPackage, os.Interrupt)
	local := make(chan os.Signal)
	notifyPackage(local, os.Interrupt) // finding
	notifyPackage(bufferedPackage, os.Interrupt)
}

// Notify reached through a variable declared as it, locally, by `:=` and by `var`.
func throughVariable() {
	unbuffered := make(chan os.Signal)
	notify := signal.Notify
	notify(unbuffered, os.Interrupt) // finding
	var notifyVariable = signal.Notify
	notifyVariable(unbuffered, os.Interrupt) // finding
	buffered := make(chan os.Signal, 1)
	notify(buffered, os.Interrupt)
	// A variable of Notify's type assigned rather than declared with it has no value to read.
	var assigned func(chan<- os.Signal, ...os.Signal)
	assigned = signal.Notify
	assigned(unbuffered, os.Interrupt)
	// Other functions of os/signal are not Notify.
	signal.Stop(unbuffered)
	signal.Reset(os.Interrupt)
}

// The channel passed is a call itself.
func calls() {
	// The builtin make, unbuffered or not, is safe: no one else holds the channel.
	signal.Notify(make(chan os.Signal), os.Interrupt)
	signal.Notify(make(chan os.Signal, 1), os.Interrupt)
	// A call with no argument or two, wrapped, indexed, received from or selected, is neither judged.
	signal.Notify((make(chan os.Signal)), os.Interrupt)              // parenthesized
	signal.Notify(newSizedChannel(1, 2), os.Interrupt)               // two arguments
	signal.Notify(newDefaultChannel(), os.Interrupt)                 // no argument
	signal.Notify(channelAndSignal())                                // no argument
	signal.Notify(channels()[0], os.Interrupt)                       // an index
	signal.Notify(<-channelOfChannels(), os.Interrupt)               // a receive
	signal.Notify(holder{channel: bufferedPackage}.channel, os.Kill) // a selector
}

// A variable declared as a call other than make, with no argument or two.
func declaredCalls() {
	fromTwo := newSizedChannel(1, 2)
	signal.Notify(fromTwo, os.Interrupt)
	fromNone := newDefaultChannel()
	signal.Notify(fromNone, os.Interrupt)
}

// Declarations whose names and values differ in number, or that reuse a name.
func declarationShapes() {
	first, second := make(chan os.Signal), make(chan os.Signal, 1)
	signal.Notify(first, os.Interrupt) // finding
	signal.Notify(second, os.Interrupt)
	var third, fourth = make(chan os.Signal, 1), make(chan os.Signal)
	signal.Notify(third, os.Interrupt)
	signal.Notify(fourth, os.Interrupt) // finding
	fromPair, fromPairToo := twoChannels()
	signal.Notify(fromPair, os.Interrupt)
	signal.Notify(fromPairToo, os.Interrupt)
	var fromVariablePair, fromVariablePairToo = twoChannels()
	signal.Notify(fromVariablePair, os.Interrupt)
	signal.Notify(fromVariablePairToo, os.Interrupt)
	received, open := <-channelOfChannels()
	signal.Notify(received, os.Interrupt)
	_ = open
	// A `:=` that reuses a name keeps its first declaration, buffered here and unbuffered after.
	kept := make(chan os.Signal, 1)
	kept, failure := make(chan os.Signal), error(nil)
	signal.Notify(kept, os.Interrupt)
	_ = failure
	keptUnbuffered := make(chan os.Signal)
	keptUnbuffered, failureToo := make(chan os.Signal, 1), error(nil)
	signal.Notify(keptUnbuffered, os.Interrupt) // finding
	_ = failureToo
	// An assignment after the declaration is not read: the declaration is. Both report reassigned, since its
	// declaration, make with no size, proves it unbuffered when declared, and judging the assignment would need flow
	// analysis.
	reassigned := make(chan os.Signal)
	reassigned = make(chan os.Signal, 1)
	signal.Notify(reassigned, os.Interrupt) // finding
	var declaredEmpty chan os.Signal
	declaredEmpty = make(chan os.Signal)
	signal.Notify(declaredEmpty, os.Interrupt)
}

// Names declared without a value of their own: parameters, results, range variables, a type switch's, a select's.
func undeclaredValues(parameter chan os.Signal, values []any, incoming chan chan os.Signal) (result chan os.Signal) {
	signal.Notify(parameter, os.Interrupt)
	signal.Notify(result, os.Interrupt)
	for _, ranged := range channels() {
		signal.Notify(ranged, os.Interrupt)
	}
	for receivedOnly := range channelOfChannels() {
		signal.Notify(receivedOnly, os.Interrupt)
	}
	for _, value := range values {
		switch switched := value.(type) {
		case chan os.Signal:
			signal.Notify(switched, os.Interrupt)
		}
	}
	select {
	case selected := <-incoming:
		signal.Notify(selected, os.Interrupt)
	default:
	}
	return result
}

// Shadowing, closures, and calls deferred or started.
func scopes() {
	shadowed := make(chan os.Signal, 1)
	{
		shadowed := make(chan os.Signal)
		signal.Notify(shadowed, os.Interrupt) // finding
	}
	signal.Notify(shadowed, os.Interrupt)
	captured := make(chan os.Signal)
	func() {
		signal.Notify(captured, os.Interrupt) // finding
		inner := make(chan os.Signal)
		signal.Notify(inner, os.Interrupt) // finding
	}()
	defer signal.Notify(captured, os.Interrupt) // finding
	go signal.Notify(captured, os.Interrupt)    // finding
	if conditional := make(chan os.Signal); conditional != nil {
		signal.Notify(conditional, os.Interrupt) // finding
	}
	// A Notify nested in another call's arguments is visited too.
	ignore(func() { signal.Notify(captured) }) // finding
}

// Generic functions, alias and named channel types.
func generic[T any](value T) {
	unbuffered := make(chan os.Signal)
	signal.Notify(unbuffered, os.Interrupt) // finding
	aliased := make(chan signalAlias)
	signal.Notify(aliased, os.Interrupt) // finding
	named := make(signals)
	signal.Notify(named, os.Interrupt) // finding
	namedSized := make(signals, 2)
	signal.Notify(namedSized, os.Interrupt)
	_ = value
}

// A method named Notify, of this package, is not os/signal's, as a value or called.
func methods() {
	unbuffered := make(chan os.Signal)
	var notifier holder
	notifier.Notify(unbuffered, os.Interrupt)
	method := notifier.Notify
	method(unbuffered, os.Interrupt)
	expression := holder.Notify
	expression(notifier, unbuffered, os.Interrupt)
	var embedding embedded
	embedding.Notify(unbuffered, os.Interrupt)
	embeddedMethod := embedding.Notify
	embeddedMethod(unbuffered, os.Interrupt)
}

// The fix prints the call afresh on one line, so a call over several lines, or holding a comment, is replaced whole.
func layout() {
	multiline := make(
		chan os.Signal,
	)
	signal.Notify(multiline, os.Interrupt) // finding
	commented := make(chan os.Signal /* unbuffered */)
	signal.Notify(commented, os.Interrupt) // finding
}

type signalAlias = os.Signal

type signals chan os.Signal

type holder struct{ channel chan os.Signal }

func (holder) Notify(channel chan<- os.Signal, notified ...os.Signal) {}

type embedded struct{ holder }

func newSizedChannel(size int, extra int) chan os.Signal { return make(chan os.Signal, size+extra) }

func newDefaultChannel() chan os.Signal { return make(chan os.Signal, 1) }

func channelAndSignal() (chan os.Signal, os.Signal) { return make(chan os.Signal, 1), os.Interrupt }

func twoChannels() (chan os.Signal, chan os.Signal) {
	return make(chan os.Signal, 1), make(chan os.Signal)
}

func channels() []chan os.Signal { return nil }

func channelOfChannels() chan chan os.Signal { return nil }

func ignore(function func()) {}
