package typescript

import (
	"strings"
	"testing"
)

// TestOptionDecodersRefuseAnUnknownKey replaces three table rows that asserted `{"somethingElse": 1}`
// decodes to the defaults. Upstream's schema for each of these rules sets `additionalProperties:
// false` (checked in the installed typescript-eslint), so ESLint refuses the key, and a config that
// loads clean while an option does nothing is the silent drop #4n972g9 closed.
func TestOptionDecodersRefuseAnUnknownKey(test *testing.T) {
	test.Parallel()
	decoders := map[string]func([]byte) error{
		"no-meaningless-void-operator": decodeErrorOf(DecodeNoMeaninglessVoidOperatorOptions),
		"require-array-sort-compare":   decodeErrorOf(DecodeRequireArraySortCompareOptions),
		"strict-void-return":           decodeErrorOf(DecodeStrictVoidReturnOptions),
	}
	for name, decode := range decoders {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			if decodeError := decode([]byte(`{}`)); decodeError != nil {
				test.Fatalf("baseline: an empty options object was refused: %v", decodeError)
			}
			decodeError := decode([]byte(`{"somethingElse": 1}`))
			if decodeError == nil || !strings.Contains(decodeError.Error(), "somethingElse") {
				test.Fatalf("want a refusal naming the key, got %v", decodeError)
			}
		})
	}
}

// decodeErrorOf is a decoder with its value dropped, so decoders of different options types sit in one
// table: this test reads only whether each refused its input.
func decodeErrorOf[Options any](decode func(raw []byte) (Options, error)) func(raw []byte) error {
	return func(raw []byte) error {
		_, decodeError := decode(raw)
		return decodeError
	}
}
