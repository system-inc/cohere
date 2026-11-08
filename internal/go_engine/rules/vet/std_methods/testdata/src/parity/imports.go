package parity

import (
	. "fmt"
	format "fmt"
	readwrite "io"

	lookalike "parity/xml"
)

// A dot import, a renamed import and a lookalike package all write their types by their package's name.
type imported int

func (imported) Format(State, rune)                                             {} // canonical by its package's name
func (imported) Scan(format.ScanState, rune) error                              { return nil }
func (imported) ReadFrom(readwrite.Reader) error                                { return nil } // reported
func (imported) MarshalXML(*lookalike.Encoder, lookalike.StartElement) error    { return nil } // canonical by name
func (imported) UnmarshalXML(*lookalike.Decoder, *lookalike.StartElement) error { return nil } // reported

var _ = Sprint
