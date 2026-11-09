package parity

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// RequireNeitherFinds runs original and port over the fixture packages patterns name, loaded as RequireIdentical
// loads them, where both must leave alone what they are handed, as framepointer leaves alone the assembly of a target
// it does not judge, and of the runtime, and asmdecl a file whose architecture it cannot tell: neither may find
// anything there, and each finding of either fails, named. RequireIdentical refuses such a run, since two empty sides
// match and prove nothing, and keeps refusing it; here judged proves the run was not empty. It is given the root
// packages of every load and counts what of them the two were handed to leave alone, as their assembly files, and a
// count of zero fails, so a load that handed them nothing to judge never passes. The count is logged beside each
// side's.
//
// judged is the port's own, since only the port's test knows what its check would judge, and it may judge the packages
// further, failing the test itself where they do not hold what the run is meant to leave alone, or counting that, as
// the writes framepointer's own judgment finds in the runtime's assembly, so the form holds what the run had to judge
// above zero and the port says what that is. It runs within the call, under the call's target. It loads for the
// machine's own target, and Target.RequireNeitherFinds for another.
func RequireNeitherFinds(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	judged func(test testing.TB, loaded []*packages.Package) int,
	patterns ...string,
) {
	test.Helper()
	requireNeitherFinds(test, Target{}, original, port, testdata, judged, patterns...)
}

// requireNeitherFinds is RequireNeitherFinds with the fixtures loaded for target.
func requireNeitherFinds(
	test testing.TB,
	target Target,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	judged func(test testing.TB, loaded []*packages.Package) int,
	patterns ...string,
) {
	test.Helper()
	defer target.held(test)()
	loads := loadFixtures(test, target, testdata, patterns, nil)
	roots := []*packages.Package{}
	for _, fixtures := range loads {
		roots = append(roots, fixtures.roots...)
	}
	counted := judged(test, roots)
	originalFindings, portFindings := findingsOver(test, original, port, loads)
	test.Logf("over %d judged, the original found %d, the port %d", counted, len(originalFindings),
		len(portFindings))
	if counted <= 0 {
		test.Fatalf("judged counts %d in the packages loaded, so neither side was handed anything to judge, and "+
			"finding nothing proves nothing", counted)
	}
	for _, found := range originalFindings {
		test.Errorf("the original found %s, where neither may find anything", found)
	}
	for _, found := range portFindings {
		test.Errorf("the port found %s, where neither may find anything", found)
	}
}
