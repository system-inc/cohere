package json_value

// Null is JSON's null.
type Null struct{}

func (Null) jsonValue() {}

// MarshalJSON writes null, where encoding/json would write the empty struct as `{}`.
func (Null) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
}
