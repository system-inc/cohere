package optionschema

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Schema is one compiled schema: a node of the tree, with the root it resolves references against.
type Schema struct {
	root *node
}

// Validate checks an option list against the schema and returns its first Violation, or nil.
func (schema *Schema) Validate(elements []json.RawMessage) error {
	list := make(JsonArray, len(elements))
	for index, element := range elements {
		// A decoder reads the element's first value and ignores any text after it, as it always has, and its
		// syntax error is the one this has always returned. The value it read is already checked JSON.
		var first json.RawMessage
		decoder := json.NewDecoder(bytes.NewReader(element))
		if decodeError := decoder.Decode(&first); decodeError != nil {
			return fmt.Errorf("element %d is not JSON: %w", index+1, decodeError)
		}
		value, parseError := ParseJsonValue(first)
		if parseError != nil {
			return fmt.Errorf("element %d is not JSON: %w", index+1, parseError)
		}
		list[index] = value
	}
	if violation := validate(schema.root, list, ""); violation != nil {
		return violation
	}
	return nil
}
