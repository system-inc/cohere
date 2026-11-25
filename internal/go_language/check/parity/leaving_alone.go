package parity

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// RequireIdenticalLeavingAlone is RequireIdentical, failing as it does where the original finds nothing, over fixtures
// where both must also leave files alone, each of alone named as findings name files, by its path below the fixtures'
// root, as `parity/parity.s`: every finding of either side in such a file fails, named, though the two agree there.
// Each file of alone must be one the load handed a fixture package, among its Go files or its other files, as an
// assembly file is, or it fails, named, since a file the load never handed over is left alone by anything. A package
// holds both the files its check judges and the files it must leave alone, as asmdecl's parity does, so patterns
// cannot part them, and this names the files instead. It loads for the machine's own target, and
// Target.RequireIdenticalLeavingAlone for another, as `On(...).JudgingBy(...)` hands over a file whose architecture
// the check must not be able to name.
func RequireIdenticalLeavingAlone(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	alone []string,
	patterns ...string,
) {
	test.Helper()
	requireIdenticalLeavingAlone(test, Target{}, original, port, testdata, alone, patterns...)
}

// requireIdenticalLeavingAlone is RequireIdenticalLeavingAlone with the fixtures loaded for target.
func requireIdenticalLeavingAlone(
	test testing.TB,
	target Target,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	alone []string,
	patterns ...string,
) {
	test.Helper()
	defer target.held(test)()
	loads := loadFixtures(test, target, testdata, patterns, nil)
	// handed is every file the load handed a root, by the name findings give it.
	handed := map[string]bool{}
	for _, fixtures := range loads {
		for _, root := range fixtures.roots {
			for _, file := range slices.Concat(root.GoFiles, root.OtherFiles) {
				handed[fixtures.fileName(file)] = true
			}
		}
	}
	for _, file := range alone {
		if !handed[file] {
			test.Errorf("the load hands neither side %s, so leaving it alone proves nothing", file)
		}
	}
	originalFindings, portFindings := findingsOver(test, original, port, loads)
	for _, side := range []struct {
		name     string
		findings []Finding
	}{{"original", originalFindings}, {"port", portFindings}} {
		for _, found := range side.findings {
			if slices.Contains(alone, found.File) {
				test.Errorf("the %s found %s, in a file both must leave alone", side.name, found)
			}
		}
	}
	compareIdentical(test, originalFindings, portFindings)
}
