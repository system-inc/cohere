// Package library holds the types parity converts to and from by a qualified name, a selector, from a package whose
// path is not a standard one, so an import of fmt placed before it is followed by a blank line.
package library

// Text is a string type declared in another package.
type Text string

// Label is an alias of string declared in another package.
type Label = string

// Count is an integer type declared in another package.
type Count int

// Number is an alias of an integer type declared in another package.
type Number = int64

// Value is an integer variable of another package.
var Value = 7

// Converted converts within the library itself, so a root other than parity is held to the original too.
func Converted(count Count) Text {
	return Text(count)
}
