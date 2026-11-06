package configuration

import (
	"encoding/json"
	"fmt"
)

// extendsList is what `extends` names: one source, or a list of them, applied in order.
//
// A list is how a project composes sets that each sit on the same base (`cohere:react` and
// `cohere:next` both extend `cohere:typescript`). The shared base is read once, where it first
// appears, so it stays the outermost layer.
type extendsList []string

func (list *extendsList) UnmarshalJSON(data []byte) error {
	var single string
	if decodeError := json.Unmarshal(data, &single); decodeError == nil {
		if single != "" {
			*list = extendsList{single}
		}
		return nil
	}
	var many []string
	if decodeError := json.Unmarshal(data, &many); decodeError != nil {
		return fmt.Errorf("\"extends\" names a path or a rule set, or a list of them: %w", decodeError)
	}
	for _, entry := range many {
		if entry == "" {
			return fmt.Errorf("\"extends\" lists an empty entry")
		}
	}
	*list = many
	return nil
}
