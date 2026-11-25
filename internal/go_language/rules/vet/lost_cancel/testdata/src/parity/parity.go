// Package parity holds every branch of lostcancel's judgment, each with a clean case beside it, and the shapes most
// likely to differ between the original and the port: every path shape the search walks (returns, panics, calls
// that never return, here and in a dependency, loops, gotos, selects, switches), closures, the cancel function
// stored or passed rather than called, generics, aliased and dot imports, and named results. The original and the port agree on every line here. A comment says what
// each line is expected to draw; the test compares the two analyzers' findings, not these comments.
package parity

import (
	"context"
	stdcontext "context"
	"errors"
	"log"
	"os"
	"time"

	"parity/halt"
)

var background = context.Background()

var condition bool

var number int

// Every function that returns a cancel function, each discarded.
func discardedEveryKind() {
	ctx, _ := context.WithCancel(background)                         // finds: discarded, WithCancel
	ctx, _ = context.WithCancelCause(background)                     // finds: discarded, WithCancelCause
	ctx, _ = context.WithTimeout(background, 0)                      // finds: discarded, WithTimeout
	ctx, _ = context.WithTimeoutCause(background, 0, nil)            // finds: discarded, WithTimeoutCause
	ctx, _ = context.WithDeadline(background, time.Time{})           // finds: discarded, WithDeadline
	ctx, _ = context.WithDeadlineCause(background, time.Time{}, nil) // finds: discarded, WithDeadlineCause
	var other, _ = context.WithCancel(ctx)                           // finds: discarded, in a var declaration
	_ = other
}

// context's other functions return no cancel function, and are not judged.
func otherContextFunctions() {
	ctx := context.WithoutCancel(background)
	ctx = context.WithValue(ctx, "key", 1)
	stop := context.AfterFunc(ctx, func() {})
	_ = stop
}

// Every Cause variant, each lost on one path.
func lostEveryKind() {
	_, cancelCause := context.WithCancelCause(background)                        // finds: not used on all paths
	_, cancelTimeout := context.WithTimeoutCause(background, 0, nil)             // finds: not used on all paths
	_, cancelDeadline := context.WithDeadlineCause(background, time.Time{}, nil) // finds: not used on all paths
	if condition {
		cancelCause(nil)
		cancelTimeout()
		cancelDeadline()
	}
} // finds: this return, three times

// An aliased import of context is context.
func aliasedImport() {
	_, cancel := stdcontext.WithCancel(background) // finds: not used on all paths
	if condition {
		cancel()
	}
	ctx, _ := stdcontext.WithTimeout(background, 0) // finds: discarded
	_ = ctx
} // finds: this return

// The selector need only sit beneath a call, not be what it calls, so a function of context passed as an argument
// is judged as if it were called.
func apply(maker func(context.Context) (context.Context, context.CancelFunc)) (context.Context, context.CancelFunc) {
	return maker(background)
}

func selectorPassedAsArgument() {
	ctx, _ := apply(context.WithCancel)    // finds: discarded, though apply's result is what is discarded
	_, cancel := apply(context.WithCancel) // finds: not used on all paths
	if condition {
		cancel()
	}
	_ = ctx
} // finds: this return

// A selector not beneath a call is not judged.
func selectorNotCalled() {
	constructor := context.WithCancel
	_, cancel := constructor(background)
	if condition {
		cancel()
	}
}

// A call whose statement assigns one name is not judged.
func oneName() {
	pair := func() (context.Context, context.CancelFunc) { return context.WithCancel(background) }
	_ = pair
}

// A variable of a type with a method named WithCancel is not the package. One named context, which the original
// takes for the package, is in testdata/src/differences, where the port drops what the original finds there.
type canceller struct{}

func (canceller) WithCancel(parent context.Context) (context.Context, func()) {
	return parent, func() {}
}

func namedOtherwise() {
	maker := canceller{}
	ctx, _ := maker.WithCancel(background)
	_ = ctx
}

// Used on every path: deferred, called, or both branches.
func usedOnEveryPath() {
	_, cancel := context.WithCancel(background)
	if condition {
		cancel()
		return
	}
	cancel()
}

// Used in the rest of its defining block, before any branch.
func usedInDefiningBlock() {
	_, cancel := context.WithCancel(background)
	stop := cancel
	if condition {
		return
	}
	stop()
}

// The defining block returns at once without using it.
func definingBlockReturns() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		cancel()
	}
	return // finds: this return
}

// A return with results in the defining block, after a use inside a literal: clean.
func definingBlockReturnsValue() error {
	_, cancel := context.WithCancel(background)
	_ = func() { cancel() }
	return nil
}

