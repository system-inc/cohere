// Package flagged holds typed vars whose initializer restates their type's zero value, each of which the rule must
// report and offer to drop.
package flagged

import (
	"time"
	"unsafe"
)

// The five shapes the rule is named for, at package level.
var count int = 0 // want "`count` starts at `0`, the zero value Go already gives every `int`"

var name string = "" // want "`name` starts at `\"\"`"

var done bool = false // want "`done` starts at `false`"

var readError error = nil // want "`readError` starts at `nil`.*`error`"

var pointer *int = nil // want "`pointer` starts at `nil`"

// A grouped declaration judges each spec on its own.
var (
	total    int     = 0  // want "`total` starts at `0`"
	label    string  = "" // want "`label` starts at"
	measured float64 = 1.5
)

// Other spellings of a basic zero: every base, a float, an exponent, an imaginary part, a raw string and a rune.
var (
	hexadecimal int        = 0x0    // want "`hexadecimal` starts at `0x0`"
	octal       int        = 0o0    // want "`octal` starts at `0o0`"
	binary      int        = 0b0    // want "`binary` starts at `0b0`"
	ratio       float64    = 0.0    // want "`ratio` starts at `0.0`"
	scaled      float64    = 0e5    // want "`scaled` starts at `0e5`"
	imaginary   complex128 = 0i     // want "`imaginary` starts at `0i`"
	raw         string     = ``     // want "`raw` starts at"
	terminator  rune       = '\x00' // want "`terminator` starts at"
	small       byte       = 0x00   // want "`small` starts at `0x00`"
)

// Celsius is a named number, whose zero is the zero of the float beneath it.
type Celsius float64

// Count is an alias, whose zero is int's.
type Count = int

// Reader is an interface declared here, whose zero is nil.
type Reader interface{ Read() string }

// A named basic type, an alias, a type from another package, and each kind of type whose zero is nil.
var (
	freezing Celsius        = 0   // want "`freezing` starts at `0`.*`Celsius`"
	tally    Count          = 0   // want "`tally` starts at `0`.*`Count`"
	timeout  time.Duration  = 0   // want "`timeout` starts at `0`.*`time.Duration`"
	items    []int          = nil // want "`items` starts at `nil`"
	index    map[string]int = nil // want "`index` starts at `nil`"
	signals  chan int       = nil // want "`signals` starts at `nil`"
	callback func()         = nil // want "`callback` starts at `nil`"
	boxed    any            = nil // want "`boxed` starts at `nil`.*`any`"
	source   Reader         = nil // want "`source` starts at `nil`.*`Reader`"
	address  unsafe.Pointer = nil // want "`address` starts at `nil`"
)

// Parentheses do not hide the zero.
var wrapped int = (0) // want "`wrapped` starts at `\\(0\\)`"

var wrappedNil error = (nil) // want "`wrappedNil` starts at `\\(nil\\)`"

// Several names, every one of them initialized to zero, lose the initializer together.
var first, second int = 0, 0 // want "`first, second` starts at `0, 0`"

// A comment after the value stays where it is.
var kept int = 0 // want "`kept` starts at `0`"

// A comment inside the initializer would be lost with it, so this is reported and left for the writer to fix.
var noted int = /* why */ 0 // want "`noted` starts at `0`"

// Locals in a function, a function literal, and a generic function are judged the same way.
func locals() (int, string, error) {
	var sum int = 0         // want "`sum` starts at `0`"
	var message string = "" // want "`message` starts at"
	var failure error = nil // want "`failure` starts at `nil`"
	run := func() bool {
		var finished bool = false // want "`finished` starts at `false`"
		return finished
	}
	run()
	return sum, message, failure
}

// A type parameter whose every type accepts nil has nil for its zero, so nil is a restated zero there too.
func emptyOf[Element any, Pointer ~*Element]() (Pointer, *Element) {
	var typed Pointer = nil  // want "`typed` starts at `nil`.*`Pointer`"
	var plain *Element = nil // want "`plain` starts at `nil`"
	return typed, plain
}
