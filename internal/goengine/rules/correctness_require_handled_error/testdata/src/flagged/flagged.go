// Package flagged holds calls that drop an error with no reason, each of which the rule must report. A want's
// regexp matches the backtick around the callee's name with a dot, since a raw string cannot hold one.
package flagged

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func save() error { return nil }

type store struct{}

func (*store) Save() error { return nil }

// failure is a concrete error type, which a result counts as an error by implementing.
type failure struct{}

func (*failure) Error() string { return "failure" }

func concrete() *failure { return nil }

// temporary is an interface that embeds error, so it is one.
type temporary interface {
	error
	Temporary() bool
}

func transient() temporary { return nil }

// errorFirst returns its error first, which is no less an error for the position.
func errorFirst() (error, int) { return nil, 0 }

func first[Value any](value Value) (Value, error) { return value, nil }

// sink has a Write shaped like io.Writer's and is no hash.Hash, so its error is its own.
type sink struct{}

func (sink) Write(data []byte) (int, error) { return len(data), nil }

func statements(file *os.File, closer io.Closer, callback func() error) {
	file.Close()                  // want `^.\(\*os\.File\)\.Close. returns an error and this call drops it`
	(file.Close())                // want `\(\*os\.File\)\.Close. returns an error`
	save()                        // want `^.save. returns an error`
	(&store{}).Save()             // want `^.\(\*store\)\.Save. returns an error`
	closer.Close()                // want `^.\(io\.Closer\)\.Close. returns an error`
	callback()                    // want `^.callback. returns an error`
	concrete()                    // want `^.concrete. returns an error`
	transient()                   // want `^.transient. returns an error`
	errorFirst()                  // want `^.errorFirst. returns an error`
	first(1)                      // want `^.first. returns an error`
	func() error { return nil }() // want `returns an error`
	(*os.File).Close(file)        // want `\(\*os\.File\)\.Close. returns an error`
}

func deferredAndStarted(file *os.File) {
	defer file.Close()               // want `\(\*os\.File\)\.Close. returns an error`
	go save()                        // want `^.save. returns an error`
	go func() error { return nil }() // want `returns an error`
}

func blanks(file *os.File, text string) int {
	_ = file.Close()               // want `\(\*os\.File\)\.Close. returns an error`
	value, _ := strconv.Atoi(text) // want `^.strconv\.Atoi. returns an error`
	_, count := errorFirst()       // want `^.errorFirst. returns an error`
	_, _ = first("text")           // want `^.first. returns an error`
	var number int
	_, number = save(), 2              // want `^.save. returns an error`
	_, _ = save(), save()              // want `^.save. returns an error` `^.save. returns an error`
	var parsed, _ = strconv.Atoi(text) // want `^.strconv\.Atoi. returns an error`
	var _ = save()                     // want `^.save. returns an error`
	return value + count + number + parsed
}

// A package-level var drops its error the same way.
var packageParsed, _ = strconv.Atoi("1") // want `^.strconv\.Atoi. returns an error`

// Printing to standard output can fail, so Println and Printf are no exemption.
func printing() {
	fmt.Println("printed")        // want `^.fmt\.Println. returns an error`
	fmt.Printf("%s\n", "printed") // want `^.fmt\.Printf. returns an error`
	_, _ = fmt.Println("printed") // want `^.fmt\.Println. returns an error`
}

// A writer that can fail, or one whose static type promises nothing, keeps its error.
func failingWriters(writer *bufio.Writer, reader io.Reader) {
	// A bufio.Writer's Flush is where its kept error comes back, and its ReadFrom writes through to what it wraps.
	writer.Flush()          // want `^.\(\*bufio\.Writer\)\.Flush. returns an error`
	defer writer.Flush()    // want `^.\(\*bufio\.Writer\)\.Flush. returns an error`
	writer.ReadFrom(reader) // want `^.\(\*bufio\.Writer\)\.ReadFrom. returns an error`
	// Standard error is an *os.File, which can fail, and the rule reads the writer's type, not its name.
	fmt.Fprintf(os.Stderr, "%s", "data") // want `^.fmt\.Fprintf. returns an error`
	os.Stderr.WriteString("data")        // want `^.\(\*os\.File\)\.WriteString. returns an error`
	var builder strings.Builder
	var held io.Writer = &builder
	fmt.Fprint(held, "data")   // want `^.fmt\.Fprint. returns an error`
	held.Write([]byte("data")) // want `^.\(io\.Writer\)\.Write. returns an error`
	// A Buffer's ReadFrom and WriteTo read or write another reader or writer, whose errors are real.
	var buffer bytes.Buffer
	buffer.ReadFrom(reader) // want `^.\(\*bytes\.Buffer\)\.ReadFrom. returns an error`
	buffer.WriteTo(writer)  // want `^.\(\*bytes\.Buffer\)\.WriteTo. returns an error`
	// A Write named like a hash's on a type that is no hash.Hash, in a package that sees hash, is judged.
	sink{}.Write([]byte("data")) // want `^.\(sink\)\.Write. returns an error`
	_ = sha256.Size
}

// A stored method value is judged by its static func type, which promises nothing, never by the method the variable
// happens to hold.
func storedMethodValue() {
	var builder strings.Builder
	write := builder.WriteString
	write("x") // want `^.write. returns an error`
}

// Read is this package's own, so crypto/rand's promise is not its promise.
func Read(data []byte) (int, error) { return len(data), nil }

func localRead() {
	Read(nil) // want `^.Read. returns an error`
}

// An embedded Builder written through fmt is judged by the writer's static type, which is not *strings.Builder.
type document struct{ strings.Builder }

func embeddedThroughFmt() {
	var page document
	fmt.Fprintln(&page, "line") // want `^.fmt\.Fprintln. returns an error`
}

func markers(file *os.File) {
	// Ignored:
	_ = file.Close() // want `^This call drops the error .\(\*os\.File\)\.Close. returns and says it is ignored`

	// An empty marker at the end of the line is a block comment, so the want after it is no reason.
	file.Close() /* Ignored: */ // want `says it is ignored without saying why`

	/* Ignored: */
	file.Close() // want `says it is ignored without saying why`

	// Ignored: this reason is a paragraph away from the call below, so it is not that call's.

	file.Close() // want `\(\*os\.File\)\.Close. returns an error`

	// The close was once Ignored: the marker sits mid-sentence here, so it is no excuse.
	file.Close() // want `\(\*os\.File\)\.Close. returns an error`

	// Ignored without a colon gives no reason.
	file.Close() // want `\(\*os\.File\)\.Close. returns an error`

	// Ignored: a reason above one statement is not the next one's.
	save()
	file.Close() // want `\(\*os\.File\)\.Close. returns an error`

	// A reason trailing the code on the line above speaks for that line, not for the statement below it.
	save()       // Ignored: this speaks for save, the line it ends.
	file.Close() // want `\(\*os\.File\)\.Close. returns an error`
}
