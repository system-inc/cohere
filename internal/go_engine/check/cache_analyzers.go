package check

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// cacheAnalyzers is what a run with the cache on knows of its analyzers: the slot of each one an entry holds, and
// each one's fact types, by factTypeName.
type cacheAnalyzers struct {
	slots     map[*analysis.Analyzer]string
	factTypes map[*analysis.Analyzer]map[string]reflect.Type

	// facts are the analyzers that learn facts, among the rules' analyzers and everything they require: a package
	// any action reaches may be asked for these.
	facts []*analysis.Analyzer

	// rules are the rules' analyzers and the report analyzers, which a package Analyze was given is asked for.
	rules []*analysis.Analyzer

	// reports are the report analyzers (see Options.Report), whose result an entry holds.
	reports map[*analysis.Analyzer]bool
}

// newCacheAnalyzers reads the analyzers an entry holds: every rule's, and every analyzer below them that learns
// facts, below them as the checker's graph has them, where a dispatcher's requirement for its listeners is kept only
// when an analyzer that requires the dispatcher requires it too (see checker.Options.ForDependents). A fact analyzer
// only a left-out rule's listener reads never runs, so an entry could never hold it, and counting it would make every
// entry fall short of what the run asks. A rule's analyzer is held under the rule's name, which is unique. Any other
// is held under its name and its fact types, since an analyzer's name alone is not: staticcheck's nilness learns
// facts, and vet's nilness is a rule. Two analyzers that would still share a slot refuse the cache for the run. A
// report analyzer is asked of a package to analyze as a rule is, under its name, and its entry holds its result.
func newCacheAnalyzers(
	ruleOf map[*analysis.Analyzer]rule.Rule,
	analyzers []*analysis.Analyzer,
	reports []*analysis.Analyzer,
) (*cacheAnalyzers, error) {
	asked := append(slices.Clone(analyzers), reports...)
	known := &cacheAnalyzers{
		slots:     map[*analysis.Analyzer]string{},
		factTypes: map[*analysis.Analyzer]map[string]reflect.Type{},
		rules:     asked,
		reports:   map[*analysis.Analyzer]bool{},
	}
	for _, report := range reports {
		known.reports[report] = true
	}
	requires := requiredInRun(asked)
	seen := map[*analysis.Analyzer]bool{}
	var visit func(analyzer *analysis.Analyzer) error
	visit = func(analyzer *analysis.Analyzer) error {
		if seen[analyzer] {
			return nil
		}
		seen[analyzer] = true
		if len(analyzer.FactTypes) > 0 {
			known.facts = append(known.facts, analyzer)
			byTypeName := map[string]reflect.Type{}
			for _, fact := range analyzer.FactTypes {
				byTypeName[factTypeName(reflect.TypeOf(fact))] = reflect.TypeOf(fact)
			}
			known.factTypes[analyzer] = byTypeName
		}
		for _, required := range requires(analyzer) {
			if requiredError := visit(required); requiredError != nil {
				return requiredError
			}
		}
		return nil
	}
	for _, analyzer := range asked {
		if visitError := visit(analyzer); visitError != nil {
			return nil, visitError
		}
	}
	held := map[string]*analysis.Analyzer{}
	for _, stored := range append(append([]*analysis.Analyzer{}, asked...), known.facts...) {
		slot := stored.Name + " " + strings.Join(slices.Sorted(maps.Keys(known.factTypes[stored])), " ")
		if storedRule, isRule := ruleOf[stored]; isRule {
			slot = storedRule.Name
		}
		if earlier, taken := held[slot]; taken && earlier != stored {
			return nil, fmt.Errorf("caching the analysis: two analyzers would be held as %s, so an entry could not "+
				"say which one it holds", slot)
		}
		held[slot] = stored
		known.slots[stored] = slot
	}
	return known, nil
}

// requirersBelow is, for every analyzer analyzers require, directly or not, those among them and below them that
// require it, every edge followed, as the checker reads its graph before it drops any edge.
func requirersBelow(analyzers []*analysis.Analyzer) map[*analysis.Analyzer][]*analysis.Analyzer {
	requirers := map[*analysis.Analyzer][]*analysis.Analyzer{}
	seen := map[*analysis.Analyzer]bool{}
	var visit func(analyzer *analysis.Analyzer)
	visit = func(analyzer *analysis.Analyzer) {
		if seen[analyzer] {
			return
		}
		seen[analyzer] = true
		for _, required := range analyzer.Requires {
			requirers[required] = append(requirers[required], analyzer)
			visit(required)
		}
	}
	for _, analyzer := range analyzers {
		visit(analyzer)
	}
	return requirers
}

// slot is where an entry holds analyzer's analysis, and false for an analyzer it does not hold: one that is no
// rule's and learns no facts.
func (known *cacheAnalyzers) slot(analyzer *analysis.Analyzer) (string, bool) {
	slot, held := known.slots[analyzer]
	return slot, held
}
