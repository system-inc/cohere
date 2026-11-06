package jsonvalue

import "encoding/json"

// JsonNumber is a JSON number kept as its own text, as json.Number keeps it, so `1.0` and `1e3` keep their
// spelling and nothing is lost to float64 before a reader asks for one.
type JsonNumber json.Number

func (JsonNumber) jsonValue() {}

// MarshalJSON writes the number as its own text. encoding/json knows json.Number by its type alone, so
// without this a JsonNumber would encode as the quoted string it is underneath. Handing the text back as a
// json.Number keeps encoding/json's own check that it is a number literal.
func (number JsonNumber) MarshalJSON() ([]byte, error) {
	return json.Marshal(json.Number(number))
}
