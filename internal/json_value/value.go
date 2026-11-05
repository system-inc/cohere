// Package json_value is a decoded JSON value as a closed set of Go types, one per JSON shape, so code that reads
// JSON it did not define (rule options, schemas, an oracle's output) switches over types the compiler knows instead
// of over whatever encoding/json put into an any. It lives on its own so the lint layer and the formatter's oracle
// tests share it without either importing the other.
package json_value

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Value is one decoded JSON value. JSON can hold any of six shapes, so the value is one of six types, one per
// shape, and a reader takes it apart with a type switch over them. The unexported marker closes the set to the six declared here, so a
// value can never hold two shapes at once or one this package does not know.
type Value interface {
	jsonValue()
}

// Object is a JSON object, its members by key.
type Object map[string]Value

// Array is a JSON array, its elements in order.
type Array []Value

// Number is a JSON number kept as its own text, as json.Number keeps it, so `1.0` and `1e3` keep their
// spelling and nothing is lost to float64 before a reader asks for one.
type Number json.Number

// String is a JSON string, unquoted.
type String string

// Boolean is JSON's true or false.
type Boolean bool

// Null is JSON's null.
type Null struct{}

func (Object) jsonValue()  {}
func (Array) jsonValue()   {}
func (Number) jsonValue()  {}
func (String) jsonValue()  {}
func (Boolean) jsonValue() {}
func (Null) jsonValue()    {}

// jsonWhitespace is the four bytes JSON allows around a value, and only those.
const jsonWhitespace = " \t\r\n"

// Parse decodes one JSON value, refusing anything json.Unmarshal refuses. It decides the shape
// from the value's first byte, and an object's members and an array's elements are read as raw JSON and
// parsed by this same function, so the whole tree is typed and nothing passes through an untyped value.
func Parse(raw []byte) (Value, error) {
	trimmed := bytes.Trim(raw, jsonWhitespace)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("reading a JSON value: there is none")
	}
	switch trimmed[0] {
	case 'n':
		// Decoding null into any Go value succeeds and leaves it alone, so the literal is checked by its text.
		if string(trimmed) != "null" {
			return nil, fmt.Errorf("reading a JSON value: %q is not null", trimmed)
		}
		return Null{}, nil
	case 't', 'f':
		var boolean bool
		if decodeError := json.Unmarshal(trimmed, &boolean); decodeError != nil {
			return nil, fmt.Errorf("reading a JSON boolean: %w", decodeError)
		}
		return Boolean(boolean), nil
	case '"':
		var text string
		if decodeError := json.Unmarshal(trimmed, &text); decodeError != nil {
			return nil, fmt.Errorf("reading a JSON string: %w", decodeError)
		}
		return String(text), nil
	case '[':
		var elements []json.RawMessage
		if decodeError := json.Unmarshal(trimmed, &elements); decodeError != nil {
			return nil, fmt.Errorf("reading a JSON array: %w", decodeError)
		}
		parsed := make(Array, len(elements))
		for index, element := range elements {
			value, parseError := Parse(element)
			if parseError != nil {
				return nil, parseError
			}
			parsed[index] = value
		}
		return parsed, nil
	case '{':
		var members map[string]json.RawMessage
		if decodeError := json.Unmarshal(trimmed, &members); decodeError != nil {
			return nil, fmt.Errorf("reading a JSON object: %w", decodeError)
		}
		parsed := make(Object, len(members))
		for key, member := range members {
			value, parseError := Parse(member)
			if parseError != nil {
				return nil, parseError
			}
			parsed[key] = value
		}
		return parsed, nil
	}
	// json.Number takes a number literal's own text after checking it is one, so `1.0` and `1e3` keep their
	// spelling, as the untyped decode kept them under UseNumber.
	var number json.Number
	if decodeError := json.Unmarshal(trimmed, &number); decodeError != nil {
		return nil, fmt.Errorf("reading a JSON number: %w", decodeError)
	}
	return Number(number), nil
}
