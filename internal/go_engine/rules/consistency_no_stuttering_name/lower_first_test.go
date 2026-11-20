package consistency_no_stuttering_name

import (
	"testing"
	"unicode"
	"unicode/utf8"
)

// lowerFirst hands back a name that opens with an ASCII byte outside A to Z as it is, without lowering it, so it must
// give what lowering the first rune gives for every opening: the letters at both ends of A to Z and the bytes just
// outside them, lowercase letters, digits and the underscore, the last ASCII byte, a multibyte letter in either case,
// a byte that is no rune, a single letter and the empty name.
func TestLowerFirstAgreesWithLoweringTheFirstRune(test *testing.T) {
	test.Parallel()
	for _, name := range []string{
		"Result", "Ahead", "Zone", "@result", "[result", "`result", "{result",
		"result", "aside", "zone", "_result", "0result", "\x7fresult",
		"Ölçü", "ölçü", "Σigma", "\xffresult", "R", "r", "",
	} {
		first, size := utf8.DecodeRuneInString(name)
		want := string(unicode.ToLower(first)) + name[size:]
		if got := lowerFirst(name); got != want {
			test.Errorf("lowerFirst(%q) = %q, want %q", name, got, want)
		}
	}
}

// A name that opens with a byte lowering leaves alone is the name itself, built into no new string.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestLowerFirstBuildsNoStringForALowercaseName(test *testing.T) {
	names := []string{"result", "fmt", "_result", "0result"}
	allocations := testing.AllocsPerRun(10, func() {
		for _, name := range names {
			lowerFirst(name)
		}
	})
	if allocations != 0 {
		test.Errorf("lowering names that already open lowercase allocated %.0f times, want none", allocations)
	}
}
