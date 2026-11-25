package store

import "encoding/json"

// stamp is encoded by its own method, which json.Marshaler names, so the method keeps its name.
type stamp struct{}

// MarshalJSON encodes a stamp as null.
func (stamp) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
}

// Stamp encodes a stamp.
func Stamp() ([]byte, error) {
	return json.Marshal(stamp{})
}
