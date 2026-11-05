package rule

import (
	"strings"
	"testing"
)

type optionsAsTestOptions struct {
	Enabled bool
}

// No options configured is the honest miss: the zero value and false, so the rule's default arm runs.
func TestOptionsAsGivesZeroAndFalseForNoOptions(test *testing.T) {
	test.Parallel()

	settings, ok := OptionsAs[optionsAsTestOptions](nil)
	if ok || settings != (optionsAsTestOptions{}) {
		test.Fatalf("expected the zero value and false, got %+v and %v", settings, ok)
	}
}

// The type the decoder returns comes back with true. DecodeOptionsInto is used, because it is the
// decoder 93 registrations carry and the one whose value type the unified-signatures bug missed.
func TestOptionsAsReadsTheDecodedValue(test *testing.T) {
	test.Parallel()

	decoded, decodeOptionsError := DecodeOptionsInto[optionsAsTestOptions]()([]byte(`{"enabled": true}`))
	if decodeOptionsError != nil {
		test.Fatal(decodeOptionsError)
	}
	settings, ok := OptionsAs[optionsAsTestOptions](decoded)
	if !ok || !settings.Enabled {
		test.Fatalf("expected the decoded value and true, got %+v and %v", settings, ok)
	}
}

// The defect itself: a pointer read over the value the decoder returns must fail loudly, naming both
// types, where the comma-ok assertion it replaces returned nil and false and the options vanished.
func TestOptionsAsPanicsWhenTheDecoderAndTheRuleDisagree(test *testing.T) {
	test.Parallel()

	decoded, decodeOptionsError := DecodeOptionsInto[optionsAsTestOptions]()([]byte(`{"enabled": true}`))
	if decodeOptionsError != nil {
		test.Fatal(decodeOptionsError)
	}
	defer func() {
		recovered := recover()
		message, isString := recovered.(string)
		if !isString {
			test.Fatalf("expected a panic with a message, got %v", recovered)
		}
		for _, name := range []string{"*rule.optionsAsTestOptions", "rule.optionsAsTestOptions"} {
			if !strings.Contains(message, name) {
				test.Fatalf("expected the panic to name %s, got: %s", name, message)
			}
		}
	}()
	OptionsAs[*optionsAsTestOptions](decoded)
}
