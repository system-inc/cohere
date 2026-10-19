package registry

import (
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/gotypes"
)

// ruleTypes is the export data of every package outside cohere that the option walks import: the standard
// library, the TypeScript checker and its shims, the modules they need. The walks read cohere's own packages
// from source and these from this product, which is built where building is allowed and never here: a walk
// that compiled ran `go list -export`, which a runner of prebuilt tests refuses (#qnnnkbf).
var ruleTypes = gotypes.Product{
	Name:      "lint rules",
	Test:      "TestProduct_RuleTypes (internal/lint/registry)",
	Directory: "../../..",
	Patterns: []string{
		"./internal/lint/rules/...",
		"./internal/lint/registry/testdata/optiondecoding",
		"./internal/lint/registry/testdata/optionreads",
	},
}

// Not parallel: it builds the product the option walks read, so it finishes before they resume. A runner of
// prebuilt tests skips it and finds the product Workshop built.
func TestProduct_RuleTypes(t *testing.T) {
	exports, err := ruleTypes.Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the lint rules types product is %s", exports.Key()[:12])
}

// ruleExports is ruleTypes found once per test binary.
var ruleExports = sync.OnceValues(ruleTypes.Find)

// loadRules loads the packages matching patterns from moduleRoot with their syntax and types, and fails
// the test on any error in any package read from source, naming each, so a walk never reads a partial tree.
func loadRules(t *testing.T, moduleRoot string, patterns ...string) []*gotypes.Package {
	t.Helper()
	exports, err := ruleExports()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := gotypes.Load(gotypes.Config{Directory: moduleRoot, Exports: exports}, patterns...)
	if err != nil {
		t.Fatalf("loading %v: %v", patterns, err)
	}
	if problems := loaded.ErrorsAnywhere(); len(problems) > 0 {
		for _, problem := range problems {
			t.Log(problem)
		}
		t.Fatalf("%d errors loading %v, so the walk would read a partial tree", len(problems), patterns)
	}
	return loaded.Roots
}
