// Package flagged holds panics that give no reason and Must calls outside package initialization.
package flagged

import (
	"errors"
	"fmt"
	"regexp"
	"text/template"
)

func unexplained(value int) {
	if value < 0 {
		panic("negative") // want `^This panic stops the process and says nothing about why it can never happen\.`
	}
}

func formatted(value int) {
	panic(fmt.Sprintf("value %d", value)) // want `This panic stops the process`
}

func parenthesized() {
	(panic)("parenthesized") // want `This panic stops the process`
}

func deferred() {
	defer panic("deferred") // want `This panic stops the process`
}

// A reason above the if that holds the panic speaks for the if, not for the panic statement inside it.
func reasonAboveTheIf(value int) {
	// Invariant: callers pass a value they read from the table.
	if value < 0 {
		panic("negative") // want `This panic stops the process`
	}
}

func reasonSeparated() {
	// Invariant: this is a paragraph away from the panic below.

	panic("separated") // want `This panic stops the process`
}

// A reason trailing the code on the line above speaks for that line, not for the panic below it.
func reasonTrailingTheLineAbove(value int) {
	_ = value         // Invariant: this speaks for the line it ends.
	panic("trailing") // want `This panic stops the process`
}

func markerMidSentence() {
	// This once held an Invariant: the marker sits mid-sentence here, so it is no excuse.
	panic("mid-sentence") // want `This panic stops the process`
}

func markerWithoutColon() {
	// Invariant the table is built from constants, but with no colon.
	panic("no colon") // want `This panic stops the process`
}

// A comment before the panic on its own line is not at the end of the line.
func commentBeforeTheStatement() {
	/* Invariant: written before the call, not after it. */ panic("before") // want `This panic stops the process`
}

func emptyReason() {
	// Invariant:
	panic("empty") // want `^This panic is marked as an invariant and gives no reason\.`
}

func emptyTrailingReason() {
	panic("empty") /* Invariant: */ // want `This panic is marked as an invariant and gives no reason`
}

func emptyBlockReason() {
	/* Invariant: */
	panic("empty") // want `This panic is marked as an invariant and gives no reason`
}

// A recover in the enclosing function is not this closure's, so the closure's panic is no re-panic.
func capturedRecover() {
	recovered := recover()
	go func() {
		panic(recovered) // want `This panic stops the process`
	}()
}

// A recovered value written again may hold anything by the time it is panicked.
func reassignedRecover() {
	defer func() {
		recovered := recover()
		recovered = errors.New("replaced")
		panic(recovered) // want `This panic stops the process`
	}()
}

func recoverByAddress() {
	defer func() {
		recovered := recover()
		replace(&recovered)
		panic(recovered) // want `This panic stops the process`
	}()
}

func replace(target *any) {
	*target = "replaced"
}

// A value derived from the recovered one is a new panic, not the same one raised again.
func wrappedRecover() {
	defer func() {
		if recovered := recover(); recovered != nil {
			panic(fmt.Errorf("wrapped: %v", recovered)) // want `This panic stops the process`
		}
	}()
}

// A function of this package named recover is not the builtin, so its result is no recovered value.
func shadowedRecover() {
	recover := func() any { return "shadowed" }
	panic(recover()) // want `This panic stops the process`
}

var deferredPanic = func() {
	panic("in a package-level function literal") // want `This panic stops the process`
}

type loader struct{}

func (loader) MustLoad(name string) string { return name }

func MustLoad(name string) string { return name }

func MustFirst[Element any](elements []Element) Element { return elements[0] }

var sharedLoader loader

func mustCalls(pattern string) {
	_ = regexp.MustCompile(pattern)                        // want "^`regexp.MustCompile` panics on a bad input"
	_ = template.Must(template.New("page").Parse(pattern)) // want "`template.Must` panics on a bad input"
	_ = MustLoad(pattern)                                  // want "^`MustLoad` panics on a bad input"
	_ = sharedLoader.MustLoad(pattern)                     // want "`MustLoad` panics on a bad input"
	_ = MustFirst([]string{pattern})                       // want "`MustFirst` panics on a bad input"
	_ = MustFirst[string]([]string{pattern})               // want "`MustFirst` panics on a bad input"
	var local = regexp.MustCompile("local")                // want "`regexp.MustCompile` panics on a bad input"
	_ = local
}

// A function literal stored by a package-level initializer runs whenever it is called, not once at start.
var matcher = func(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern) // want "`regexp.MustCompile` panics on a bad input"
}

var build = func() {
	_ = regexp.MustCompile("stored") // want "`regexp.MustCompile` panics on a bad input"
}

func useStored() {
	build()
	_ = matcher("later")
}

var handler func()

// An init function runs at start, but a literal it stores or starts as a goroutine runs later.
func init() {
	handler = func() {
		_ = regexp.MustCompile("stored by init") // want "`regexp.MustCompile` panics on a bad input"
	}
	go func() {
		_ = regexp.MustCompile("started by init") // want "`regexp.MustCompile` panics on a bad input"
	}()
}

// A method named init is called like any other method, so it is no init function.
func (loader) init() {
	_ = regexp.MustCompile("method") // want "`regexp.MustCompile` panics on a bad input"
}
