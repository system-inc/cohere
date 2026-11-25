// Package parity holds every branch of stdmethods' judgment, each with a clean case beside it, one wrong signature
// for every entry in the table of canonical methods, and the shapes most likely to set the port and x/tools'
// stdmethods apart: generics, aliases, a dot import, a renamed import, a package that shares a standard package's
// name, interface methods in every place an interface type is written, embedded fields and plain functions. The
// differential test runs both over each and compares.
package parity

import (
	"encoding/xml"
	"fmt"
	"io"
)

// canonical declares every method in the table with the canonical signature, and is never reported.
type canonical int

func (canonical) Format(fmt.State, rune)                            {}
func (canonical) GobDecode([]byte) error                            { return nil }
func (canonical) GobEncode() ([]byte, error)                        { return nil, nil }
func (canonical) MarshalJSON() ([]byte, error)                      { return nil, nil }
func (canonical) MarshalXML(*xml.Encoder, xml.StartElement) error   { return nil }
func (canonical) ReadByte() (byte, error)                           { return 0, nil }
func (canonical) ReadFrom(io.Reader) (int64, error)                 { return 0, nil }
func (canonical) ReadRune() (rune, int, error)                      { return 0, 0, nil }
func (canonical) Scan(fmt.ScanState, rune) error                    { return nil }
func (canonical) Seek(int64, int) (int64, error)                    { return 0, nil }
func (canonical) UnmarshalJSON([]byte) error                        { return nil }
func (canonical) UnmarshalXML(*xml.Decoder, xml.StartElement) error { return nil }
func (canonical) UnreadByte() error                                 { return nil }
func (canonical) UnreadRune() error                                 { return nil }
func (canonical) WriteByte(byte) error                              { return nil }
func (canonical) WriteTo(io.Writer) (int64, error)                  { return 0, nil }

// named declares the canonical signatures with parameter and result names, which no comparison reads.
type named int

func (named) Format(state fmt.State, verb rune)                 {}
func (named) ReadRune() (character rune, size int, err error)   { return 0, 0, nil }
func (named) Seek(offset int64, whence int) (at int64, e error) { return 0, nil }

// wrong declares every method in the table with a wrong signature, each reported.
type wrong int

func (wrong) Format(fmt.State, rune) error                   { return nil } // a result where there is none
func (wrong) GobDecode(string) error                         { return nil } // the wrong parameter
func (wrong) GobEncode() []byte                              { return nil } // one result of two
func (wrong) MarshalJSON() (data []byte)                     { return nil } // one named result of two
func (wrong) MarshalXML(xml.Encoder, xml.StartElement) error { return nil } // a value where a pointer belongs
func (wrong) ReadByte() (int, error)                         { return 0, nil }
func (wrong) ReadFrom(io.Reader) int64                       { return 0 }
func (wrong) ReadRune() (rune, error)                        { return 0, nil }
func (wrong) Scan(fmt.ScanState, rune, int) error            { return nil } // one parameter too many
func (wrong) Seek(int64, int64) (int64, error)               { return 0, nil }
func (wrong) UnmarshalJSON(string) error                     { return nil }
func (wrong) UnmarshalXML(*xml.Decoder) error                { return nil } // one parameter too few
func (wrong) UnreadByte() bool                               { return false }
func (wrong) UnreadRune()                                    {}
func (wrong) WriteByte(...byte) error                        { return nil } // variadic, so its type is []byte
func (wrong) WriteTo(io.Writer) error                        { return nil }

// pointer declares wrong signatures on a pointer receiver, each reported as on a value.
type pointer struct{ value int }

func (*pointer) ReadByte() byte          { return 0 }
func (*pointer) WriteTo(io.Writer) int64 { return 0 }

// unsignalled declares the methods whose table entry has a signal type with something else in its place, so none
// means the canonical method and none is reported, whatever the rest of its signature.
type unsignalled int

func (unsignalled) Format(int, rune)                 {}
func (unsignalled) Format2(fmt.State, rune) int      { return 0 } // a name the table does not hold
func (unsignalled) ReadFrom(string) int              { return 0 }
func (unsignalled) Scan(int, rune) error             { return nil }
func (unsignalled) Seek(int, int) (int64, error)     { return 0, nil }
func (unsignalled) WriteTo(io.ReadWriter) (int, int) { return 0, 0 }

// missingSignal declares the signalled methods with no parameter at all where the signal belongs, which is no
// signal either.
type missingSignal int

func (missingSignal) Format()   {}
func (missingSignal) ReadFrom() {}
func (missingSignal) Scan()     {}

// manyWriteTo declares WriteTo with more than one parameter, left alone even when the first is an io.Writer.
type manyWriteTo int

func (manyWriteTo) WriteTo(io.Writer, int) error { return nil }

// unlisted declares methods named in the analyzer's documentation or in a comment of the original's table but not in
// the table itself, so none is judged.
type unlisted int

func (unlisted) Peek() int   { return 0 }
func (unlisted) Flush() int  { return 0 }
func (unlisted) String() int { return 0 }

// Plain functions with the table's names are no methods, and implement nothing.
func Format(fmt.State, byte)  {}
func ReadByte() int           { return 0 }
func Unwrap() int             { return 0 }
func WriteTo(io.Writer) error { return nil }

// embedder embeds wrong, whose methods are promoted and judged once, where they are declared.
type embedder struct {
	wrong
	*pointer
}
