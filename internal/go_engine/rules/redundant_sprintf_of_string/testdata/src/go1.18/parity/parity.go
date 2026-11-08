// Package parity holds every shape S1025's query and judgments branch on, and the near miss of each, where the
// original and the port must agree finding for finding, fixes and all.
package parity

import (
	"fmt"
	"reflect"
	"strings"
)

// Text is defined on string, and Again on Text, so each converts to a string and has no String method.
type Text string

type Again Text

// Number has a String method on its value, so a Number and a pointer to one are each a fmt.Stringer.
type Number int

func (Number) String() string { return "" }

// PointerNumber has its String method on its pointer alone, so a PointerNumber is no fmt.Stringer.
type PointerNumber int

func (*PointerNumber) String() string { return "" }

// Bytes is defined on []byte, and Bytes2 is a slice of Byte, a type defined on byte, which converts under go1.18.
type Bytes []byte

type Byte byte

type Bytes2 []Byte

// Formatted has a Format method of fmt's shape on its value, and PointerFormatted on its pointer alone.
type Formatted string

func (Formatted) Format(fmt.State, rune) {}

type PointerFormatted string

func (*PointerFormatted) Format(fmt.State, rune) {}

// OneParameter and Returning each have a Format method of another shape, so neither is a formatter.
type OneParameter string

func (OneParameter) Format(fmt.State) {}

type Returning string

func (Returning) Format(fmt.State, rune) error { return nil }

// FormattedStringer is a formatter and a fmt.Stringer, and the formatter wins.
type FormattedStringer int

func (FormattedStringer) Format(fmt.State, rune) {}

func (FormattedStringer) String() string { return "" }

// Embedded promotes Number's String, and Wrapper promotes reflect.Value's.
type Embedded struct{ Number }

type Wrapper struct{ reflect.Value }

// ValueAlias is reflect.Value under another name, and ValueDefined a type of its own on reflect.Value's structure.
type ValueAlias = reflect.Value

type ValueDefined reflect.Value

// Named has a String method and a field of type string, Holder the field alone.
type Named struct{ Name string }

func (Named) String() string { return "" }

type Holder struct{ Name string }

// Generic has a String method on every instantiation, and GenericBytes converts on every one.
type Generic[T any] string

func (Generic[T]) String() string { return "" }

type GenericBytes[T any] []byte

// TextAlias is string, and TextAliasDefined is Text, under other names.
type TextAlias = string

type TextAliasDefined = Text

// FormatAndString is an interface of both methods, which the formatter wins.
type FormatAndString interface {
	fmt.Formatter
	fmt.Stringer
}

// percent is the format as a named constant, and verbose another verb.
const percent = "%s"

const verbose = "%v"

func formats(text string, values []any) {
	_ = fmt.Sprintf("%s", "literal")
	_ = fmt.Sprintf("%s", text)
	_ = fmt.Sprintf(percent, text)
	_ = fmt.Sprintf("%"+"s", text)
	_ = fmt.Sprintf(("%s"), (text))
	_ = (fmt.Sprintf)("%s", text)
	_ = fmt.Sprintf(`%s`, text)
	_ = fmt.Sprintf("%s" /* the value */, text)
	_ = fmt.Sprintf("%s", strings.Join(
		values[0].([]string),
		",",
	))
	_ = fmt.Sprintf("%s", text+
		text)

	// Near misses: another verb, another format, a format that is no constant, a count of arguments other than two,
	// a spread of values that is no string, another function of fmt, and fmt.Sprintf held in a variable.
	_ = fmt.Sprintf(verbose, text)
	_ = fmt.Sprintf("%s ", text)
	_ = fmt.Sprintf(text, text)
	_ = fmt.Sprintf("%s", text, text)
	_ = fmt.Sprintf("%s")
	_ = fmt.Sprintf("%s", values...)
	_ = fmt.Sprint(text)
	sprintf := fmt.Sprintf
	_ = sprintf("%s", text)
	_ = fmt.Sprintf("%s", nil)
}

