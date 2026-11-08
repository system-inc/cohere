// Package parity holds every shape S1038's three forms branch on, the near misses each leaves alone, and the edges
// its patterns could be read more than one way at, so the original and the port are compared on all of them.
package parity

import (
	"fmt"
	"io"
	"log"
	"os"
	"testing"
)

const format = "%d"

var packageSprintf = fmt.Sprintf

// The fmt form: each print with its arity, the Println forms only with a literal format.
func formatPrints(writer io.Writer, argument string, values []any) {
	fmt.Print(fmt.Sprintf("%d", 1))               // reported
	fmt.Sprint(fmt.Sprintf("%d", 1))              // reported
	fmt.Fprint(writer, fmt.Sprintf("%d", 1))      // reported
	fmt.Println(fmt.Sprintf("%d", 1))             // reported
	fmt.Sprintln(fmt.Sprintf("%d", 1))            // reported
	fmt.Fprintln(os.Stdout, fmt.Sprintf("%d", 1)) // reported
	fmt.Println(fmt.Sprintf(`%d`, 1))             // reported: a raw string is a literal
	fmt.Println(fmt.Sprintf(("%d"), 1))           // reported: the parentheses are stripped
	fmt.Println(fmt.Sprintf(format, 1))           // left alone: a named constant is no literal
	fmt.Fprintln(writer, fmt.Sprintf(format, 1))  // left alone
	fmt.Println(fmt.Sprintf(argument, 1))         // left alone
	fmt.Print(fmt.Sprintf(argument, 1))           // reported: Print needs no newline
	fmt.Print(fmt.Sprintf(argument, values...))   // reported: the arguments are never read

	// Parentheses at every level.
	(fmt.Print)(fmt.Sprintf("%d", 1))            // reported
	fmt.Print((fmt.Sprintf("%d", 1)))            // reported
	fmt.Print((fmt.Sprintf)("%d", 1))            // reported
	(fmt.Fprint)(writer, (fmt.Sprintf("%d", 1))) // reported

	// The wrong arity for each.
	fmt.Print("prefix", fmt.Sprintf("%d", 1))            // left alone: Print with two
	fmt.Println("prefix", fmt.Sprintf("%d", 1))          // left alone
	fmt.Fprint(writer, "prefix", fmt.Sprintf("%d", 1))   // left alone: Fprint with three
	fmt.Fprintln(writer, "prefix", fmt.Sprintf("%d", 1)) // left alone
	fmt.Fprint(writer, fmt.Sprintf("%d", 1), "suffix")   // left alone: Sprintf is not last

	// Not fmt.Sprintf, or not a print.
	sprintf := fmt.Sprintf
	fmt.Print(sprintf("%d", 1))        // left alone: through a variable
	fmt.Print(packageSprintf("%d", 1)) // left alone: through a package's variable
	fmt.Print(fmt.Sprint("%d", 1))     // left alone
	fmt.Print(Sprintf[int]("%d", 1))   // left alone: a generic function of the package's own
	fmt.Print(Sprintf("%d", 1))        // left alone: inferred
	fmt.Printf(fmt.Sprintf("%d", 1))   // left alone: already formatting
	fmt.Errorf(fmt.Sprintf("%d", 1))   // left alone
	print := fmt.Print
	print(fmt.Sprintf("%d", 1)) // left alone: a print through a variable
	_ = fmt.Sprintf("%d", 1)
}

// Sprintf is a generic function of the package's own, named as fmt's is.
func Sprintf[Value any](format string, values ...Value) string { return "" }

// Errorer has the nine names, and none of testing's or log's.
type Errorer struct{}

func (Errorer) Error(string)   {}
func (Errorer) Errorf(string)  {}
func (Errorer) Print(string)   {}
func (Errorer) Println(string) {}

// Wrapper embeds a test and declares Errorf with another signature, so Error and Errorf are not declared together.
type Wrapper struct {
	*testing.T
}

func (Wrapper) Errorf(count int) {}

// ValueWrapper embeds a test by value, so its methods are promoted from *testing.common when it is addressable.
type ValueWrapper struct {
	testing.T
}

// Generic embeds a test in a generic type.
type Generic[Value any] struct {
	*testing.T
	value Value
}

// Bigger is an interface embedding testing.TB, whose methods are testing.TB's own.
type Bigger interface {
	testing.TB
	Extra()
}

// Fielded holds functions as fields named as testing's methods are.
type Fielded struct {
	Error  func(...any)
	Errorf func(string, ...any)
}

