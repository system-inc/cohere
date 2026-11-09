package parity

import (
	"cmp"
	"fmt"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// RequireStaticcheckWants runs port over staticcheck's own fixtures in testdata, copied unchanged: every `//@ diag`
// note they hold, read by staticcheck_wants, the port reports at the note's line with a message the note matches, and
// it reports nothing no note expects, so the port adds nothing there either. Where the fixtures hold a `//@ fact` note,
// facts are held the same way, as testutil holds them: every fact note the port meets with a fact it exports about the
// object the note names, declared on the note's line, printed as the note says, and it exports no fact about an object
// of a package it reads notes for that no note expects. Fixtures with no fact note hold no fact. Each package is held
// to the notes of its own files, as staticcheck's testutil holds it, so a file a package and its test variant share is
// held in each. A fixture module (see fixtureLoad) loads under its own go.mod, as testutil runs each version's folder,
// and fixtures that hold no note at all fail, since the comparison would prove nothing. It loads for the machine's own
// target, and Target.RequireStaticcheckWants for another.
func RequireStaticcheckWants(test testing.TB, port *analysis.Analyzer, testdata string, patterns ...string) {
	test.Helper()
	requireStaticcheckWants(test, Target{}, port, testdata, patterns...)
}

// requireStaticcheckWants is RequireStaticcheckWants with the fixtures loaded for target.
func requireStaticcheckWants(
	test testing.TB,
	target Target,
	port *analysis.Analyzer,
	testdata string,
	patterns ...string,
) {
	test.Helper()
	defer target.held(test)()
	wanted := 0
	reported := 0
	factsWanted := 0
	exported := 0
	// A fact's failures wait until every package is read, since a fact is held only where the fixtures hold a fact
	// note somewhere.
	factFailures := []string{}
	for _, fixtures := range loadFixtures(test, target, testdata, patterns, nil) {
		for _, action := range portActions(test, port, fixtures.roots) {
			wants := []staticcheck_wants.Want{}
			factWants := []staticcheck_wants.FactWant{}
			for _, file := range action.Package.GoFiles {
				fileWants, fileFactWants, readError := staticcheck_wants.ReadWithFacts(file)
				if readError != nil {
					test.Fatalf("reading the notes of %s: %v", fixtures.fileName(file), readError)
				}
				wants = append(wants, fileWants...)
				factWants = append(factWants, fileFactWants...)
			}
			findings := []staticcheck_wants.Finding{}
			for _, diagnostic := range action.Diagnostics {
				position := action.Package.Fset.Position(diagnostic.Pos)
				findings = append(findings, staticcheck_wants.Finding{
					File:    position.Filename,
					Line:    position.Line,
					Message: diagnostic.Message,
				})
			}
			wanted += len(wants)
			reported += len(findings)
			unexpected, untaken := staticcheck_wants.Compare(wants, findings)
			for _, found := range unexpected {
				test.Errorf("%s:%d: the port reported %q in %s, which no note on the line expects",
					fixtures.fileName(found.File), found.Line, found.Message, action.Package.ID)
			}
			for _, want := range untaken {
				// The want as its fixture writes it, its file named as the findings name theirs.
				want.File = fixtures.fileName(want.File)
				test.Errorf("%s: the port reported nothing in %s matching it", want, action.Package.ID)
			}
			facts := []staticcheck_wants.FoundFact{}
			for _, objectFact := range action.AllObjectFacts() {
				// The action holds the facts it inherited from the packages it imports beside its own.
				if objectFact.Object.Pkg() != action.Package.Types {
					continue
				}
				position := action.Package.Fset.Position(objectFact.Object.Pos())
				facts = append(facts, staticcheck_wants.FoundFact{
					File:   position.Filename,
					Line:   position.Line,
					Object: objectFact.Object.Name(),
					Fact:   fmt.Sprint(objectFact.Fact),
				})
			}
			// AllObjectFacts reads a map, so the facts are sorted, and their failures read in the same order every run.
			slices.SortFunc(facts, func(first staticcheck_wants.FoundFact, second staticcheck_wants.FoundFact) int {
				return cmp.Or(cmp.Compare(first.File, second.File), cmp.Compare(first.Line, second.Line),
					cmp.Compare(first.Object, second.Object), cmp.Compare(first.Fact, second.Fact))
			})
			factsWanted += len(factWants)
			exported += len(facts)
			unexpectedFacts, untakenFacts := staticcheck_wants.CompareFacts(factWants, facts)
			for _, found := range unexpectedFacts {
				factFailures = append(factFailures, fmt.Sprintf("%s:%d: the port exported %q about %s in %s, which "+
					"no note on the line expects", fixtures.fileName(found.File), found.Line, found.Fact, found.Object,
					action.Package.ID))
			}
			for _, want := range untakenFacts {
				want.File = fixtures.fileName(want.File)
				factFailures = append(factFailures, fmt.Sprintf("%s: the port exported nothing in %s matching it",
					want, action.Package.ID))
			}
		}
	}
	if factsWanted == 0 {
		test.Logf("the fixtures want %d, the port reported %d", wanted, reported)
	} else {
		test.Logf("the fixtures want %d findings and %d facts, the port reported %d and exported %d", wanted,
			factsWanted, reported, exported)
		for _, failure := range factFailures {
			test.Errorf("%s", failure)
		}
	}
	if wanted == 0 && factsWanted == 0 {
		test.Fatalf("the fixtures hold no note, so the comparison proves nothing")
	}
}

// portActions runs port alone over roots in one checker and returns its actions there, one for each root. A package
// it did not run on fails the test, since silence there would read as agreement.
func portActions(test testing.TB, port *analysis.Analyzer, roots []*packages.Package) []*checker.Action {
	test.Helper()
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{port}, roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing %d packages: %v", len(roots), analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running on %s: %v", action.Package.ID, action.Err)
		}
	}
	return graph.Roots
}
