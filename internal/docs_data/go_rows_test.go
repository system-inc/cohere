package docs_data

import (
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	goenginerule "github.com/system-inc/cohere/internal/go_engine/rule"
)

// placeholderAnalyzer stands in for a rule's analyzer: the rows read a rule's naming and provenance, never
// what its analyzer does, so any analyzer that Validate accepts will do.
var placeholderAnalyzer = &analysis.Analyzer{
	Name: "placeholder",
	Doc:  "placeholder stands in for a rule's analyzer in the docs rows' tests",
	Run:  func(*analysis.Pass) (any, error) { return nil, nil },
}

// TestAGoRuleHasARowInRulesJson: a Go house rule and a ported vet rule each reach rules.json through Build,
// under the Go language, the cohere-go namespace and their own origin, with the house rule's category
// split by the naming scheme and the port's upstream name kept.
func TestAGoRuleHasARowInRulesJson(test *testing.T) {
	test.Parallel()
	inputs := sourceInputs(test)
	inputs.GoRules = []goenginerule.Rule{
		{
			Name:     "cohere-go/performance-require-parallel-test",
			Origin:   goenginerule.OriginHouse,
			Analyzer: placeholderAnalyzer,
		},
		{
			Name:         "cohere-go/printf",
			Origin:       goenginerule.OriginVet,
			UpstreamName: "printf",
			TypeAware:    true,
			Analyzer:     placeholderAnalyzer,
		},
	}
	var rules Rules
	if decodeError := json.Unmarshal(built(test, inputs)[RulesPath], &rules); decodeError != nil {
		test.Fatal(decodeError)
	}
	rowsByName := map[string]RuleRow{}
	for _, row := range rules.Rules {
		if row.Language == "Go" {
			rowsByName[row.Name] = row
		}
	}
	if len(rowsByName) != 2 {
		test.Fatalf("rules.json has %d Go rows, expected the 2 registered: %v", len(rowsByName), rowsByName)
	}
	house := rowsByName["cohere-go/performance-require-parallel-test"]
	if house.Namespace != "cohere-go" || house.Origin != "house" || house.Category != "performance" ||
		house.UpstreamName != "" {
		test.Errorf("the house rule's row is %+v", house)
	}
	ported := rowsByName["cohere-go/printf"]
	if ported.Namespace != "cohere-go" || ported.Origin != "vet" || ported.UpstreamName != "printf" ||
		ported.Category != "" || !ported.TypeAware {
		test.Errorf("the ported rule's row is %+v", ported)
	}
}

// TestAMalformedGoRuleRefusesTheBuild: a house rule outside the naming scheme, and a rule the registry's
// own Validate refuses, each stop the docs from publishing rather than reaching the site.
func TestAMalformedGoRuleRefusesTheBuild(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name     string
		goRule   goenginerule.Rule
		expected string
	}{
		{
			name: "a house name outside the scheme",
			goRule: goenginerule.Rule{
				Name:     "cohere-go/nothing-like-a-category",
				Origin:   goenginerule.OriginHouse,
				Analyzer: placeholderAnalyzer,
			},
			expected: "cohere-go/nothing-like-a-category",
		},
		{
			name: "a port with no upstream name",
			goRule: goenginerule.Rule{
				Name:     "cohere-go/printf",
				Origin:   goenginerule.OriginVet,
				Analyzer: placeholderAnalyzer,
			},
			expected: "names no upstream rule",
		},
		{
			name: "a rule with no analyzer",
			goRule: goenginerule.Rule{
				Name:         "cohere-go/printf",
				Origin:       goenginerule.OriginVet,
				UpstreamName: "printf",
			},
			expected: "has no analyzer",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			_, buildError := buildGoRows([]goenginerule.Rule{testCase.goRule})
			if buildError == nil || !strings.Contains(buildError.Error(), testCase.expected) {
				test.Fatalf("buildGoRows refused with %v, expected an error naming %q", buildError, testCase.expected)
			}
		})
	}
}
