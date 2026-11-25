package registry

import (
	"encoding/json"
	"fmt"
)

// eslintFinding is one of ESLint's findings on a row, vendored as the array `[messageId, start, end]`, its
// offsets in UTF-16 code units. The verdict compares places, not message ids, so only the offsets are kept.
type eslintFinding struct {
	start int
	end   int
}

// UnmarshalJSON reads the vendored three-element array, refusing any other shape so a re-extract gone wrong
// fails here rather than reading as a finding at offset zero.
func (finding *eslintFinding) UnmarshalJSON(data []byte) error {
	var elements [3]json.RawMessage
	if decodeError := json.Unmarshal(data, &elements); decodeError != nil {
		return fmt.Errorf("reading ESLint's finding %s as [messageId, start, end]: %w", data, decodeError)
	}
	if decodeError := json.Unmarshal(elements[1], &finding.start); decodeError != nil {
		return fmt.Errorf("reading the start offset of ESLint's finding %s: %w", data, decodeError)
	}
	if decodeError := json.Unmarshal(elements[2], &finding.end); decodeError != nil {
		return fmt.Errorf("reading the end offset of ESLint's finding %s: %w", data, decodeError)
	}
	return nil
}
