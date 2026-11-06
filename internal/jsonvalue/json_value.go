package jsonvalue

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// JsonValue is one decoded JSON value: a schema, a schema's keyword, or an option element being checked. A
// schema can hold any JSON shape, so the value is one of six types, one per shape, and a reader takes it
// apart with a type switch over them. The unexported marker closes the set to the six this package declares, so a
// value can never hold two shapes at once or one this package does not know.
type JsonValue interface {
	jsonValue()
}

// JsonObject is a JSON object, its members by key.
type JsonObject map[string]JsonValue

// JsonArray is a JSON array, its elements in order.
type JsonArray []JsonValue

// JsonString is a JSON string, unquoted.
type JsonString string

// JsonBoolean is JSON's true or false.
type JsonBoolean bool

func (JsonObject) jsonValue()  {}
func (JsonArray) jsonValue()   {}
func (JsonString) jsonValue()  {}
func (JsonBoolean) jsonValue() {}

// jsonWhitespace is the four bytes JSON allows around a value, and only those.
const jsonWhitespace = " \t\r\n"

// ParseJsonValue decodes one JSON value, refusing anything json.Unmarshal refuses. It decides the shape
// from the value's first byte, and an object's members and an array's elements are read as raw JSON and
// parsed by this same function, so the whole tree is typed and nothing passes through an untyped value.
func ParseJsonValue(raw []byte) (JsonValue, error) {
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
		return JsonNull{}, nil
	case 't', 'f':
		var boolean bool
		if decodeError := json.Unmarshal(trimmed, &boolean); decodeError != nil {
			return nil, fmt.Errorf("reading a JSON boolean: %w", decodeError)
		}
		return JsonBoolean(boolean), nil
	case '"':
		var text string
		if decodeError := json.Unmarshal(trimmed, &text); decodeError != nil {
			return nil, fmt.Errorf("reading a JSON string: %w", decodeError)
		}
		return JsonString(text), nil
	case '[':
		var elements []json.RawMessage
		if decodeError := json.Unmarshal(trimmed, &elements); decodeError != nil {
			return nil, fmt.Errorf("reading a JSON array: %w", decodeError)
		}
		parsed := make(JsonArray, len(elements))
		for index, element := range elements {
			value, parseError := ParseJsonValue(element)
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
		parsed := make(JsonObject, len(members))
		for key, member := range members {
			value, parseError := ParseJsonValue(member)
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
	return JsonNumber(number), nil
}
