package function_purity

import (
	"testing"

	"honnef.co/go/tools/analysis/facts/purity"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// The purity fixture marks every function the port's push holds pure, and only those, each `// want` naming one, so
// what the port's judgment judges is pinned apart from the original as well as against it.
func TestPortMarksTheFixturesPureFunctions(test *testing.T) {
	test.Parallel()
	analysistest.Run(test, analysistest.TestData(), pushAnalyzer, "purity")
}

// Over the purity fixture, the original and the port's push export the same facts, on the fixture and on every package
// it imports, strings and time among them, whose listed functions the fixture's calls stand on. The demand
// differential holds the resolver's answers to the push's in turn.
func TestPortExportsTheOriginalsFacts(test *testing.T) {
	test.Parallel()
	parity.RequireIdenticalFacts(test, purity.Analyzer, pushAnalyzer, analysistest.TestData(), "purity")
}
