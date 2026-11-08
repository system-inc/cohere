// Package parity holds the shapes the pattern `(CallExpr (Symbol "io.WriteString") [_ (CallExpr (Builtin "string")
// [arg])])` accepts and refuses, and the slices that do and do not convert to a string directly, in a module older
// than Go 1.18, where a slice of a type defined on byte does not. Each call's comment says what the original makes of
// it; the parity test holds only that the port makes the same.
package parity

import (
	"fmt"
	"io"
	stdio "io"
)

type buffer []byte

type text string

func use(values ...interface{}) {}

func produce() []byte { return nil }

func calls(writer io.Writer, bytes []byte, named buffer, unsigned []uint8, runes []rune, plain string) {
	io.WriteString(writer, string(bytes))                 // Found.
	io.WriteString(writer, string(named))                 // Found: a type defined on []byte.
	io.WriteString(writer, string(unsigned))              // Found: uint8 is byte.
	io.WriteString(writer, string(produce()))             // Found: a call's result.
	io.WriteString(writer, string(bytes[1:]))             // Found: a slicing.
	stdio.WriteString(writer, string(bytes))              // Found: the package by another name.
	(io.WriteString)(writer, string(bytes))               // Found: the function in parentheses.
	io.WriteString(writer, (string(bytes)))               // Found: the conversion in parentheses.
	io.WriteString(writer, (string)(bytes))               // Found: string in parentheses.
	io.WriteString(writer, string((bytes)))               // Found: the slice in parentheses.
	io.WriteString(writer, string(runes))                 // Not found: runes are no bytes.
	io.WriteString(writer, string(text(plain)))           // Not found: a string converted.
	io.WriteString(writer, plain)                         // Not found: no conversion.
	io.WriteString(writer, string(rune(65)))              // Not found: a rune converted.
	fmt.Fprint(writer, string(bytes))                     // Not found: another function.
	use(io.WriteString)                                   // Not found: no call.
	_, _ = writer.Write(bytes)                            // Not found.
	_, _ = io.WriteString(writer, string(bytes))          // Found: the results kept.
	defer io.WriteString(writer, string(bytes))           // Found: deferred.
	go io.WriteString(writer, string(bytes))              // Found: in a goroutine.
	use(func() { io.WriteString(writer, string(named)) }) // Found: in a closure.
}

// Not found: string is a function here.
func shadowed(writer io.Writer, bytes []byte) {
	string := func(bytes []byte) string { return "" }
	io.WriteString(writer, string(bytes))
}

// Not found: a writer of the package's own named WriteString.
func ownWriteString(bytes []byte) {
	WriteString(nil, string(bytes))
}

// WriteString is the package's own, no io.WriteString.
func WriteString(writer io.Writer, text string) (int, error) { return 0, nil }
