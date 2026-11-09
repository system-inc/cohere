package parity

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// moduleFacts is the tree the corpus differential's own tests run over: a module of one package whose functions the
// planted analyzers mark.
func moduleFacts() string {
	return filepath.Join(analysistest.TestData(), "module_facts")
}

// A pair that marks the same functions over the tree, the unexported one and the one named `_` among them, passes.
func TestRequireIdenticalCorpusFactsPassesTheSameFacts(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalCorpusFacts(recording, marking[originalSide]("First", "third", "_"),
			marking[portSide]("First", "third", "_"), moduleFacts())
	}))
}

// A fact on an unexported function, which no importer inherits and RequireIdenticalFacts never compares, is compared
// here, and since objectpath gives such a function no path, it is keyed by where it is declared.
func TestRequireIdenticalCorpusFactsFailsAnUnexportedFactOnOneSide(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalCorpusFacts(recording, marking[originalSide]("First", "third"), marking[portSide]("First"),
			moduleFacts())
	}), `on example.com/module_facts, example.com/module_facts.third: the original exported "marked", and the port `+
		`no fact (declared at `)
}

// Two sides that export nothing match, and prove nothing.
func TestRequireIdenticalCorpusFactsFailsWhenTheOriginalExportsNothing(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireIdenticalCorpusFacts(recording, marking[originalSide](), marking[portSide](), moduleFacts())
	}), "the original exported no fact over")
}