func definingBlockReturnsFirst() error {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		return errors.New("early") // finds: this return
	}
	cancel()
	return nil
}

// Several returns: the search reports the first it reaches, depth first, successors in order.
func severalReturns() int {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	switch number {
	case 0:
		cancel()
		return 0
	case 1:
		return 1 // finds: this return, the first reached
	case 2:
		return 2
	}
	cancel()
	return 3
}

// Defined in an if statement's initializer, lost on the else path.
func definedInIfInitializer() {
	if _, cancel := context.WithCancel(background); condition { // finds: not used on all paths
		cancel()
	}
} // finds: this return

// Defined in a switch's initializer, used in every case.
func definedInSwitchInitializer() {
	switch _, cancel := context.WithCancel(background); number {
	case 0:
		cancel()
	default:
		cancel()
	}
}

// Defined in a for statement's initializer, used only inside the loop, which may not run.
func definedInForInitializer() {
	for _, cancel := context.WithCancel(background); condition; { // finds: not used on all paths
		cancel()
		break
	}
} // finds: this return

// Panics end their paths, as calls that never return.
func panicsOnTheOtherPath() {
	_, cancel := context.WithCancel(background)
	if condition {
		cancel()
	} else {
		panic("never returns")
	}
}

// A panic only on one path of two.
func panicsOnOnePath() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	switch number {
	case 0:
		panic(0)
	case 1:
		cancel()
	}
} // finds: this return

// A deferred call may recover a later panic, but the search only follows edges, and a panic has none.
func panicsAfterDefer() {
	_, cancel := context.WithCancel(background)
	defer print()
	if condition {
		cancel()
		return
	}
	panic(1)
}

// Calls that never return, in this package.
func exitHere() {
	os.Exit(1)
}

func exitThrough() {
	exitHere()
}

func exitsByRecursion(depth int) {
	exitsByRecursionToo(depth)
}

func exitsByRecursionToo(depth int) {
	exitsByRecursion(depth + 1)
}

type fatal struct{}

func (fatal) die() {
	log.Fatal("dies")
}

func neverReturnsHere[T any](value T) {
	panic(value)
}

func callsThatNeverReturnHere() {
	_, cancel := context.WithCancel(background)
	switch number {
	case 0:
		exitHere()
	case 1:
		exitThrough()
	case 2:
		fatal{}.die()
	default:
		cancel()
	}
}

// ctrlflow breaks a cycle of calls where it enters it, and the call that closes the cycle is judged before either
// function is known not to return, so the cycle is taken to return.
func callsIntoACycle() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		exitsByRecursion(0)
	} else {
		cancel()
	}
} // finds: this return

// A generic function's call, instantiated implicitly or explicitly, is a static call of its declaration, which never
// returns.
func callsAGenericThatNeverReturns() {
	_, cancel := context.WithCancel(background)
	if condition {
		neverReturnsHere(4)
	} else {
		cancel()
	}
}

func callsAGenericThatNeverReturnsExplicitly() {
	_, cancel := context.WithCancel(background)
	if condition {
		neverReturnsHere[string]("five")
	} else {
		cancel()
	}
}

// A method value is not a static call, so it may return.
func methodValueMayReturn() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	die := fatal{}.die
	if condition {
		die()
	} else {
		cancel()
	}
} // finds: this return

// A function variable is not a static call, so it may return.
var stopVariable = func() { os.Exit(1) }

func functionVariableMayReturn() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		stopVariable()
	} else {
		cancel()
	}
} // finds: this return

// A call that never returns, used for its value, is not a statement, so the graph does not end there.
func neverReturnsValue() int {
	exitHere()
	return 0
}

func callForValueMayReturn() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		_ = neverReturnsValue
		number = neverReturnsValue()
	} else {
		cancel()
	}
} // finds: this return

// Calls that never return, in a dependency, through ctrlflow's facts.
func callsThatNeverReturnInADependency() {
	_, cancel := context.WithCancel(background)
	switch number {
	case 0:
		halt.Exit()
	case 1:
		halt.ExitThrough()
	case 2:
		halt.Stop()
	case 3:
		halt.Fail("three")
	case 4:
		halt.Fatal{}.Die()
	case 5:
		halt.Forever()
	case 6:
		halt.Generic(6)
	case 7:
		log.Fatalf("seven")
	default:
		cancel()
	}
}

// A dependency's function that returns sometimes may return.
func dependencyMayReturn() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		halt.Maybe(condition)
	} else {
		cancel()
	}
} // finds: this return

