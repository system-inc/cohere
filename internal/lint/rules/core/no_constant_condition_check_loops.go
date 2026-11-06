package core

import (
	"encoding/json"
	"fmt"
)

// NoConstantConditionCheckLoops is the `checkLoops` value, in the three-string spelling.
type NoConstantConditionCheckLoops string

// UnmarshalJSON reads every value upstream's schema accepts and refuses the rest.
//
// The schema is `enum: ["all", "allExceptWhileTrue", "none", true, false]`, and upstream maps the
// two booleans onto the strings before anything reads them: true is "all" and false is "none". The
// booleans are the option's older spelling, and ESLint 10.8.1 still accepts them (#d21war2). An
// unlisted string is refused rather than reaching the rule, where it would match no case and check
// every loop.
func (checkLoops *NoConstantConditionCheckLoops) UnmarshalJSON(raw []byte) error {
	// `null` decodes into a bool as false without an error, which would read it as "none".
	if string(raw) == "null" {
		return fmt.Errorf("checkLoops is null, which upstream's schema refuses")
	}
	var enabled bool
	if decodeError := json.Unmarshal(raw, &enabled); decodeError == nil {
		*checkLoops = "none"
		if enabled {
			*checkLoops = "all"
		}
		return nil
	}
	var spelling string
	if decodeError := json.Unmarshal(raw, &spelling); decodeError != nil {
		return fmt.Errorf(`checkLoops is %s, which is neither a string nor a boolean`, raw)
	}
	switch spelling {
	case "all", "allExceptWhileTrue", "none":
		*checkLoops = NoConstantConditionCheckLoops(spelling)
		return nil
	}
	return fmt.Errorf(`checkLoops is %q, which is not "all", "allExceptWhileTrue", "none", true or false`, spelling)
}
