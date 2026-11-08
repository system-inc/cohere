package parity

import (
	"go/ast"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// marking is an analyzer that exports a mark[Side] about each function names names that its package declares. The
// original marks with mark[originalSide] and the port with mark[portSide], since the checker refuses one fact type
// two analyzers declare.
func marking[Side any](names ...string) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:      "marking",
		Doc:       "marking exports a fact about each function it names.",
		FactTypes: []analysis.Fact{&mark[Side]{}},
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				for _, declaration := range file.Decls {
					function, isFunction := declaration.(*ast.FuncDecl)
					if isFunction && slices.Contains(names, function.Name.Name) {
						pass.ExportObjectFact(pass.TypesInfo.Defs[function.Name], &mark[Side]{Marked: true})
					}
				}
			}
			return nil, nil
		},
	}
}

// mark is a planted fact, printed as marked whichever side's it is, so the two sides' facts compare by where they
// are. It holds a field, since the checker encodes a fact with gob, which refuses a struct with none exported.
type mark[Side any] struct{ Marked bool }

// originalSide and portSide tell the two sides' mark types apart.
type (
	originalSide struct{}
	portSide     struct{}
)

func (*mark[Side]) AFact() {
	// AFact only marks the type as a fact, so it does nothing.
}

func (*mark[Side]) String() string { return "marked" }

// A pair that marks the same functions passes.
func TestRequireIdenticalFactsPassesTheSameFacts(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalFacts(recording, marking[originalSide]("First", "Second"),
			marking[portSide]("First", "Second"), analysistest.TestData(), "facts")
	}))
}

// A port that exports one fact fewer fails on that fact, and one that exports one more on that one, each named by
// its object's position, name and fact.
func TestRequireIdenticalFactsFailsAFactOnOneSide(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalFacts(recording, marking[originalSide]("First", "Second"), marking[portSide]("First"),
			analysistest.TestData(), "facts")
	}), "the original exported facts/facts.go:7:6: Second marked, and the port did not")
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalFacts(recording, marking[originalSide]("First", "Second"),
			marking[portSide]("First", "Second", "Third"), analysistest.TestData(), "facts")
	}), "the port exported facts/facts.go:9:6: Third marked, and the original did not")
}

// Two sides that export nothing match, and prove nothing.
func TestRequireIdenticalFactsFailsWhenTheOriginalExportsNothing(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalFacts(recording, marking[originalSide](), marking[portSide](), analysistest.TestData(),
			"facts")
	}), "the original exported no fact in the fixtures, so the comparison proves nothing")
}

// The facts on a package the fixture imports are compared as much as the fixture's own: over facts_importer, whose
// own First both sides mark, a port that leaves out the mark on Second of facts, the dependency, fails on it.
func TestRequireIdenticalFactsComparesTheFactsOfADependency(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalFacts(recording, marking[originalSide]("First", "Second"),
			marking[portSide]("First", "Second"), analysistest.TestData(), "facts_importer")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalFacts(recording, marking[originalSide]("First", "Second"), marking[portSide]("First"),
			analysistest.TestData(), "facts_importer")
	}), "the original exported facts/facts.go:7:6: Second marked, and the port did not")
}
