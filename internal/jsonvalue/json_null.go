package jsonvalue

// JsonNull is JSON's null.
type JsonNull struct{}

func (JsonNull) jsonValue() {}

// MarshalJSON writes null, where encoding/json would write the empty struct as `{}`.
func (JsonNull) MarshalJSON() ([]byte, error) {
	return []byte("null"), nil
}
