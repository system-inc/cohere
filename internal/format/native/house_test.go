package native

import (
	"strings"
	"testing"
)

// FormatHouse is the house format whatever directory it runs in: a generator's JSON comes out with the house's
// four-space indentation and a short array on one line, a second pass changes nothing, which is what lets a
// regeneration leave the tree as it was, and a file type no printer reads is refused rather than written
// unformatted (#4wjhppw).
func TestFormatHouseIsTheHouseFormatAndAFixedPoint(test *testing.T) {
	test.Parallel()
	generated := "{\n  \"rules\": {\n    \"a\": [\n      \"x\",\n      \"y\"\n    ]\n  }\n}\n"
	formatted, formatHouseError := FormatHouse("docs/data/rules.json", []byte(generated))
	if formatHouseError != nil {
		test.Fatal(formatHouseError)
	}
	if want := "{\n    \"rules\": {\n        \"a\": [\"x\", \"y\"]\n    }\n}\n"; string(formatted) != want {
		test.Fatalf("formatted %q, want %q", formatted, want)
	}
	again, formatHouseError := FormatHouse("docs/data/rules.json", formatted)
	if formatHouseError != nil {
		test.Fatal(formatHouseError)
	}
	if string(again) != string(formatted) {
		test.Errorf("a second pass changed %q to %q", formatted, again)
	}

	if _, formatHouseError := FormatHouse("NOTICE", []byte("text\n")); formatHouseError == nil ||
		!strings.Contains(formatHouseError.Error(), "NOTICE") {
		test.Errorf("a file no printer reads was not refused by name: %v", formatHouseError)
	}
}
