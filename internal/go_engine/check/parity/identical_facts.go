package parity

import (
	"fmt"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
)

// RequireIdenticalFacts runs original and port over the fixture packages patterns name, loaded as RequireIdentical
// loads them, and the facts each exports on every package the checker ran it on, the fixture packages and every
// package they import, the standard library's among them, must be the same multiset, since a fact one side exports on
// a dependency decides what it finds in the packages importing it: each a Finding at its object's position, its file
// named as findings name theirs, or by its path outside the fixtures, whose Message is the object's name and the fact
// as `%v` prints it, as `Println isWrapper(...)`, or for a fact about a package, a Finding with no position whose
// Message is `package`, the package's path and the fact. A fact is one an importer inherits (see
// checker.Action.ExportedFacts), so a fact kept within its own package is not compared, and the two sides' fact types
// differ, since the checker refuses one two analyzers declare, so what counts is how a fact prints. Each side's count
// is logged, since two sides that export nothing would match and prove nothing, and the test fails if the original
// exports nothing at all, as it does on a fixture package that did not load clean or one either side did not run on,
// a dependency among them. It loads for the machine's own target, and Target.RequireIdenticalFacts for another.
func RequireIdenticalFacts(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	requireIdenticalFacts(test, Target{}, original, port, testdata, patterns...)
}

// requireIdenticalFacts is RequireIdenticalFacts with the fixtures loaded for target.
func requireIdenticalFacts(
	test testing.TB,
	target Target,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	defer target.held(test)()
	exported := map[*analysis.Analyzer][]Finding{original: {}, port: {}}
	for _, fixtures := range loadFixtures(test, target, testdata, patterns, nil) {
		graph, analyzeError := checker.Analyze([]*analysis.Analyzer{original, port}, fixtures.roots, &checker.Options{})
		if analyzeError != nil {
			test.Fatalf("analyzing %s: %v", fixtures.described(), analyzeError)
		}
		for action := range graph.All() {
			// The graph holds the actions of the analyzers the two require as well, whose facts are neither side's.
			if action.Analyzer != original && action.Analyzer != port {
				continue
			}
			if action.Err != nil {
				test.Fatalf("running on %s: %v", action.Package.ID, action.Err)
			}
			objectFacts, packageFacts := action.ExportedFacts()
			for _, objectFact := range objectFacts {
				position := action.Package.Fset.Position(objectFact.Object.Pos())
				exported[action.Analyzer] = append(exported[action.Analyzer], Finding{
					File:    fixtures.fileName(position.Filename),
					Line:    position.Line,
					Column:  position.Column,
					Message: fmt.Sprintf("%s %v", objectFact.Object.Name(), objectFact.Fact),
				})
			}
			for _, packageFact := range packageFacts {
				exported[action.Analyzer] = append(exported[action.Analyzer], Finding{
					Message: fmt.Sprintf("package %s %v", packageFact.Package.Path(), packageFact.Fact),
				})
			}
		}
	}
	// ExportedFacts reads a map, so each side is sorted, and its failures read in the same order every run.
	slices.SortFunc(exported[original], compareFindings)
	slices.SortFunc(exported[port], compareFindings)
	test.Logf("the original exported %d facts, the port %d", len(exported[original]), len(exported[port]))
	if len(exported[original]) == 0 {
		test.Fatalf("the original exported no fact in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range Difference(exported[original], exported[port]) {
		test.Errorf("the original exported %s, and the port did not", missing)
	}
	for _, extra := range Difference(exported[port], exported[original]) {
		test.Errorf("the port exported %s, and the original did not", extra)
	}
}
