// Package parity holds every shape S1030's query and judgment branch on, and the near miss of each, for the parity
// tests to run the original and the port over.
package parity

import (
	"bytes"
	renamed "bytes"
)

type (
	aliasString = string
	aliasBytes  = []byte
	aliasByte   = byte
	// A defined type over string or over a slice of byte is not the universe's type, which the original compares.
	definedString string
	definedBytes  []byte
)

// embedding promotes the buffer's methods from a field held by value, and pointerEmbedding from one held by pointer.
type embedding struct{ bytes.Buffer }

type pointerEmbedding struct{ *bytes.Buffer }

// own declares methods of the buffer's names, which are not the buffer's.
type own struct{}

func (own) Bytes() []byte { return nil }

func (own) String() string { return "" }

// buffered is an interface with the buffer's methods, whose methods are the interface's, not the buffer's.
type buffered interface {
	Bytes() []byte
	String() string
}

func conversions(buffer *bytes.Buffer, value bytes.Buffer, other *renamed.Buffer) {
	_ = string(buffer.Bytes())
	_ = []byte(buffer.String())
	// An addressable buffer, not a pointer, has its pointer methods called alike.
	_ = string(value.Bytes())
	_ = []byte(value.String())
	// The method is judged by its object, so a renamed import names it.
	_ = string(other.Bytes())
	// A receiver that is a call, an index and a selector.
	_ = string(bytes.NewBufferString("text").Bytes())
	_ = []byte(bytes.NewBuffer(nil).String())
	buffers := []*bytes.Buffer{buffer}
	_ = []byte(buffers[0].String())
	holder := struct{ inner *bytes.Buffer }{buffer}
	_ = string(holder.inner.Bytes())
	// Parentheses around the receiver are rendered in the message and dropped from the fix, and parentheses around
	// the callee and around the type are seen through.
	_ = string((buffer).Bytes())
	_ = []byte((buffer).String())
	_ = string((buffer.Bytes)())
	_ = (string)(buffer.Bytes())
	_ = ([]byte)(buffer.String())
	// Parentheses around the argument match the query and fail code.IsCallTo, so neither reports.
	_ = string((buffer.Bytes()))
	_ = []byte((buffer.String()))
	// Aliases are seen through, to the string, the slice and its element.
	_ = aliasString(buffer.Bytes())
	_ = aliasBytes(buffer.String())
	_ = []aliasByte(buffer.String())
	// Near misses: a defined type, uint8 spelled out, runes, and each conversion of the method that already gives it.
	_ = definedString(buffer.Bytes())
	_ = definedBytes(buffer.String())
	_ = []uint8(buffer.String())
	_ = []rune(buffer.String())
	_ = string(buffer.String())
	_ = []byte(buffer.Bytes())
	// Near misses: a method expression has an argument, and a method value is called through no selector.
	_ = string((*bytes.Buffer).Bytes(buffer))
	bytesOf := buffer.Bytes
	_ = string(bytesOf())
	// Near misses: another method of the buffer, and a function, not a conversion, over the buffer's bytes.
	_ = string(buffer.Next(1))
	_ = bytes.ToUpper(buffer.Bytes())
	_ = len(buffer.String())
}

func embedded(value embedding, pointer pointerEmbedding, mine own, interfaced buffered) {
	_ = string(value.Bytes())
	_ = []byte(value.String())
	_ = string(pointer.Bytes())
	_ = []byte(pointer.String())
	_ = string(mine.Bytes())
	_ = []byte(mine.String())
	_ = string(interfaced.Bytes())
	_ = []byte(interfaced.String())
}

func indexes(buffer *bytes.Buffer, counts map[string]int, texts []string) {
	// A conversion to string held directly by an index is left alone, as a key or as what is indexed.
	_ = counts[string(buffer.Bytes())]
	counts[string(buffer.Bytes())]++
	_ = string(buffer.Bytes())[0]
	// Held by anything else, parentheses, a slice expression or a sum, it is reported.
	_ = counts[(string(buffer.Bytes()))]
	_ = string(buffer.Bytes())[1:]
	_ = counts[string(buffer.Bytes())+"suffix"]
	// The conversion to a slice of byte has no such exemption.
	_ = []byte(buffer.String())[0]
	_ = texts[len([]byte(buffer.String()))]
}

// A type parameter's conversion is to the parameter, not to string or a slice of byte.
func generic[Text ~string, Raw ~[]byte](buffer *bytes.Buffer) (Text, Raw) {
	return Text(buffer.Bytes()), Raw(buffer.String())
}

func shadowed(buffer *bytes.Buffer) {
	// A local string is a function here, whose type is no conversion's.
	string := func(contents []byte) []byte { return contents }
	_ = string(buffer.Bytes())
}
