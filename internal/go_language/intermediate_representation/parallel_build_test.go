package intermediate_representation_test

// A package large enough to build on several workers (#s2z79xj), whose functions' builds create functions of their
// own as they go: each instantiates a generic function at a type of its own, takes a method value, which makes a
// bound wrapper, and converts its type to an interface, which makes the method set's wrappers. Built, it must come
// out function for function as go/ir's eager build does, every function built once and none left unbuilt.

import (
	"fmt"
	"strings"
	"testing"
)

// parallelBuildFunctions is how many functions the fixture declares, well past the package size that builds on
// several workers, so the parallel path is the one taken.
const parallelBuildFunctions = 300

// parallelBuildFixture is the fixture's source.
func parallelBuildFixture() string {
	var source strings.Builder
	source.WriteString("package parallel\n\n")
	source.WriteString("type speaker interface{ speak() int }\n\n")
	source.WriteString("func identity[T any](value T) T { return value }\n\n")
	for index := range parallelBuildFunctions {
		fmt.Fprintf(&source, "type value%d struct{ number int }\n\n", index)
		fmt.Fprintf(&source, "func (each value%d) speak() int { return each.number + %d }\n\n", index, index)
		fmt.Fprintf(&source, "func use%d(input int) int {\n", index)
		fmt.Fprintf(&source, "\tmade := identity(value%d{number: input})\n", index)
		source.WriteString("\tbound := made.speak\n")
		source.WriteString("\tvar asked speaker = made\n")
		source.WriteString("\tif input > 0 {\n\t\treturn bound() + asked.speak()\n\t}\n")
		source.WriteString("\treturn identity(input)\n}\n\n")
	}
	return source.String()
}

func TestParallelBuildMatchesTheEagerBuild(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "parallel", parallelBuildFixture())

	tally := differentialCompareUnit(test, unit, nil)

	for _, difference := range tally.differences {
		test.Errorf("%s: %s at %s: go/ir has %s, ours %s", difference.function, difference.cause,
			difference.where, difference.theirs, difference.ours)
	}
	// Every declared function, its method, and the instances and wrappers their builds create: at least the two
	// declared per type, so a function left unbuilt or never compared shows here.
	if tally.functions < 2*parallelBuildFunctions || tally.identical != tally.functions {
		test.Errorf("compared %d functions, %d identical, want at least %d and all identical", tally.functions,
			tally.identical, 2*parallelBuildFunctions)
	}
	test.Logf("%d functions compared, %d identical, %d instructions", tally.functions, tally.identical,
		tally.instructions)
}
