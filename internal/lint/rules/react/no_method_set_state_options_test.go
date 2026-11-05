package react

import "testing"

// noMethodSetStateOptions decodes one fixture's option the way the config layer would.
//
// The option column in the three set-state rules' tables is upstream's spelling: "" for a bare
// severity, and otherwise the single string upstream passes. Routing it through the decoder rather
// than building the struct is what keeps the decoder under every fixture.
func noMethodSetStateOptions(test *testing.T, option string) any {
	test.Helper()
	raw := []byte(nil)
	if option != "" {
		raw = []byte(`"` + option + `"`)
	}
	decoded, decodeNoMethodSetStateOptionsError := DecodeNoMethodSetStateOptions(raw)
	if decodeNoMethodSetStateOptionsError != nil {
		test.Fatalf("decoding option %q: %v", option, decodeNoMethodSetStateOptionsError)
	}
	return decoded
}

// TestDecodeNoMethodSetStateOptions pins the decoder against upstream's schema,
// `[{ enum: ["disallow-in-func"] }]`.
func TestDecodeNoMethodSetStateOptions(test *testing.T) {
	test.Parallel()

	accepted := []struct {
		name string
		raw  string
		want bool
	}{
		{"no options at all allows nested functions", ``, false},
		{"upstream's disallow-in-func", `"disallow-in-func"`, true},
	}
	for _, testCase := range accepted {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, decodeNoMethodSetStateOptionsError := DecodeNoMethodSetStateOptions([]byte(testCase.raw))
			if decodeNoMethodSetStateOptionsError != nil {
				test.Fatalf("unexpected error: %v", decodeNoMethodSetStateOptionsError)
			}
			if decoded.(NoMethodSetStateOptions).DisallowInFunc != testCase.want {
				test.Errorf("DisallowInFunc = %v, wanted %v", decoded.(NoMethodSetStateOptions).DisallowInFunc, testCase.want)
			}
		})
	}

	// The two objects are the shapes cohere invented before #d21war2, one per rule family member,
	// and "allowed" is oxc's spelling of the default. No ESLint version accepts any of them.
	refused := []struct {
		name string
		raw  string
	}{
		{"the old invented boolean object", `{"disallowInFunc":true}`},
		{"the old invented mode object", `{"mode":"disallow-in-func"}`},
		{"oxc's allowed", `"allowed"`},
		{"upstream's internal default name", `"allow-in-func"`},
		{"a camelCase spelling", `"disallowInFunc"`},
		{"an empty object", `{}`},
		{"an empty string", `""`},
		{"null", `null`},
	}
	for _, testCase := range refused {
		test.Run(testCase.name+" is refused", func(test *testing.T) {
			test.Parallel()
			if _, decodeNoMethodSetStateOptionsError := DecodeNoMethodSetStateOptions([]byte(testCase.raw)); decodeNoMethodSetStateOptionsError == nil {
				test.Errorf("%s decoded; upstream's schema refuses it", testCase.raw)
			}
		})
	}
}