// Loops: one without end never reaches a return, one with a condition may end, and a break leaves one without end.
func infiniteLoop() {
	_, cancel := context.WithCancel(background)
	if condition {
		cancel()
		return
	}
	for {
		print(0)
	}
}

func conditionalLoop() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	for condition {
		print(0)
	}
	if condition {
		cancel()
	}
} // finds: this return

func breakOutOfInfiniteLoop() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	for {
		if condition {
			break
		}
		cancel()
	}
} // finds: this return

func labeledBreak() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
outer:
	for {
		for {
			if condition {
				break outer
			}
			if number > 0 {
				continue outer
			}
			cancel()
		}
	}
} // finds: this return

func usedInLoopBody() {
	_, cancel := context.WithCancel(background)
	for {
		cancel()
		if condition {
			return
		}
	}
}

func rangeLoop(values []int) {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	for range values {
		cancel()
	}
} // finds: this return

func rangeOverFunction(sequence func(func(int) bool)) {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	for value := range sequence {
		if value > 0 {
			cancel()
		}
	}
} // finds: this return

// Gotos: one that skips the use, one that forms a loop without end, one that jumps to the use.
func gotoSkipsTheUse() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		goto done
	}
	cancel()
done:
	print()
} // finds: this return

func gotoLoopsForever() {
	_, cancel := context.WithCancel(background)
	if condition {
		cancel()
		return
	}
again:
	print()
	goto again
}

func gotoJumpsToTheUse() {
	_, cancel := context.WithCancel(background)
	if condition {
		goto use
	}
	print()
use:
	cancel()
}

// Selects: one that blocks runs a case, one with a default may take it.
func blockingSelect(channel chan int) {
	_, cancel := context.WithCancel(background)
	select {
	case <-channel:
		cancel()
	case channel <- 1:
		cancel()
	}
}

func selectWithDefault(channel chan int) {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	select {
	case <-channel:
		cancel()
	default:
	}
} // finds: this return

func emptySelect() {
	_, cancel := context.WithCancel(background)
	if condition {
		cancel()
		return
	}
	select {}
}

// Switches: a type switch, and a fallthrough into the use.
func typeSwitch(value any) {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	switch value.(type) {
	case int:
		cancel()
	case string:
		return // finds: this return
	}
	cancel()
}

func fallthroughToTheUse() {
	_, cancel := context.WithCancel(background)
	switch number {
	case 0:
		print()
		fallthrough
	default:
		cancel()
	}
}

// Closures: a use inside a nested literal counts, even uncalled.
func usedInClosure() {
	_, cancel := context.WithCancel(background)
	later := func() { cancel() }
	_ = later
}

func usedInDeferredClosure() {
	_, cancel := context.WithCancel(background)
	defer func() {
		cancel()
	}()
}

func usedInGoroutine() {
	_, cancel := context.WithCancel(background)
	go cancel()
}

// A literal is judged on its own: its cancel variable lost inside it, and the outer function clean.
func lostInsideLiteral() {
	_, cancel := context.WithCancel(background)
	defer cancel()
	run := func() {
		_, inner := context.WithCancel(background) // finds: not used on all paths
		if condition {
			inner()
		}
	} // finds: this return
	run()
}

// A literal nested twice.
func lostTwoLiteralsDeep() {
	outer := func() func() {
		return func() {
			_, cancel := context.WithTimeout(background, time.Second) // finds: not used on all paths
			if condition {
				return // finds: this return
			}
			cancel()
		}
	}
	outer()()
}

// A literal assigning a variable of the function around it leaves it alone, assumed used out there.
func assignedFromLiteral() {
	var cancel context.CancelFunc
	func() {
		_, cancel = context.WithCancel(background)
	}()
	if condition {
		cancel()
	}
}

// A parameter is in the function's scope, so a cancel function assigned to one is judged.
func assignedToParameter(cancel context.CancelFunc) {
	_, cancel = context.WithCancel(background) // finds: not used on all paths
	if condition {
		cancel()
	}
} // finds: this return

// A package-level variable is assumed used elsewhere.
var packageCancel context.CancelFunc

func assignedToPackageVariable() {
	_, packageCancel = context.WithCancel(background)
}

// Stored or passed: each is a use.
type holder struct {
	cancel  context.CancelFunc
	cancels []context.CancelFunc
}

func storedInField(into *holder) {
	_, cancel := context.WithCancel(background)
	into.cancel = cancel
}

func storedInSlice(into *holder) {
	_, cancel := context.WithCancel(background)
	into.cancels = append(into.cancels, cancel)
}

