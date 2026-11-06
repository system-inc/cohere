package core

import (
	"encoding/json"
	"fmt"
)

// PreferConstDestructuring selects how a destructuring declaration is judged.
type PreferConstDestructuring int

// UnmarshalJSON decodes the option's two spellings from a configuration file.
//
// The option arrives as the JSON string "any" or "all" and the decoder is rule.UnmarshalOptions
// into this struct, so without this the string lands on an int field and the whole option object
// fails to decode. That failure is quiet in the direction that matters: the rule then runs on its
// zero value, which is "any", and a project that asked for "all" silently gets the other answer.
//
// An unrecognized value is an error rather than a fallback to the default, because ESLint's schema
// is an enum with additionalProperties false: a typo is a configuration mistake and reporting it is
// more useful than guessing which of the two the author meant.
func (destructuring *PreferConstDestructuring) UnmarshalJSON(raw []byte) error {
	var spelling string
	if decodeError := json.Unmarshal(raw, &spelling); decodeError != nil {
		return decodeError
	}
	switch spelling {
	case "any":
		*destructuring = PreferConstDestructuringAny
	case "all":
		*destructuring = PreferConstDestructuringAll
	default:
		return fmt.Errorf("destructuring is %q, which is neither \"any\" nor \"all\"", spelling)
	}
	return nil
}
