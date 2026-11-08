package parity

import (
	"fmt"
	"io"
)

// Aliases are written by their own names, so an alias of a canonical type does not match it.
type (
	byteAlias   = byte
	errorAlias  = error
	stateAlias  = fmt.State
	writerAlias = io.Writer
	anyAlias    = any
)

type aliased int

func (aliased) ReadByte() (byteAlias, error)       { return 0, nil } // reported: byteAlias is not byte
func (aliased) GobDecode([]byte) errorAlias        { return nil }    // reported
func (aliased) Format(stateAlias, rune)            {}                // no signal: stateAlias is not fmt.State
func (aliased) WriteTo(writerAlias) (int64, error) { return 0, nil } // no signal, though canonical in fact
func (aliased) WriteByte(uint8) error              { return nil }    // reported: uint8 is not byte by name
func (aliased) ReadRune() (int32, int, error)      { return 0, 0, nil }
func (aliased) UnreadByte() error                  { return nil }

type aliasedError int

func (aliasedError) Error() string      { return "" }
func (aliasedError) Unwrap() errorAlias { return nil } // reported: errorAlias is not error by name
func (aliasedError) As(anyAlias) bool   { return false }

// A defined type, not an alias, over a canonical one.
type definedByte byte

func (definedByte) ReadByte() (definedByte, error) { return 0, nil }

// An alias of an error type is an error type, and its methods are declared on the type it names.
type aliasOfError = aliasedError
