package check

import (
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/rules/standing_facts"
)

// pushedFactTypes are the fact types a run's analyzers export, which the checker pushes from a package to its
// importers and a build compares with the host's (#wxx4ar6), each by the name the findings cache gives it
// (factTypeName): ctrlflow's which functions never return, the deprecation facts staticcheck's fact_deprecated
// exports, and printf's which functions wrap a print and how. The facts' reach compares the first of each function
// itself (#5vbs1np), so a fourth is read, for what a build must compare of it, before it joins them.
var pushedFactTypes = []string{
	"golang.org/x/tools/go/analysis/passes/ctrlflow.noReturn",
	"honnef.co/go/tools/analysis/facts/deprecated.IsDeprecated",
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf.isWrapper",
}

// pushedFactTypesOf is the name of every fact type among roots and everything they require, directly or not, sorted.
func pushedFactTypesOf(roots []*analysis.Analyzer) []string {
	var found []string
	seen := map[*analysis.Analyzer]bool{}
	var visit func(analyzer *analysis.Analyzer)
	visit = func(analyzer *analysis.Analyzer) {
		if seen[analyzer] {
			return
		}
		seen[analyzer] = true
		for _, fact := range analyzer.FactTypes {
			if name := factTypeName(reflect.TypeOf(fact)); !slices.Contains(found, name) {
				found = append(found, name)
			}
		}
		for _, required := range analyzer.Requires {
			visit(required)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	slices.Sort(found)
	return found
}

// A run's pushed fact types are exactly the three read for the facts' reach (#wxx4ar6): every rule's analyzer and
// what it requires, and the facts' reach's own, which Run roots beside the rules (Options.FactReachSettle).
func TestARunPushesOnlyTheThreeKnownFactTypes(test *testing.T) {
	test.Parallel()

	roots := []*analysis.Analyzer{standing_facts.Analyzer}
	for _, enabled := range rules.All() {
		roots = append(roots, enabled.Analyzer)
	}
	want := slices.Sorted(slices.Values(pushedFactTypes))
	if found := pushedFactTypesOf(roots); !slices.Equal(found, want) {
		test.Errorf("a run pushes the fact types\n%q\nwant pushedFactTypes\n%q\nRead a new one as the facts' reach "+
			"must, whether a build compares it and whether it is about its own package's objects alone, before "+
			"adding it", found, want)
	}
}
