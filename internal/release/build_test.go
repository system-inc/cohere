package release

import (
	"strings"
	"testing"
)

// A released binary that quietly stops being stripped grows about 18 MB, measured at 43.1 against
// 61.9 for the same commit. Nothing fails when that happens: the build succeeds, the packages stage,
// the binaries run. The only signal is a size nobody was watching, which is why it is asserted here
// rather than trusted to review.

func TestReleaseBuildStripsSymbols(t *testing.T) {
	if len(StripFlags) == 0 {
		t.Fatalf("no strip flags, so released binaries would ship with symbols and DWARF")
	}

	stripped := strings.Join(StripFlags, " ")
	for _, required := range []string{"-s", "-w"} {
		if !strings.Contains(stripped, required) {
			t.Errorf("strip flags %q are missing %s", stripped, required)
		}
	}
}

func TestDescribeBuildNamesTheFlagsActuallyUsed(t *testing.T) {
	// A size label that names the wrong build is worse than an unlabeled size: it is confidently
	// wrong rather than ambiguous. So the description is built from the same values the compiler
	// receives, and this asserts it cannot drift into a hand-written string.
	description := DescribeBuild()

	for _, flag := range BuildFlags {
		if !strings.Contains(description, flag) {
			t.Errorf("the build description %q omits %s, which the build actually passes", description, flag)
		}
	}
	for _, flag := range StripFlags {
		if !strings.Contains(description, flag) {
			t.Errorf("the build description %q omits %s, which the build actually passes", description, flag)
		}
	}
}
