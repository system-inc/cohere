package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NoWarningCommentsLocation says where in a comment a term has to appear to count.
type NoWarningCommentsLocation string

// UnmarshalJSON reads upstream's spelling, `"start"` or `"anywhere"`, and refuses anything else.
//
// The config used to want `"Start"` and `"Anywhere"`, which refused upstream's own spelling while
// no ESLint version accepted ours; the PascalCase form is now refused instead (#d21war2). An
// unrecognized value is an error rather than a fallback, because every arm is selected by string
// equality and an unknown value would silently pick a third behaviour of matching nothing.
func (location *NoWarningCommentsLocation) UnmarshalJSON(raw []byte) error {
	var spelling string
	if decodeError := json.Unmarshal(raw, &spelling); decodeError != nil {
		return decodeError
	}
	switch spelling {
	case "start":
		*location = NoWarningCommentsStart
	case "anywhere":
		*location = NoWarningCommentsAnywhere
	default:
		return fmt.Errorf(`location is %q, which is neither "start" nor "anywhere"`, spelling)
	}
	return nil
}

// MarshalJSON writes the spelling UnmarshalJSON reads, so an options value round-trips.
func (location NoWarningCommentsLocation) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.ToLower(string(location)))
}
