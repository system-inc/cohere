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

	// rules are the rules' analyzers, which a package Analyze was given is asked for.
	rules []*analysis.Analyzer
}

// newCacheAnalyzers reads the analyzers an entry holds: every rule's, and every analyzer below them that learns
// facts. A rule's analyzer is held under the rule's name, which is unique. Any other is held under its name and its
// fact types, since an analyzer's name alone is not: staticcheck's nilness learns facts, and vet's nilness is a
// rule. Two analyzers that would still share a slot refuse the cache for the run.
func newCacheAnalyzers(
	ruleOf map[*analysis.Analyzer]rule.Rule,
	analyzers []*analysis.Analyzer,
) (*cacheAnalyzers, error) {
	known := &cacheAnalyzers{
		slots:     map[*analysis.Analyzer]string{},
		factTypes: map[*analysis.Analyzer]map[string]reflect.Type{},
		rules:     analyzers,
	}
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
		for _, required := range analyzer.Requires {
			if requiredError := visit(required); requiredError != nil {
				return requiredError
			}
		}
		return nil
	}
	for _, analyzer := range analyzers {
		if visitError := visit(analyzer); visitError != nil {
			return nil, visitError
		}
	}
	held := map[string]*analysis.Analyzer{}
	for _, stored := range append(append([]*analysis.Analyzer{}, analyzers...), known.facts...) {
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

// slot is where an entry holds analyzer's analysis, and false for an analyzer it does not hold: one that is no
// rule's and learns no facts.
func (known *cacheAnalyzers) slot(analyzer *analysis.Analyzer) (string, bool) {
	slot, held := known.slots[analyzer]
	return slot, held
}