func kinds(
	text Text, again Again, alias TextAlias, aliasDefined TextAliasDefined, number Number, pointer *Number,
	pointerNumber PointerNumber, bytes Bytes, bytes2 Bytes2, formatted Formatted, pointerFormatted PointerFormatted,
	oneParameter OneParameter, returning Returning, formattedStringer FormattedStringer, embedded Embedded,
	wrapper Wrapper, value reflect.Value, valueAlias ValueAlias, valueDefined ValueDefined, stringer fmt.Stringer,
	formatter fmt.Formatter, both FormatAndString, empty any, failure error, generic Generic[int],
	genericBytes GenericBytes[string], integer int, runes []rune,
) {
	_ = fmt.Sprintf("%s", text)
	_ = fmt.Sprintf("%s", again)
	_ = fmt.Sprintf("%s", alias)
	_ = fmt.Sprintf("%s", aliasDefined)
	_ = fmt.Sprintf("%s", number)
	_ = fmt.Sprintf("%s", &number)
	_ = fmt.Sprintf("%s", pointer)
	_ = fmt.Sprintf("%s", *pointer)
	_ = fmt.Sprintf("%s", Number(1))
	_ = fmt.Sprintf("%s", pointerNumber)
	_ = fmt.Sprintf("%s", &pointerNumber)
	_ = fmt.Sprintf("%s", bytes)
	_ = fmt.Sprintf("%s", bytes2)
	_ = fmt.Sprintf("%s", []byte("literal"))
	_ = fmt.Sprintf("%s", []byte{})
	_ = fmt.Sprintf("%s", []Byte{})
	_ = fmt.Sprintf("%s", formatted)
	_ = fmt.Sprintf("%s", pointerFormatted)
	_ = fmt.Sprintf("%s", &pointerFormatted)
	_ = fmt.Sprintf("%s", oneParameter)
	_ = fmt.Sprintf("%s", returning)
	_ = fmt.Sprintf("%s", formattedStringer)
	_ = fmt.Sprintf("%s", embedded)
	_ = fmt.Sprintf("%s", &embedded)
	_ = fmt.Sprintf("%s", wrapper)
	_ = fmt.Sprintf("%s", value)
	_ = fmt.Sprintf("%s", &value)
	_ = fmt.Sprintf("%s", valueAlias)
	_ = fmt.Sprintf("%s", valueDefined)
	_ = fmt.Sprintf("%s", stringer)
	_ = fmt.Sprintf("%s", formatter)
	_ = fmt.Sprintf("%s", both)
	_ = fmt.Sprintf("%s", empty)
	_ = fmt.Sprintf("%s", failure)
	_ = fmt.Sprintf("%s", generic)
	_ = fmt.Sprintf("%s", genericBytes)
	_ = fmt.Sprintf("%s", Generic[bool]("literal"))
	_ = fmt.Sprintf("%s", integer)
	_ = fmt.Sprintf("%s", runes)
	_ = fmt.Sprintf("%s", Named{})
	_ = fmt.Sprintf("%s", Holder{}.Name)
	_ = fmt.Sprintf("%s", Named{Name: "literal"}.Name)
}

// A type parameter is skipped whatever its constraint, as is a slice of one, which converts to no string.
func parameters[S fmt.Stringer, T ~string, B ~[]byte, A any](stringer S, text T, bytes B, values []A, raw []T) {
	_ = fmt.Sprintf("%s", stringer)
	_ = fmt.Sprintf("%s", text)
	_ = fmt.Sprintf("%s", bytes)
	_ = fmt.Sprintf("%s", values)
	_ = fmt.Sprintf("%s", raw)
	_ = fmt.Sprintf("%s", string(text))
	_ = fmt.Sprintf("%s", stringer.String())
}

// Where the call stands, each fix is the original's: indexed through parentheses or of a value an index binds
// tighter than, beside an operator, in a control clause inside parentheses, a call, braces or a case, as a statement
// of the String method, and before a block declares a string of its own.
func places(text string, other Text, number Number, channel chan string, texts []string) {
	_ = fmt.Sprintf("%s", text)[0]
	_ = fmt.Sprintf("%s", text)[1:]
	_ = (fmt.Sprintf("%s", text+text))[0]
	_ = fmt.Sprintf("%s", texts[0])[0]
	_ = fmt.Sprintf("%s", other)[0]
	_ = fmt.Sprintf("%s", number)[0]
	_ = fmt.Sprintf("%s", text+text) + text
	_ = text + fmt.Sprintf("%s", text+text)
	_ = fmt.Sprintf("%s", <-channel) == text
	if fmt.Sprintf("%s", text) == "" {
	}
	if (fmt.Sprintf("%s", Named{})) == "" {
	}
	if strings.HasPrefix(fmt.Sprintf("%s", Holder{}.Name), "") {
	}
	if texts := []string{fmt.Sprintf("%s", Holder{}.Name)}; len(texts) == 0 {
	}
	if fmt.Sprintf("%s", []byte{}) == "" {
	}
	if fmt.Sprintf("%s", Text(Holder{}.Name)) == "" {
	}
	switch {
	case fmt.Sprintf("%s", Named{}) == "":
	}
	if func() bool { return fmt.Sprintf("%s", Named{}) == "" }() {
	}
	fmt.Sprintf("%s", number)
	defer fmt.Sprintf("%s", number)
	go fmt.Sprintf("%s", number)
	_ = fmt.Sprintf("%s", other)
	string := 0
	_ = string
}
