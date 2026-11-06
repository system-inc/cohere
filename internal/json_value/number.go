package json_value

import "encoding/json"

// Number is a JSON number kept as its own text, as json.Number keeps it, so `1.0` and `1e3` keep their
// spelling and nothing is lost to float64 before a reader asks for one.
type Number json.Number

func (Number) jsonValue() {}

// MarshalJSON writes the number as its own text. encoding/json knows json.Number by its type alone, so
// without this a Number would encode as the quoted string it is underneath. Handing the text back as a
// json.Number keeps encoding/json's own check that it is a number literal.
func (number Number) MarshalJSON() ([]byte, error) {
	return json.Marshal(json.Number(number))
}
