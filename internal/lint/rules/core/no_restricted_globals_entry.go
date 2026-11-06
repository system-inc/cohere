package core

import (
	"encoding/json"
	"fmt"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// noRestrictedGlobalsEntry is one item of the globals list in either of its two spellings.
type noRestrictedGlobalsEntry struct {
	Name    string
	Message string
}

// UnmarshalJSON accepts both a bare string and an object, which is upstream's `oneOf`.
func (entry *noRestrictedGlobalsEntry) UnmarshalJSON(raw []byte) error {
	var name string
	if decodeError := json.Unmarshal(raw, &name); decodeError == nil {
		entry.Name = name
		entry.Message = ""
		return nil
	}
	var object struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	// Strict, which is upstream's `additionalProperties: false`: a misspelled `message` key would
	// otherwise be accepted and the custom message silently never shown.
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &object); unmarshalOptionsError != nil {
		return unmarshalOptionsError
	}
	if object.Name == "" {
		return fmt.Errorf("no-restricted-globals entry needs a name")
	}
	entry.Name = object.Name
	entry.Message = object.Message
	return nil
}
