// Package allowed holds panics that say why they cannot happen, re-panics, and Must calls in package-level
// initializers, all of which the rule must leave alone.
package allowed

import (
	"fmt"
	"regexp"
	"text/template"
)

func reasonAbove(value int) {
	if value < 0 {
		// Invariant: callers pass a value they read from the table, which holds no negative entry.
		panic("negative")
	}
}

func reasonAtTheEnd(value int) {
	panic(fmt.Sprintf("value %d", value)) // Invariant: the switch above covers every other value.
}

func reasonInAGroup() {
	// The table is built once from constants.
	//
	// Invariant: every name the caller passes is a key of the table.
	panic("missing")
}

func reasonInABlockComment() {
	/* Invariant: a block comment may say it too. */
	panic("block")
}

func reasonOnTheLastLine(value int) {
	panic(fmt.Sprintf(
		"value %d",
		value,
	)) // Invariant: the caller checked the value against the same bound.
}

func reasonInACase(value int) {
	switch value {
	case 0:
	default:
		// Invariant: value is a bit, decided by the mask above.
		panic("not a bit")
	}
}

func deferredWithAReason() {
	// Invariant: deferred only on a path that already broke the invariant.
	defer panic("deferred")
}

func rePanic() {
	defer func() {
		if recovered := recover(); recovered != nil {
			fmt.Println("cleaning up")
			panic(recovered)
		}
	}()
}

func rePanicDeclared() {
	defer func() {
		var recovered = recover()
		if recovered != nil {
			panic((recovered))
		}
	}()
}

func rePanicDirect() {
	defer func() {
		panic(recover())
	}()
}

type loader struct{}

func (loader) MustLoad(name string) string { return name }

func MustLoad(name string) string { return name }

func MustFirst[Element any](elements []Element) Element { return elements[0] }

var sharedLoader loader

var (
	pattern = regexp.MustCompile(`^[a-z]+$`)
	page    = template.Must(template.New("page").Parse("{{.}}"))
	loaded  = MustLoad("configuration")
	method  = sharedLoader.MustLoad("method")
	first   = MustFirst([]string{"first"})
	// A Must call anywhere in the initializer's expression runs once at start, however deep it sits.
	table = map[string]*regexp.Regexp{
		"word":   regexp.MustCompile(`\w+`),
		"number": regexp.MustCompile(`\d+`),
	}
	wrapped = fmt.Sprint(MustLoad("wrapped"))
)

// A function literal called on the spot in an initializer runs once at start, as do its arguments and the literals
// it calls on the spot in turn.
var immediate = func(name string) *regexp.Regexp {
	return func() *regexp.Regexp {
		return regexp.MustCompile(name)
	}()
}(MustLoad("argument"))

var (
	compiled *regexp.Regexp
	named    string
)

// An init function's body is package initialization, and so is a literal it calls on the spot.
func init() {
	compiled = regexp.MustCompile(`^\w+$`)
	func() {
		named = MustLoad("in a called literal")
	}()
}
