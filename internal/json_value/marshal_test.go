package json_value

import (
	"encoding/json"
	"testing"
)

// Each text is compact and its object keys sorted, which is how encoding/json writes a value back, so a value
// that marshals to anything else lost or changed something on the way through Parse. A number keeps
// its own spelling, `1.0` and `1e3` and `-0` as written, and null stays null, where encoding/json alone would
// write a Number as a quoted string and a Null as `{}`.
func TestEachValueMarshalsToTheTextItParsed(test *testing.T) {
	test.Parallel()

	texts := []string{
		`null`,
		`true`,
		`false`,
		`0`,
		`-0`,
		`1.0`,
		`1e3`,
		`-12.5E-7`,
		`12345678901234567890`,
		`""`,
		`"text"`,
		`[]`,
		`{}`,
		`[null,1.50,"a",[0e0],{"key":null}]`,
		`{"array":[1,2],"boolean":true,"null":null,"number":2.000,"object":{"nested":-1e-1},"string":"s"}`,
	}
	for _, text := range texts {
		test.Run(text, func(test *testing.T) {
			test.Parallel()

			value, parseError := Parse([]byte(text))
			if parseError != nil {
				test.Fatal(parseError)
			}
			encoded, marshalError := json.Marshal(value)
			if marshalError != nil {
				test.Fatal(marshalError)
			}
			if string(encoded) != text {
				test.Errorf("parsed %s and marshaled it back as %s", text, encoded)
			}
		})
	}
}

// A Number made by hand rather than parsed still goes through encoding/json's check that it is a number
// literal, so a value that is not one is an error rather than invalid JSON in the output.
func TestNumberThatIsNotOneFailsToMarshal(test *testing.T) {
	test.Parallel()

	if encoded, marshalError := json.Marshal(Number("one")); marshalError == nil {
		test.Errorf("marshaled Number(\"one\") as %s, without an error", encoded)
	}
}
