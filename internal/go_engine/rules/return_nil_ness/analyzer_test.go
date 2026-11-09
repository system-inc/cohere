package return_nil_ness

import (
	"testing"

	"honnef.co/go/tools/analysis/facts/nilness"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// The nilness fixture marks every function the port exports a fact on, and only those, each `// want` naming one with
// the nilness of its results, so what the port judges is pinned apart from the original as well as against it.
func TestPortMarksTheFixturesNilness(test *testing.T) {
	test.Parallel()
	analysistest.Run(test, analysistest.TestData(), pushAnalyzer, "nilness")
}

// Over the nilness fixture, the original and the port export the same facts, on the fixture and on every package it
// imports, errors and fmt among them, whose facts the fixture's calls stand on.
func TestPortExportsTheOriginalsFacts(test *testing.T) {
	test.Parallel()
	parity.RequireIdenticalFacts(test, nilness.Analysis, pushAnalyzer, analysistest.TestData(), "nilness")
}
