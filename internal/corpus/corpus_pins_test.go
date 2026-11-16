package corpus

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// The pins the land gate reads from the commit it lands are this package's own constants: every corpus's checkout,
// and Structure's count pin held beside its checkout (#5r0vrte).
func TestThePinsTheGateReadsAreTheConstants(test *testing.T) {
	test.Parallel()
	source, readError := os.ReadFile("corpus_pinned.go")
	if readError != nil {
		test.Fatal(readError)
	}
	pins, pinsError := PinsIn(source)
	if pinsError != nil {
		test.Fatal(pinsError)
	}
	want := map[string]Pins{
		Ahra.Name:         {Checkout: AhraPin, Held: []string{AhraPin}},
		Connected.Name:    {Checkout: ConnectedPin, Held: []string{ConnectedPin}},
		PrettierFork.Name: {Checkout: PrettierForkPin, Held: []string{PrettierForkPin}},
		Structure.Name: {
			Checkout: StructurePin,
			Held:     slices.Sorted(slices.Values([]string{StructurePin, StructureCountPin})),
		},
	}
	if len(pins) != len(want) {
		test.Fatalf("read pins for %d corpora, want %d: %v", len(pins), len(want), pins)
	}
	for name, wanted := range want {
		if pins[name].Checkout != wanted.Checkout || !slices.Equal(pins[name].Held, wanted.Held) {
			test.Errorf("%s: read %+v, want %+v", name, pins[name], wanted)
		}
	}
}

// A corpus with no checkout pin, or a pin that is no whole commit, refuses, so a landing can't lay a corpus at a
// commit it misread.
func TestPinsRefuseWhatNamesNoCommit(test *testing.T) {
	test.Parallel()
	whole := "package corpus\n\n" +
		"const AhraPin = \"958fffe2bbedffa0c514f095a5e70045d44e67eb\"\n" +
		"const ConnectedPin = \"394d26d8a9ec58d06247029c6c415d78f1054c49\"\n" +
		"const PrettierForkPin = \"54c8b90aeaeb09fbc9a4b41717c45911ea55636d\"\n" +
		"const StructurePin = \"1fa60efa5922c795ce8c42b7390ca21bd8030bec\"\n"
	if _, pinsError := PinsIn([]byte(whole)); pinsError != nil {
		test.Fatalf("a file naming every checkout refused: %v", pinsError)
	}
	cases := []struct{ name, source, says string }{
		{"no checkout", strings.Replace(whole, "const ConnectedPin", "const ConnectedCommit", 1), "no ConnectedPin"},
		{"short", strings.Replace(whole, "1fa60efa5922c795ce8c42b7390ca21bd8030bec", "1fa60efa", 1),
			"no whole commit"},
		{"not a string", whole + "const StructureCountPin = 9\n", "is no string"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			_, pinsError := PinsIn([]byte(testCase.source))
			if pinsError == nil || !strings.Contains(pinsError.Error(), testCase.says) {
				test.Fatalf("read %v, want an error saying %q", pinsError, testCase.says)
			}
		})
	}
}
