package function_purity

import (
	"testing"

	"honnef.co/go/tools/analysis/facts/purity"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// The purity fixture marks every function the port holds pure, and only those, each `// want` naming one, so what the
// port judges is pinned apart from the original as well as against it.
func TestPortMarksTheFixturesPureFunctions(test *testing.T) {
	test.Parallel()
	analysistest.Run(test, analysistest.TestData(), Analyzer, "purity")
}

// Over the purity fixture, the original and the port export the same facts, on the fixture and on every package it
// imports, strings and time among them, whose listed functions the fixture's calls stand on.
func TestPortExportsTheOriginalsFacts(test *testing.T) {
	test.Parallel()
	parity.RequireIdenticalFacts(test, purity.Analyzer, Analyzer, analysistest.TestData(), "purity")
}
