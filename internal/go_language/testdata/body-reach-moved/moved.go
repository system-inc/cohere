// Package moved holds three readers of a value's whole type, printf's verb match, `json.Marshal`'s walk and
// `binary.Write`'s, each handed a value as an empty interface whose own type no build changes and which reaches, below
// a named slice, a type each build's own file declares, for the body reach census (#h0k644s): printedValue,
// marshalledValue and writtenValue are each one its reader takes on the host and refuses on windows.
package moved

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// PrintedEntry holds printedValue by value, so its underlying bit is set on windows and its name bit is not.
type PrintedEntry struct{ Value printedValue }

// PrintedEntries holds PrintedEntry behind a slice, which reads its name alone, so neither of its own bits is set.
type PrintedEntries []PrintedEntry

// PrintedNested holds a PrintedEntries by value, and neither of its bits is set.
type PrintedNested struct{ Entries PrintedEntries }

// MarshalledEntry holds marshalledValue by value.
type MarshalledEntry struct{ Value marshalledValue }

// MarshalledEntries holds MarshalledEntry behind a slice.
type MarshalledEntries []MarshalledEntry

// MarshalledNested holds a MarshalledEntries by value.
type MarshalledNested struct{ Entries MarshalledEntries }

// WrittenEntry holds writtenValue by value.
type WrittenEntry struct{ Value writtenValue }

// WrittenEntries holds WrittenEntry behind a slice.
type WrittenEntries []WrittenEntry

// WrittenNested holds a WrittenEntries by value.
type WrittenNested struct{ Entries WrittenEntries }

// PlainEntry holds nothing any build changes.
type PlainEntry struct{ Value int }

// PlainEntries holds PlainEntry behind a slice.
type PlainEntries []PlainEntry

// Show prints a named slice, which printf looks through to printedValue.
func Show(entries PrintedEntries) {
	fmt.Printf("%d\n", entries)
}

// ShowNested prints a struct holding the named slice.
func ShowNested(nested PrintedNested) {
	fmt.Printf("%d\n", nested)
}

// ShowPlain prints a named slice a look through reaches nothing any build changes below.
func ShowPlain(entries PlainEntries) {
	fmt.Printf("%d\n", entries)
}

// Marshal marshals a named slice, which the marshaller walks to marshalledValue.
func Marshal(entries MarshalledEntries) ([]byte, error) {
	return json.Marshal(entries)
}

// MarshalNested marshals a struct holding the named slice.
func MarshalNested(nested MarshalledNested) ([]byte, error) {
	return json.Marshal(nested)
}

// Write writes a named slice, which the writer walks to writtenValue.
func Write(writer io.Writer, entries WrittenEntries) error {
	return binary.Write(writer, binary.LittleEndian, entries)
}

// WriteNested writes a struct holding the named slice.
func WriteNested(writer io.Writer, nested WrittenNested) error {
	return binary.Write(writer, binary.LittleEndian, nested)
}
