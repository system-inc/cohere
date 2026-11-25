package parity

import (
	"fmt"
	. "fmt"
	"log"
)

// anything is an alias of the empty interface, which a wrapper's arguments may be.
type anything = any

// text is an alias of string, which a format parameter is not taken for.
type text = string

// printer is a named function type, whose calls the printf check passes by and the print check reads.
type printer func(format string, arguments ...any)

// lineWriter is a named function type of print shape.
type lineWriter func(arguments ...any)

// Declared wrappers of each kind, and the shapes that keep a function from being one.
func logf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

func aliasedf(format string, arguments ...anything) {
	fmt.Printf(format, arguments...)
}

func textf(format text, arguments ...any) {
	fmt.Printf(format, arguments...) // A print-shaped candidate forwarding to printf, without a format it owns.
}

func logln(arguments ...any) {
	fmt.Println(arguments...)
}

func failf(format string, arguments ...any) error {
	return fmt.Errorf(format, arguments...)
}

func throughf(format string, arguments ...any) {
	logf(format, arguments...)
}

func laterf(format string, arguments ...any) {
	beforef(format, arguments...) // Calls a wrapper declared after it.
}

func beforef(format string, arguments ...any) {
	log.Printf(format, arguments...)
}

func missingDotsf(format string, arguments ...any) {
	fmt.Printf(format, arguments) // Missing ... in args forwarded to printf-like function.
}

func missingDotsln(arguments ...any) {
	fmt.Println(arguments) // Missing ... in args forwarded to print-like function.
}

func missingDotsError(format string, arguments ...any) error {
	return fmt.Errorf(format, arguments) // Missing ... in args forwarded to errorf-like function.
}

func extraArgumentf(first string, arguments ...any) {
	fmt.Printf("%s %v", first, arguments) // Clean: adding ... would not compile.
}

func reassignedf(format string, arguments ...any) {
	arguments = append(arguments, 1)
	fmt.Printf(format, arguments...) // No wrapper: its arguments changed first.
}

func addressedf(format string, arguments ...any) {
	_ = &format
	fmt.Printf(format, arguments...) // No wrapper: its format's address is taken.
}

func afterf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
	format = "" // A wrapper still: the change comes after the call.
	_ = format
}

func notVariadic(format string, arguments []any) {
	fmt.Printf(format, arguments...)
}

func typedArgumentsf(format string, arguments ...string) {
	fmt.Println(format, arguments)
}

func dotImportedf(format string, arguments ...any) {
	Printf(format, arguments...)
}

func ifFalsef(format string, arguments ...any) string {
	if false {
		_ = fmt.Sprintf(format, arguments...) // Enables the check.
	}
	return fmt.Sprintf("("+format+")", arguments...)
}

// Warnf and warn are known by the -funcs flag the tests set, with no body to be found from.
func Warnf(format string, arguments ...any) {}

func warn(arguments ...any) {}

// Methods and generics.
type receiver struct{}

func (receiver) Logf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

func (*receiver) Logln(level int, arguments ...any) {
	fmt.Println(arguments...)
}

type holder[T any] struct{ value T }

func (holder[T]) Logf(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

func genericf[T any](value T, format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

func callsWrappers(value string, number int) {
	logf("%d", value)               // Wrong type string.
	logf("%d", number)              // Clean.
	aliasedf("%d", value)           // Wrong type string.
	textf("%d", value)              // Clean: no wrapper.
	logln("%d", number)             // Possible directive.
	_ = failf("%w", value)          // Wrong type string for %w.
	_ = failf("%w", fmt.Errorf("")) // Clean.
	throughf("%s")                  // Reads arg #1 of 0.
	laterf("%s")                    // Reads arg #1 of 0.
	missingDotsf("%d", value)       // Clean: a wrapper that forwards wrongly is no wrapper.
	reassignedf("%d", value)        // Clean.
	addressedf("%d", value)         // Clean.
	afterf("%d", value)             // Wrong type string.
	notVariadic("%d", nil)          // Clean.
	typedArgumentsf("%d", value)    // Clean.
	dotImportedf("%d", value)       // Wrong type string.
	Printf("%d", value)             // Wrong type string, through the dot import.
	_ = ifFalsef("%d", value)       // Wrong type string.
	Warnf("%d", value)              // Wrong type string, known by the flag.
	Warnf("%d", number)             // Clean.
	warn("%d", number)              // Possible directive, known by the flag.
	receiver{}.Logf("%d", value)    // Wrong type string.
	(&receiver{}).Logln(1, "%d", 2) // Possible directive.
	holder[int]{}.Logf("%d", value) // Wrong type string, an instantiated receiver.
	genericf(1, "%d", value)        // Wrong type string, a generic function.
	genericf[string]("", "%s", 1)   // Wrong type int, instantiated.
	method := receiver{}.Logf
	method("%d", value)                    // Clean: a method value's variable is no wrapper.
	receiver.Logf(receiver{}, "%d", value) // A method expression.
}

// Function literals assigned to variables and fields.
var packageLevelf = func(format string, arguments ...any) {
	fmt.Printf(format, arguments...)
}

var fields struct {
	logf  func(format string, arguments ...any)
	plain func(format string, arguments ...any)
}

func literals(value string) {
	localf := func(format string, arguments ...any) {
		fmt.Printf(format, arguments...)
	}
	localf("%d", value) // Wrong type string.
	var assignedf func(format string, arguments ...any)
	assignedf = func(format string, arguments ...any) {
		log.Printf(format, arguments...)
	}
	assignedf("%d", value) // Wrong type string.
	var declaredf = func(format string, arguments ...any) {
		fmt.Printf(format, arguments...)
	}
	declaredf("%d", value)     // Wrong type string.
	packageLevelf("%d", value) // Wrong type string.
	fields.logf = func(format string, arguments ...any) {
		fmt.Printf(format, arguments...)
	}
	fields.logf("%d", value)  // Wrong type string, a field.
	fields.plain("%d", value) // Clean.
	var named printer = func(format string, arguments ...any) {
		fmt.Printf(format, arguments...)
	}
	named("%d", value) // Clean: a named function type is passed by.
	var lines lineWriter = func(arguments ...any) {
		fmt.Println(arguments...)
	}
	lines("%d", value) // Possible directive: the print check reads the underlying type.
	first, second := func(format string, arguments ...any) {
		fmt.Printf(format, arguments...)
	}, 2
	first("%d", value) // Wrong type string, assigned among others.
	_ = second
	nested := func(format string, arguments ...any) {
		inner := func(format string, arguments ...any) {
			fmt.Printf(format, arguments...)
		}
		inner(format, arguments...)
	}
	nested("%d", value) // Wrong type string, through a closure in a closure.
	var notAssigned = []func(string, ...any){func(format string, arguments ...any) {
		fmt.Printf(format, arguments...)
	}}
	notAssigned[0]("%d", value) // Clean: an element is no variable.
	logf, dotsf := nested, missingDotsf
	logf("%d", value)  // Clean: the variable is assigned no function literal.
	dotsf("%d", value) // Clean.
}
