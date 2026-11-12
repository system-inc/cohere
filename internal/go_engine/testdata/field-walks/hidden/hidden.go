// Package hidden marshals a Hidden.
package hidden

import (
	"encoding/json"

	"example.com/fieldwalks/platform"
)

// Marshal marshals hidden.
func Marshal(hidden *platform.Hidden) ([]byte, error) { return json.Marshal(hidden) }