func storedInMap(into map[string]context.CancelFunc) {
	_, cancel := context.WithCancel(background)
	into["cancel"] = cancel
}

func register(cancel context.CancelFunc) {}

func passedToFunction() {
	_, cancel := context.WithCancel(background)
	register(cancel)
}

func sentOnChannel(channel chan context.CancelFunc) {
	_, cancel := context.WithCancel(background)
	channel <- cancel
}

func returned() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(background)
	return ctx, cancel
}

// Stored only on one path.
func storedOnOnePath(into *holder) {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		into.cancel = cancel
	}
} // finds: this return

// Assigned straight into a field, a slice element or a map entry, the cancel function names no variable and is left
// alone.
func assignedIntoField(into *holder) {
	_, into.cancel = context.WithCancel(background)
}

func assignedIntoElement(into *holder, entries map[string]context.CancelFunc) {
	_, into.cancels[0] = context.WithCancel(background)
	_, entries["cancel"] = context.WithCancel(background)
}

// A method assigning its receiver's field leaves it alone too.
func (into *holder) start() {
	_, into.cancel = context.WithCancel(background)
}

// A method whose cancel variable is lost.
func (into *holder) lost() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		into.cancel = cancel
	}
} // finds: this return

// Named results: a naked return uses them, a return with results does not.
func namedResultNakedReturn() (ctx context.Context, cancel context.CancelFunc) {
	ctx, cancel = context.WithCancel(background)
	if condition {
		return
	}
	return
}

func namedResultReturnsOther() (ctx context.Context, cancel context.CancelFunc) {
	ctx, cancel = context.WithCancel(background) // finds: not used on all paths
	if condition {
		return ctx, nil // finds: this return
	}
	return
}

// A naked return inside a literal counts as a use of the outer function's named results too, since the walk for a
// use goes into nested literals.
func namedResultNakedReturnInLiteral() (ctx context.Context, cancel context.CancelFunc) {
	ctx, cancel = context.WithCancel(background)
	run := func() { return }
	run()
	return ctx, nil
}

// A cancel function that is not a named result gains nothing from a naked return.
func unnamedNakedReturn() (err error) {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		cancel()
	}
	return // finds: this return
}

// Redefined: only the statement that last defines a variable is searched from.
func redefinedLastUsed() {
	var cancel context.CancelFunc
	_, cancel = context.WithCancel(background)
	if condition {
		print()
	}
	_, cancel = context.WithCancel(background)
	cancel()
}

func redefinedLastLost() {
	_, cancel := context.WithCancel(background)
	cancel()
	_, cancel = context.WithTimeout(background, 0) // finds: not used on all paths
	if condition {
		cancel()
	}
} // finds: this return

// Two cancel variables in one function, each lost: two pairs of findings.
func twoLost() {
	_, first := context.WithCancel(background)  // finds: not used on all paths
	_, second := context.WithCancel(background) // finds: not used on all paths
	if condition {
		first()
		second()
	}
} // finds: this return, twice

// Defined after a return: its block is unreachable, and the end of the function with it, so no return is reached.
func definedWhereUnreachable() {
	return
	_, cancel := context.WithCancel(background)
	if condition {
		cancel()
	}
}

// Defined after a return, but a label makes the rest reachable again.
func definedAfterLabel() {
	goto start
start:
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		cancel()
	}
} // finds: this return

// Generics: a generic function, a method of a generic type, and an instantiation.
func generic[T any](value T) T {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		cancel()
	}
	return value // finds: this return
}

type box[T any] struct{ value T }

func (container *box[T]) open() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		cancel()
	}
} // finds: this return

func (container *box[T]) closed() {
	_, cancel := context.WithCancel(background)
	defer cancel()
}

func instantiated() {
	_ = generic[int](1)
	(&box[string]{}).open()
}

// An alias of context.CancelFunc changes nothing.
type stopFunction = context.CancelFunc

func aliasedCancelType() {
	var stop stopFunction
	_, stop = context.WithCancel(background) // finds: not used on all paths
	if condition {
		stop()
	}
} // finds: this return

// Embedded: a cancel function promoted from an embedded field is a field, not a variable.
type embedding struct {
	holder
}

func assignedIntoPromotedField(into *embedding) {
	_, into.cancel = context.WithCancel(background)
}

// A function literal at package level is judged as any other.
var packageLiteral = func() {
	_, cancel := context.WithCancel(background) // finds: not used on all paths
	if condition {
		cancel()
	}
} // finds: this return