// Logged embeds a logger.
type Logged struct {
	*log.Logger
}

// Sprintfer has a method named Sprintf.
type Sprintfer struct{}

func (Sprintfer) Sprintf(format string, values ...any) string { return "" }

// The method form.
func methodPrints(
	test *testing.T,
	benchmark *testing.B,
	fuzz *testing.F,
	common testing.TB,
	bigger Bigger,
	logger *log.Logger,
	logged Logged,
	generic Generic[int],
	wrapper Wrapper,
	fielded Fielded,
	errorer Errorer,
	sprintfer Sprintfer,
) {
	test.Error(fmt.Sprintf("%d", 1))          // reported
	test.Fatal(fmt.Sprintf("%d", 1))          // reported
	test.Log(fmt.Sprintf("%d", 1))            // reported
	test.Skip(fmt.Sprintf("%d", 1))           // reported
	benchmark.Error(fmt.Sprintf("%d", 1))     // reported
	fuzz.Error(fmt.Sprintf("%d", 1))          // reported
	common.Log(fmt.Sprintf("%d", 1))          // reported
	bigger.Error(fmt.Sprintf("%d", 1))        // reported: testing.TB's methods, through the embedding
	generic.Error(fmt.Sprintf("%d", 1))       // reported: a generic type's promoted method
	logger.Print(fmt.Sprintf("%d", 1))        // reported
	logger.Println(fmt.Sprintf("%d", 1))      // reported
	logger.Fatalln(fmt.Sprintf("%d", 1))      // reported
	logger.Panicln(fmt.Sprintf("%d", 1))      // reported
	logged.Panic(fmt.Sprintf("%d", 1))        // reported
	logged.Logger.Fatal(fmt.Sprintf("%d", 1)) // reported: the receiver is a selector itself

	// Parentheses: the receiver is bound without them, the selector with them.
	(test).Error(fmt.Sprintf("%d", 1)) // reported: t.Errorf beside (t).Error
	(test.Error)(fmt.Sprintf("%d", 1)) // reported
	(test).Log((fmt.Sprintf("%d", 1))) // reported

	var value ValueWrapper
	value.Error(fmt.Sprintf("%d", 1)) // reported: addressable, so *testing.common's methods are there

	// Near misses.
	wrapper.Error(fmt.Sprintf("%d", 1))    // left alone: Errorf is the wrapper's own
	wrapper.Fatal(fmt.Sprintf("%d", 1))    // reported: Fatalf is still testing's
	fielded.Error(fmt.Sprintf("%d", 1))    // left alone: a field, not a method
	errorer.Error(fmt.Sprintf("%d", 1))    // left alone: not testing's
	errorer.Print(fmt.Sprintf("%d", 1))    // left alone
	test.Error(fmt.Sprintf("%d", 1), 2)    // left alone: two arguments
	test.Error("%d", fmt.Sprintf("%d", 1)) // left alone
	test.Error(sprintfer.Sprintf("%d", 1)) // left alone: a method named Sprintf
	test.Errorf(fmt.Sprintf("%d", 1))      // left alone: already formatting
	method := test.Error
	method(fmt.Sprintf("%d", 1))                   // left alone: a method value through a variable
	(*testing.T).Error(test, fmt.Sprintf("%d", 1)) // left alone: a method expression of two arguments
	logger.Printf(fmt.Sprintf("%d", 1))            // left alone
	fmt.Print(fmt.Sprintf("%d", 1))                // reported by the fmt form alone
}

// The log form.
func logPrints() {
	log.Fatal(fmt.Sprintf("%d", 1))           // reported
	log.Fatalln(fmt.Sprintf("%d", 1))         // reported
	log.Panic(fmt.Sprintf("%d", 1))           // reported
	log.Panicln(fmt.Sprintf("%d", 1))         // reported
	log.Print(fmt.Sprintf("%d", 1))           // reported
	log.Println(fmt.Sprintf("%d", 1))         // reported
	(log.Print)(fmt.Sprintf("%d", 1))         // reported
	log.Print((fmt.Sprintf("%d", 1)))         // reported
	log.Printf(fmt.Sprintf("%d", 1))          // left alone
	log.Print("prefix", fmt.Sprintf("%d", 1)) // left alone
	log.Print(fmt.Sprint("%d", 1))            // left alone
	logPrint := log.Print
	logPrint(fmt.Sprintf("%d", 1)) // left alone
}
