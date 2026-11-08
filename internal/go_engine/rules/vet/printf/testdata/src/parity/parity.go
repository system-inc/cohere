// Package parity holds a violation of every message printf can report and a clean case beside each, in a file with
// no Go version, which the gated checks read as unknown: the non-constant format, %q and %d checks fail open there,
// and the %w pointer check and wrappers found through interfaces apply. The files beside it hold each version gate
// on both sides.
package parity

import (
	"errors"
	"fmt"
	"log"
	"os"
	"parity/dependency"
	"testing"
	"unsafe"
)

// Formats that do not parse.
func parseErrors(text string, number int) {
	fmt.Printf("%")                                 // Missing verb at end of string.
	fmt.Printf("%-")                                // Flags, then the end.
	fmt.Printf("%12")                               // A width that runs to the end.
	fmt.Printf("%.3")                               // A precision that runs to the end.
	fmt.Printf("%[1", number)                       // Missing closing ].
	fmt.Printf("%[0]d", number)                     // An index of zero.
	fmt.Printf("%[x]d", number)                     // An index that is no number.
	fmt.Printf("%[]d", number)                      // An empty index.
	fmt.Printf("%[99999999999]d", 1)                // An index past 32 bits.
	fmt.Printf("%[1]", number)                      // An index, then the end.
	fmt.Printf("%.[2", number)                      // A precision's index missing its ].
	fmt.Printf("%d %d", number, number)             // Clean.
	fmt.Printf("%[2]d %[1]d", 1, 2)                 // Clean, indexed.
	fmt.Printf("%[3]d", 1, 2)                       // Reads arg #3 of 2.
	fmt.Printf("%[2]*[1]d", 1, 2)                   // Clean, an indexed width.
	fmt.Printf("%[2]*.[1]*[3]d", 1, 2, 3)           // Clean, an indexed width and precision.
	fmt.Printf("%d %d", number)                     // Reads arg #2 of 1.
	fmt.Printf("%d", number, number)                // Needs 1 arg but has 2.
	fmt.Printf("%d %[1]d", number, 2)               // Indexed, so the extra argument is ignored.
	fmt.Printf("no directives", number)             // Arguments but no directives.
	fmt.Printf("no directives")                     // Clean.
	fmt.Printf("%d%%", number)                      // Clean, %% uses no argument.
	fmt.Printf("%10.2%%d", 4)                       // A percent verb with a width and precision, then a verb.
	fmt.Sprintf("%"+("s"), "hi", 3)                 // A constant expression, reported at the whole argument.
	fmt.Printf(text)                                // Non-constant, but the version is unknown, so it fails open.
	fmt.Printf(text, number)                        // Non-constant with arguments.
	fmt.Printf("\t\u00e9\x41%d %s", "x", 1)         // Escapes before the operations, which shift where each is.
	fmt.Printf("\U0001F600 %[2]*.[1]*[3]s", 1, "2") // An indexed operation after a rune of four bytes.
	fmt.Printf(`raw
	%d`, "x") // A raw string over two lines.
	fmt.Printf("%d"+"%s", 1, 2) // Two literals added, so no literal to place the operation in.
	_ = text
}

