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
	decoders := map[string]func([]byte) (any, error){
		"no-meaningless-void-operator": DecodeNoMeaninglessVoidOperatorOptions,
		"require-array-sort-compare":   DecodeRequireArraySortCompareOptions,
		"strict-void-return":           DecodeStrictVoidReturnOptions,
	}
	for name, decode := range decoders {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			if _, decodeError := decode([]byte(`{}`)); decodeError != nil {
				test.Fatalf("baseline: an empty options object was refused: %v", decodeError)
			}
			_, decodeError := decode([]byte(`{"somethingElse": 1}`))
			if decodeError == nil || !strings.Contains(decodeError.Error(), "somethingElse") {
				test.Fatalf("want a refusal naming the key, got %v", decodeError)
			}
		})
	}
}
