// Package differences holds the shapes where the original and the port could most plausibly part, and the one way
// they do, where the original is wrong: a holder of a buffer whose other method, the one the message asks for, is
// not the buffer's, which the original flags with a fix that calls another method or does not compile, and the port
// drops.
package differences

import (
	"bytes"
	"fmt"
)

// overriding embeds a buffer and declares a String of its own, which its Bytes, the buffer's, does not agree with.
type overriding struct{ bytes.Buffer }

func (overriding) String() string { return "overriding" }

// rawOverriding embeds a buffer and declares a Bytes of its own.
type rawOverriding struct{ *bytes.Buffer }

func (rawOverriding) Bytes() []byte { return nil }

// ambiguous embeds a buffer and a fmt.Stringer at the same depth, so its String is ambiguous and selects nothing.
type ambiguous struct {
	*bytes.Buffer
	fmt.Stringer
}

// nested holds a String two levels down, under the buffer's one level down, which wins.
type nested struct{ overriding }

type deep struct {
	*bytes.Buffer
	nested
}

// outer embeds the overriding type, whose String is still the shallowest.
type outer struct{ overriding }

func drops(overridden overriding, raw rawOverriding, either ambiguous, wrapped outer, pointer *overriding) {
	// overriding declares its own String, which the fix would call in place of the buffer's.
	_ = string(overridden.Bytes()) // port drops: should use overridden.String() instead of string(overridden.Bytes())
	// rawOverriding declares its own Bytes, which the fix would call in place of the buffer's.
	_ = []byte(raw.String()) // port drops: should use raw.Bytes() instead of []byte(raw.String())
	// ambiguous's String selects nothing, so the fix would not compile.
	_ = string(either.Bytes()) // port drops: should use either.String() instead of string(either.Bytes())
	// outer's String is overriding's, one level above the buffer's.
	_ = string(wrapped.Bytes()) // port drops: should use wrapped.String() instead of string(wrapped.Bytes())
	// Through a pointer, overriding's String is still its own.
	_ = string(pointer.Bytes()) // port drops: should use pointer.String() instead of string(pointer.Bytes())
	// The called method is not the buffer's, so neither reports.
	_ = []byte(overridden.String())
	_ = string(raw.Bytes())
}

func agreements(buffer *bytes.Buffer, buffers []*bytes.Buffer, deeper deep, either ambiguous) {
	// The buffer's String is one level down and the overriding one two, so the buffer's is the one selected.
	_ = string(deeper.Bytes())
	_ = []byte(deeper.String())
	// Through the ambiguous holder's buffer by name, the String asked for is the buffer's, so both report.
	_ = string(either.Buffer.Bytes())
	// A receiver across lines, and comments among the conversion, render alike and print alike in the fix.
	_ = string(pick(
		buffers,
		0,
	).Bytes())
	_ = []byte(pick( /* the buffers */ buffers, 0).String())
	_ = []byte( /* the contents */ buffer.String())
	_ = string(buffer. /* the method */ Bytes())
	// Conversions nested in conversions, calls and function literals.
	_ = []byte(string(buffer.Bytes()))
	_ = string([]byte(buffer.String()))
	_ = fmt.Sprint(string(buffer.Bytes()), []byte(buffer.String()))
	_ = func() string { return string(buffer.Bytes()) }()
}

func pick(buffers []*bytes.Buffer, index int) *bytes.Buffer { return buffers[index] }
