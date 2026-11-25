package parity

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

func buffer() *bytes.Buffer { return &bytes.Buffer{} }

// failure has an Error method of type func() string with a value receiver.
type failure struct{}

func (failure) Error() string { return "failure" }

// label is a defined string type, so a method returning it is not of type func() string.
type label string

// text is an alias of string, so a method returning it is of type func() string.
type text = string

// shapes has methods named String and Error of every other type, and of type func() string through an alias.
type shapes struct{}

func (shapes) String(width int) string { return "" }

func (shapes) Error() (string, error) { return "", nil }

func (*shapes) Name() string { return "shapes" }

func (*shapes) GoString() string { return "shapes" }

func (*shapes) Count() int { return 0 }

func (shapes) Bytes() []byte { return nil }

type labeled struct{}

func (labeled) String() label { return "" }

type aliased struct{}

func (aliased) String() text { return "" }

type variadic struct{}

func (variadic) String(widths ...int) string { return "" }

// Box and Pair are generic, with value and pointer receivers.
type Box[T any] struct{ value T }

func (Box[T]) String() string { return "box" }

type Pair[K comparable, V any] struct {
	key   K
	value V
}

func (*Pair[K, V]) Error() string { return "pair" }

// describer embeds fmt.Stringer, whose String it promotes.
type describer interface {
	fmt.Stringer
	Name() string
}

// wrapped embeds bytes.Buffer, and holder an error, each promoting its String or Error.
type wrapped struct{ bytes.Buffer }

type holder struct{ error }

// alias is another name for bytes.Buffer.
type alias = bytes.Buffer

func methods(err error, stringer fmt.Stringer, description describer, duration time.Duration) {
	var written bytes.Buffer
	var builder strings.Builder
	var shaped shapes
	err.Error()                      // reported
	failure{}.Error()                // reported
	written.String()                 // reported
	builder.String()                 // reported
	stringer.String()                // reported
	description.String()             // reported
	description.Name()               // clean: Name is not in stringmethods by default
	duration.String()                // reported
	time.Duration(1).String()        // reported
	time.Now().String()              // reported
	buffer().String()                // reported
	shaped.String(1)                 // clean: String takes a parameter
	shaped.Error()                   // clean: Error returns two results
	shaped.Name()                    // clean
	shaped.GoString()                // clean
	shaped.Count()                   // clean
	shaped.Bytes()                   // clean
	written.Bytes()                  // clean
	written.Len()                    // clean
	labeled{}.String()               // clean: label is not string
	aliased{}.String()               // reported: text is string
	variadic{}.String()              // clean: a variadic String is another type
	Box[int]{}.String()              // reported
	Box[string]{}.String()           // reported
	(&Pair[string, int]{}).Error()   // reported
	(&wrapped{}).String()            // reported, as bytes.Buffer's
	holder{}.Error()                 // reported, as error's
	(&alias{}).String()              // reported, as bytes.Buffer's
	_ = written.String()             // clean
	fmt.Println(written.String())    // clean
	defer written.String()           // clean
	go err.Error()                   // clean
	method := written.String         // clean
	method()                         // clean: a function value has no declared callee
	(*bytes.Buffer).String(&written) // reported: a method expression
	(*bytes.Buffer).Len(&written)    // clean
	error.Error(err)                 // reported: an interface's method expression
	fmt.Stringer.String(stringer)    // reported
	time.Duration.String(duration)   // reported
	Box[int].String(Box[int]{})      // reported: a generic type's method expression
	(*Pair[int, int]).Error(nil)     // reported
	(failure.Error)(failure{})       // reported: a method expression in parentheses
	(written.String)()               // reported: a method value in parentheses
	(err.Error)()                    // reported
	func() { written.String() }()    // reported, inside the literal
}

// Type parameters: a method of the constraint, and of a parameterized receiver.
func typeParameters[T fmt.Stringer, E interface{ Error() string }](value T, failed E, pair *Pair[string, E]) {
	value.String()     // reported
	failed.Error()     // reported
	pair.Error()       // reported
	_ = value.String() // clean
}