// Verbs, flags and the types of their operands.
func verbsAndTypes(text string, number int, pointer *int, values []int, bytes []byte, array [3]byte, ints [2]int,
	boxed []any) {
	fmt.Printf("%z", number)                  // Unknown verb.
	fmt.Printf("%#d", number)                 // Unrecognized flag #.
	fmt.Printf("%0d %-5d", 1, 2)              // Clean: 0 is never reported, - is known.
	fmt.Printf("% x %+q", text, text)         // Clean flags.
	fmt.Printf("%#t", true)                   // Unrecognized flag #.
	fmt.Printf("%d", text)                    // Wrong type string.
	fmt.Printf("%s", number)                  // Wrong type int.
	fmt.Printf("%c", pointer)                 // Wrong type, use %p for a pointer.
	fmt.Printf("%x", pointer)                 // Clean, %x takes a pointer.
	fmt.Printf("%d", pointer)                 // Clean in a file of unknown version.
	fmt.Printf("%p", values)                  // Clean, a slice with %p.
	fmt.Printf("%s %s", bytes, array)         // Clean, byte slices and arrays with %s.
	fmt.Printf("%s", values)                  // Wrong type []int.
	fmt.Printf("%d", values)                  // Clean, []int with %d.
	fmt.Printf("%d", ints)                    // Clean, [2]int with %d.
	fmt.Printf("%s", ints)                    // Wrong type [2]int.
	fmt.Printf("%q", number)                  // Clean in a file of unknown version.
	fmt.Printf("%q", 3.5)                     // Wrong type float64.
	fmt.Printf("%t", 1)                       // Wrong type int.
	fmt.Printf("%t", true)                    // Clean.
	fmt.Printf("%e %g", 1.5, 2i)              // Clean, a float and a complex.
	fmt.Printf("%e", number)                  // Wrong type int.
	fmt.Printf("%U %c", 'x', 'y')             // Clean runes.
	fmt.Printf("%c", 'x', number)             // Needs 1 arg but has 2.
	fmt.Printf("%x", unsafe.Pointer(pointer)) // Clean.
	fmt.Printf("%c", unsafe.Pointer(pointer)) // Wrong type, use %p for a pointer.
	fmt.Printf("%d", nil)                     // Wrong type untyped nil.
	fmt.Printf("%s", nil)                     // Wrong type untyped nil.
	fmt.Printf("%v %T", nil, nil)             // Clean.
	fmt.Printf("%d", map[int]int{})           // Clean, a map of ints with %d.
	fmt.Printf("%d", map[string]int{})        // Wrong type, its key.
	fmt.Printf("%p", map[string]int{})        // Clean.
	fmt.Printf("%d", make(chan int))          // Wrong type chan int.
	fmt.Printf("%p", make(chan int))          // Clean.
	fmt.Printf("%x", make(chan int))          // Clean, %x takes a pointer.
	fmt.Printf("%d", struct{ A, B int }{})    // Clean, every field an int.
	fmt.Printf("%d", struct {
		A int
		B string
	}{}) // Wrong type, a field is a string.
	fmt.Printf("%d", &struct{ A int }{})          // Clean, a top-level pointer to a struct.
	fmt.Printf("%d", []*struct{ A int }{})        // Wrong type, a nested pointer to a struct.
	fmt.Printf("%s", struct{ err error }{})       // Wrong type, an unexported error field.
	fmt.Printf("%s", struct{ Err error }{})       // Clean, an exported one.
	fmt.Printf("%d", parseErrors)                 // A func value, not called.
	fmt.Printf("%p %T", parseErrors, parseErrors) // Clean.
	fmt.Printf("%v", interface{}(number))         // Clean.
	fmt.Printf("%z", interface{}(number))         // Clean, an interface may hold a Formatter.
	fmt.Printf("%z", formatted{})                 // Clean, a Formatter takes any verb.
	fmt.Printf("%#z", formatted{})                // Clean, and any flag.
	fmt.Printf("%d", stringer{})                  // Wrong type stringer.
	fmt.Printf("%s", stringer{})                  // Clean, it has String.
	fmt.Printf("%s", errorValue{})                // Clean, it has Error.
	fmt.Printf("%s", &number)                     // Wrong type *int.
	fmt.Printf("%v", 1, 2)                        // Needs 1 arg but has 2.
	fmt.Printf("%d %d", boxed...)                 // Passed with ..., so nothing can be counted.
	fmt.Printf("%d %d %d", 1, values)             // Wrong type []int is fine with %d, reads arg #3 of 2.
}

// Dynamic widths and precisions.
func stars(text string, number int, values []any) {
	fmt.Printf("%*d", number, number)        // Clean.
	fmt.Printf("%*d", text, number)          // Non-int as argument of *.
	fmt.Printf("%.*s", 3.5, text)            // Non-int as argument of *.
	fmt.Printf("%*d", number)                // Reads arg #2 of 1.
	fmt.Printf("%*d", values...)             // Passed with ..., nothing to see.
	fmt.Printf("%[2]*d", number, text)       // Non-int as argument of *, through an index.
	fmt.Printf("%-*s|", number, text)        // Clean.
	fmt.Printf("%*.*f", number, number, 1.5) // Clean.
}

// Wrapping errors.
func wrapping(err error, value parityError) {
	_ = fmt.Errorf("%w", err)                    // Clean.
	_ = fmt.Errorf("%w", &value)                 // A pointer to an error type, which defeats errors.Is.
	_ = fmt.Errorf("%w", value)                  // Clean.
	_ = fmt.Errorf("%w", "text")                 // Wrong type string.
	_ = fmt.Errorf("%w %w", err, errors.New("")) // Clean, two wrapped.
	_ = fmt.Errorf("%#w", err)                   // Clean, %w takes every flag.
	fmt.Printf("%w", err)                        // Printf does not support %w.
	log.Printf("%w", err)                        // log.Printf does not support %w.
	_ = fmt.Sprintf("%w", err)                   // Sprintf does not support %w.
	var pointer *pointerError
	_ = fmt.Errorf("%w", pointer) // Clean, Error is on the pointer.
}

// String and Error methods that format their own receiver.
type stringer struct{ name string }

func (value stringer) String() string {
	fmt.Println(value)                // Recursive call to String.
	_ = fmt.Sprintf("%s", value)      // Recursive String method call.
	_ = fmt.Sprintf("%v", &value)     // Recursive, through the address.
	_ = fmt.Sprintf("%d", value)      // Wrong type, reported before recursion.
	_ = fmt.Sprintf("%#v", value)     // Clean, # keeps fmt from calling String.
	_ = fmt.Sprintf("%#x", value)     // Recursive: # does not, with %x.
	_ = fmt.Sprintf("%T", value)      // Clean.
	_ = fmt.Sprintf("%s", value.name) // Clean.
	return value.name
}

type errorValue struct{ text string }

func (value *errorValue) Error() string {
	_ = fmt.Sprint(value)         // Recursive call to Error.
	_ = fmt.Sprintf("%s", value)  // Recursive Error method call.
	_ = fmt.Sprintf("%s", *value) // Clean, a copy is no receiver.
	return value.text
}

