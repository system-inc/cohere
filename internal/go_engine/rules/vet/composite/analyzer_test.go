package composite

import (
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/tools/go/analysis/passes/composite"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// localFlagPath is the import path the go command gives fixture a's flag, imported by the local path "./flag": an
// underscore and the package's absolute directory.
var localFlagPath = "_" + filepath.Join(analysistest.TestData(), "src", "a", "flag")

// fixtureLoadErrors are the errors the fixture packages load with, all of them x/tools' fixture a's, which fails to
// type-check on purpose since composites runs despite errors: literals of flag.Flag with a field too many and one too
// few, sync.Mutex's unexported fields given values, and a type its import lacks. The import of its flag by a local
// path is refused under the fixtures' `GOPATH`, its position given below that root as the loader gives it. Its test
// variant, `a [a.test]`, checks a.go again and names flag by its local import path, which is absolute, so it repeats
// the two flag.Flag errors in other words, built here from the testdata directory.
var fixtureLoadErrors = []parity.LoadError{
	{Position: "src/a/a.go:16:2", Message: `local import "./flag" in non-local package`},
	{Position: "a/a.go:89:2", Message: "too many values in struct literal of type flag.Flag"},
	{Position: "a/a.go:95:1", Message: "too few values in struct literal of type flag.Flag"},
	{
		Position: "a/a.go:119:21",
		Message:  `implicit assignment to unexported field _ in struct literal of type "sync".Mutex`,
	},
	{
		Position: "a/a.go:119:24",
		Message:  `implicit assignment to unexported field mu in struct literal of type "sync".Mutex`,
	},
	{Position: "a/a.go:127:29", Message: "undefined: unicode.NoSuchType"},
	{
		Position: "a/a.go:89:2",
		Message:  "too many values in struct literal of type " + strconv.Quote(localFlagPath) + ".Flag",
	},
	{
		Position: "a/a.go:95:1",
		Message:  "too few values in struct literal of type " + strconv.Quote(localFlagPath) + ".Flag",
	},
}

// x/tools' own fixtures for composites, copied unchanged into testdata/src and run as x/tools' test runs them, with
// suggested fixes and no flags: every `// want` the original expects, the port reports at the same line with the
// same message, analysistest fails on a finding no want expects as much as on a want nothing found, so the port adds
// nothing there either, and the port's fixes applied to each file give its .golden.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the judgment and the shapes most likely to differ, and their findings must be the same
// multiset of findings, each with its span, related entries and suggested fixes. Fixture a fails to type-check on
// purpose, so the fixtures must load with fixtureLoadErrors exactly, and parity, which holds the shapes a broken
// literal would blur, with none.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdenticalDespiteLoadErrors(test, composite.Analyzer, Analyzer, analysistest.TestData(),
		fixtureLoadErrors, "a", "typeparams", "parity")
}

// The same comparison with the whitelist option set false on both sides, so the allow list's branch is held to the
// original's in both of its settings.
//
// Not parallel: the original keeps its whitelist option in a variable of its package, which every analyzer of it
// reads, so the option is set and put back while no other test of this package runs. Go runs the parallel tests only
// once every sequential one is done.
func TestPortMatchesOriginalWithTheAllowListOff(test *testing.T) {
	if setError := composite.Analyzer.Flags.Set("whitelist", "false"); setError != nil {
		test.Fatalf("turning the original's allow list off: %v", setError)
	}
	test.Cleanup(func() {
		if setError := composite.Analyzer.Flags.Set("whitelist", "true"); setError != nil {
			test.Errorf("turning the original's allow list back on: %v", setError)
		}
	})
	// A dispatcher of its own, since a dispatcher names each rule once and the shared one holds the registry's port.
	port := newAnalyzer(dispatch.NewSyntax())
	if setError := port.Flags.Set("whitelist", "false"); setError != nil {
		test.Fatalf("turning the port's allow list off: %v", setError)
	}
	parity.RequireIdenticalDespiteLoadErrors(test, composite.Analyzer, port, analysistest.TestData(), fixtureLoadErrors,
		"a", "typeparams", "parity")
}
