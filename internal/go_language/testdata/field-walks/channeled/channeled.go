// Package channeled marshals a Channeled.
package channeled

import (
	"encoding/json"

	"example.com/fieldwalks/platform"
)

// Marshal marshals channeled.
func Marshal(channeled *platform.Channeled) ([]byte, error) { return json.Marshal(channeled) }
