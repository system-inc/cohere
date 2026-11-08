package ineffective_sort

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa4029"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreams are staticcheck's own fixtures for SA4029, copied unchanged from
// staticcheck/sa4029/testdata/go1.0/CheckIneffectiveSort and go1.9/CheckIneffectiveSort with their golden files. Both
// folders hold the same package name, so each keeps its version folder as the first element of its import path,
// testdata/src/go1.0/CheckIneffectiveSort and testdata/src/go1.9/CheckIneffectiveSort; the version sets the language
// version of staticcheck's run, which SA4029 never reads, so the fixtures load from a tree laid out by import path as
// the house's fixtures do.
var upstreams = []string{"go1.0/CheckIneffectiveSort", "go1.9/CheckIneffectiveSort"}

// staticcheck's own fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreams...)
}

// The port's fixes, applied to each of staticcheck's fixtures, make its golden file, as staticcheck's testutil
// requires of the original's.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstreams...)
}

// Over staticcheck's fixtures and parity's packages, the original and the port find the same findings, fixes
// included. parity holds every way the pattern accepts or refuses an assignment: each of sort's three types,
// parentheses around the target, the conversion, its function and its argument, a renamed import, a package-level
// alias and an alias of it, an alias a function declares, targets of a named slice type, of sort's own type, of a type
// parameter and of a field, an argument spelled otherwise, := and two targets, and a generated file; parity/dotted
// imports sort into its scope; parity/through reaches sort.IntSlice only through parity/aliases' alias, so the type
// index holds no sort there and the original walks nothing, and parity/throughmethod does the same but calls a sort
// method, so the index holds sort and the original walks it.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4029.Analyzer, Analyzer, analysistest.TestData(), append(slices.Clone(upstreams),
		"parity", "parity/dotted", "parity/aliases", "parity/through", "parity/throughmethod")...)
}

// The differences fixture holds the shapes where the rendered message and the printed fix could part, a conversion
// spread over lines, beside comments and inside parentheses. It records no difference, since there is none, so the two
// must agree there finding for finding, fixes included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4029.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
