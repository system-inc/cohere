package rule

import "testing"

type givenTestOptions struct {
	Enabled bool
}

// No options configured is the honest miss: the zero value and false, so the rule's default arm runs.
func TestGivenReadsZeroAndFalseWithoutOptions(test *testing.T) {
	test.Parallel()

	settings, configured := Given[givenTestOptions]{}.Read()
	if configured || settings != (givenTestOptions{}) {
		test.Fatalf("expected the zero value and false, got %+v and %v", settings, configured)
	}
}

// The value the decoder returns comes back with true. DecodeOptionsInto is used because it is the decoder most
// registrations carry, and the one whose value type the unified-signatures bug missed (#qmvkf83).
func TestGivenReadsTheDecodedValue(test *testing.T) {
	test.Parallel()

	decoded, decodeError := DecodeOptionsInto[givenTestOptions]()([]byte(`{"enabled": true}`))
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	settings, configured := Configured(decoded).Read()
	if !configured || !settings.Enabled {
		test.Fatalf("expected the decoded value and true, got %+v and %v", settings, configured)
	}
}

// Every read is counted, which is what lets the registry tell a rule that reached its options from one that
// ran without reading them (#zwd43jn).
//
// Not parallel: it reads OptionsReads before and after one read, and another test's read in between would move
// the count by more than one.
func TestGivenCountsEveryRead(test *testing.T) {
	before := OptionsReads.Load()
	Configured(givenTestOptions{}).Read()
	if after := OptionsReads.Load(); after != before+1 {
		test.Fatalf("expected one read counted, got %d", after-before)
	}
}
