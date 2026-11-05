package optionschema

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// JsonKind is which of JSON's six shapes a JsonValue holds.
type JsonKind string

// The kinds a JsonValue can hold, one per JSON shape.
const (
	JsonKindNull    JsonKind = "Null"
	JsonKindBoolean JsonKind = "Boolean"
	JsonKindNumber  JsonKind = "Number"
	JsonKindString  JsonKind = "String"
	JsonKindArray   JsonKind = "Array"
	JsonKindObject  JsonKind = "Object"
)

// JsonValue is one decoded JSON value: a schema, a schema's keyword, or an option element being checked. A
// schema can hold any JSON shape, so the decode keeps the shape it found in Kind and the value in the one
// field that kind reads, and every reader switches on Kind instead of asserting on an untyped value.
type JsonValue struct {
	Kind     JsonKind
	Boolean  bool                 // Kind JsonKindBoolean
	Number   json.Number          // Kind JsonKindNumber, kept as its text so nothing is lost to float64
	Text     string               // Kind JsonKindString
	Elements []JsonValue          // Kind JsonKindArray
	Members  map[string]JsonValue // Kind JsonKindObject
}

// jsonWhitespace is the four bytes JSON allows around a value, and only those.
const jsonWhitespace = " \t\r\n"

// UnmarshalJSON decides the kind from the value's first byte and decodes the rest into that kind's field.
// An array's elements and an object's members decode through this same method, so the whole tree is typed.
func (value *JsonValue) UnmarshalJSON(data []byte) error {
	trimmed := bytes.Trim(data, jsonWhitespace)
	if len(trimmed) == 0 {
		return fmt.Errorf("reading a JSON value: there is none")
	}
	switch trimmed[0] {
	case 'n':
		// Decoding null into any Go value succeeds and leaves it alone, so the literal is checked by its text.
		if string(trimmed) != "null" {
			return fmt.Errorf("reading a JSON value: %q is not null", trimmed)
		}
		*value = JsonValue{Kind: JsonKindNull}
	case 't', 'f':
		var boolean bool
		if decodeError := json.Unmarshal(trimmed, &boolean); decodeError != nil {
			return fmt.Errorf("reading a JSON boolean: %w", decodeError)
		}
		*value = JsonValue{Kind: JsonKindBoolean, Boolean: boolean}
	case '"':
		var text string
		if decodeError := json.Unmarshal(trimmed, &text); decodeError != nil {
			return fmt.Errorf("reading a JSON string: %w", decodeError)
		}
		*value = JsonValue{Kind: JsonKindString, Text: text}
	case '[':
		// Never nil, so an empty array reads and encodes as one rather than as a missing list.
		elements := []JsonValue{}
		if decodeError := json.Unmarshal(trimmed, &elements); decodeError != nil {
			return fmt.Errorf("reading a JSON array: %w", decodeError)
		}
		*value = JsonValue{Kind: JsonKindArray, Elements: elements}
	case '{':
		members := map[string]JsonValue{}
		if decodeError := json.Unmarshal(trimmed, &members); decodeError != nil {
			return fmt.Errorf("reading a JSON object: %w", decodeError)
		}
		*value = JsonValue{Kind: JsonKindObject, Members: members}
	default:
		// json.Number takes a number literal's own text after checking it is one, so `1.0` and `1e3` keep
		// their spelling, as the untyped decode kept them under UseNumber.
		var number json.Number
		if decodeError := json.Unmarshal(trimmed, &number); decodeError != nil {
			return fmt.Errorf("reading a JSON number: %w", decodeError)
		}
		*value = JsonValue{Kind: JsonKindNumber, Number: number}
	}
	return nil
}

// MarshalJSON writes the value back as the JSON it was decoded from, with an object's keys sorted and a
// number in its own spelling, which is what encoding/json wrote for the untyped value. It takes the value
// rather than a pointer so an element of a list or a member of a map, which is not addressable, still has it.
func (value JsonValue) MarshalJSON() ([]byte, error) {
	switch value.Kind {
	case JsonKindNull:
		return []byte("null"), nil
	case JsonKindBoolean:
		return json.Marshal(value.Boolean)
	case JsonKindNumber:
		return json.Marshal(value.Number)
	case JsonKindString:
		return json.Marshal(value.Text)
	case JsonKindArray:
		// A nil list is still an array, never encoding/json's null for a nil slice.
		if value.Elements == nil {
			return []byte("[]"), nil
		}
		return json.Marshal(value.Elements)
	case JsonKindObject:
		if value.Members == nil {
			return []byte("{}"), nil
		}
		return json.Marshal(value.Members)
	}
	return nil, fmt.Errorf("writing a JSON value: %q is not a kind of JSON value", value.Kind)
}

// Equal is ajv's deep equality (fast-deep-equal): numbers by value, objects by key set and values.
func (value JsonValue) Equal(other JsonValue) bool {
	if value.Kind != other.Kind {
		return false
	}
	switch value.Kind {
	case JsonKindNull:
		return true
	case JsonKindBoolean:
		return value.Boolean == other.Boolean
	case JsonKindString:
		return value.Text == other.Text
	case JsonKindNumber:
		number, numberError := value.Number.Float64()
		otherNumber, otherNumberError := other.Number.Float64()
		return numberError == nil && otherNumberError == nil && number == otherNumber
	case JsonKindArray:
		if len(value.Elements) != len(other.Elements) {
			return false
		}
		for index := range value.Elements {
			if !value.Elements[index].Equal(other.Elements[index]) {
				return false
			}
		}
		return true
	case JsonKindObject:
		if len(value.Members) != len(other.Members) {
			return false
		}
		for key, member := range value.Members {
			otherMember, present := other.Members[key]
			if !present || !member.Equal(otherMember) {
				return false
			}
		}
		return true
	}
	return false
}