type generic[T any] struct{ value T }

func (box generic[T]) String() string {
	return fmt.Sprintf("%v", box) // Recursive, through the generic receiver's origin.
}

type formatterStringer struct{}

func (formatterStringer) Format(fmt.State, rune) {}

func (value formatterStringer) String() string {
	return fmt.Sprintf("%s", value) // Clean, it has a Format method.
}

type formatted struct{}

func (formatted) Format(state fmt.State, verb rune) {}

type notQuiteFormatter struct{}

func (notQuiteFormatter) Format(state fmt.State, verb int) {}

func formatters() {
	fmt.Printf("%z", notQuiteFormatter{}) // Unknown verb, its Format takes no rune.
}

type parityError struct{}

func (parityError) Error() string { return "" }

type pointerError struct{}

func (*pointerError) Error() string { return "" }

// Print functions given what looks like a format.
func prints(text string, number int, stream *os.File) {
	fmt.Println("%d", number)         // Possible directive %d.
	fmt.Println("50%")                // Clean, a trailing percent.
	fmt.Println("%2F%3a")             // Clean, URL escapes.
	fmt.Println("%2F then %s", text)  // Possible directive %s, after an escape.
	fmt.Println("x % y")              // Clean, a space is no flag.
	fmt.Println("%+v and %v")         // Possible directive %+v.
	fmt.Println("%[1]*d", 1)          // Possible directive with an index.
	fmt.Println(os.Stdout, "text")    // Takes no io.Writer.
	fmt.Println(os.Stderr)            // Takes no io.Writer.
	fmt.Println(stream, "text")       // Clean, not os.Std.
	fmt.Fprintln(os.Stdout, "%d", 1)  // Possible directive: Fprintln's print arguments start after the writer.
	fmt.Print(prints)                 // A func value, not called.
	fmt.Print(text, prints, number)   // A func value among others.
	fmt.Print(text, number)           // Clean.
	log.Print("%s", text)             // Possible directive.
	log.Fatalln()                     // Clean, nothing.
	_ = fmt.Sprint("text", number)    // Clean.
	_ = fmt.Append(nil, "%d", number) // Possible directive.
}

// Known functions of the standard library, and methods of testing.
func knownFunctions(test *testing.T, benchmark testing.TB, logger *log.Logger) {
	test.Errorf("%d", "text")        // Wrong type string.
	test.Log("%d", 1)                // Possible directive.
	test.Skipf("%s")                 // Reads arg #1 of 0.
	benchmark.Fatalf("%d", "")       // Wrong type string, through testing.TB.
	logger.Printf("%s", 1)           // Wrong type int.
	logger.Panicln("%v", 1)          // Possible directive.
	_ = fmt.Appendf(nil, "%d", "")   // Wrong type string.
	fmt.Fprintf(os.Stdout, "%d", "") // Wrong type string.
}

// Calls through the dependency's wrappers, each known by its fact.
func dependencies(text string) {
	dependency.Logf("%d", text)                // Wrong type string.
	dependency.Logf("%d", 1)                   // Clean.
	dependency.Logln(1, "%d", 2)               // Possible directive.
	dependency.Logln(1, os.Stdout)             // Clean: the print arguments start after level.
	_ = dependency.Failf("%w", errors.New("")) // Clean.
	_ = dependency.Failf("%d", text)           // Wrong type string.
	dependency.Indirectf("%s")                 // Reads arg #1 of 0.
	dependency.Generic(1, "%d", text)          // Wrong type string, a generic function.
	dependency.Generic[string]("", "%s", 1)    // Wrong type int, instantiated.
	dependency.Box[int]{}.Printf("%d", text)   // Wrong type string, a generic receiver.
	box := &dependency.Box[string]{}
	box.Print("%d", 1)                 // Possible directive.
	dependency.Assigned("%d", text)    // Wrong type string, a variable with a fact.
	dependency.Unassigned("%d", text)  // Wrong type string: no fact, but assigned a wrapper in init below.
	dependency.Hooks.Logf("%d", text)  // Wrong type string, a field with a fact.
	dependency.Hooks.Other("%d", text) // Wrong type string: assigned a wrapper in init below.
	dependency.NotWrapper("%d", text)  // Clean, no wrapper.
	var outer Outer
	outer.Logf("%d", text) // Wrong type string, a promoted method.
	logf := dependency.Logf
	logf("%d", text)                        // Clean: a method value's variable is no wrapper.
	dependency.NewLogger().Logf("%d", text) // Wrong type string, through the interface's fact.
}

// Outer promotes the dependency's wrapper.
type Outer struct {
	dependency.Inner
}

func init() {
	dependency.Unassigned = func(format string, arguments ...any) {
		fmt.Printf(format, arguments...)
	}
	dependency.Unassigned("%d", "text") // Wrong type string, a wrapper here though it has no fact.
	dependency.Hooks.Other = func(format string, arguments ...any) {
		fmt.Printf(format, arguments...)
	}
	dependency.Hooks.Other("%d", "text") // Wrong type string, a field assigned here.
}
