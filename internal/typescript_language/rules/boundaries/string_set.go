package boundaries

import (
	"encoding/json"
	"fmt"
)

// stringSet is upstream's string-or-array-of-strings.
type stringSet []string

func (set *stringSet) UnmarshalJSON(raw []byte) error {
	var single string
	if decodeError := json.Unmarshal(raw, &single); decodeError == nil {
		*set = stringSet{single}
		return nil
	}
	var many []string
	if decodeError := json.Unmarshal(raw, &many); decodeError != nil {
		return fmt.Errorf("expected a string or an array of strings, got %s", raw)
	}
	*set = many
	return nil
}
