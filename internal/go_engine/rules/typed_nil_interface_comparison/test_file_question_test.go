package typed_nil_interface_comparison_test

import (
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
	"github.com/system-inc/cohere/internal/go_engine/rules/typed_nil_interface_comparison"
)

// A comparison in a test file is never reported, so the rule never asks the nilness of its callee there (#k8e189q):
// over the testfile fixture, whose one comparison with a function's result stands in a test file, return_nil_ness
// judges no function. The finding for the value made from a concrete value in the same file shows the rule walked it.
func TestAComparisonInATestFileAsksNoNilness(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"testfile"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the testfile fixture: %v", loadError)
	}
	// The checker skips a package that does not type-check, which would ask nothing and read green.
	if len(loaded.Roots) == 0 {
		test.Fatal("loading the testfile fixture found no package")
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("the fixture %s does not load clean: %v", root.ID, root.Errors)
		}
	}
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{typed_nil_interface_comparison.Analyzer},
		loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the testfile fixture: %v", analyzeError)
	}
	reported := 0
	for _, action := range graph.Roots {
		if action.Analyzer != typed_nil_interface_comparison.Analyzer {
			continue
		}
		if action.Err != nil {
			test.Fatalf("running the rule on %s: %v", action.Package, action.Err)
		}
		reported += len(action.Diagnostics)
	}
	if reported != 1 {
		test.Errorf("the rule reported %d findings over the fixture, want 1, the value made from a concrete value",
			reported)
	}
	if computed := resolver.Counts(return_nil_ness.Kind).Computed.Load(); computed != 0 {
		test.Errorf("return_nil_ness judged %d functions for comparisons in a test file, want 0", computed)
	}
}
