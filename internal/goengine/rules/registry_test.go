package rules

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
)

// registryProblems is every reason a list of rules could not be the registry: a rule that does not
// validate, or a name two rules share, which would make a finding's rule ambiguous and its config key
// configure two rules at once.
func registryProblems(registered []rule.Rule) []string {
	problems := []string{}
	seen := map[string]bool{}
	for _, registeredRule := range registered {
		if validateError := registeredRule.Validate(); validateError != nil {
			problems = append(problems, validateError.Error())
		}
		if seen[registeredRule.Name] {
			problems = append(problems, fmt.Sprintf("rule %q is registered twice", registeredRule.Name))
		}
		seen[registeredRule.Name] = true
	}
	return problems
}

// TestEveryRegisteredRuleValidatesAndHasItsOwnName holds the registry to well-formed, uniquely named
// rules. The registry is empty today, so the check also runs over a planted list, to prove it can fail.
func TestEveryRegisteredRuleValidatesAndHasItsOwnName(t *testing.T) {
	t.Parallel()
	for _, problem := range registryProblems(All()) {
		t.Error(problem)
	}

	planted := &analysis.Analyzer{
		Name: "planted",
		Doc:  "planted finds nothing; it exists so a rule under test has an analyzer",
		Run: func(pass *analysis.Pass) (any, error) {
			return nil, nil
		},
	}
	plantedRegistry := []rule.Rule{
		{Name: "cohere-go/printf", Origin: rule.OriginVet, UpstreamName: "printf", Analyzer: planted},
		{Name: "cohere-go/consistency-no-long-line", Origin: rule.OriginHouse, UpstreamName: "lll", Analyzer: planted},
		{Name: "cohere-go/printf", Origin: rule.OriginVet, UpstreamName: "printf", Analyzer: planted},
	}
	plantedProblems := registryProblems(plantedRegistry)
	expected := []string{"a house rule has no upstream", `rule "cohere-go/printf" is registered twice`}
	if len(plantedProblems) != len(expected) {
		t.Fatalf("the planted registry has %d problems, expected %d: %q", len(plantedProblems), len(expected), plantedProblems)
	}
	for index, saying := range expected {
		if !strings.Contains(plantedProblems[index], saying) {
			t.Errorf("planted problem %d is %q, expected it to say %q", index, plantedProblems[index], saying)
		}
	}
}
