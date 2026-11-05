package rule

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// plantedAnalyzer is an analyzer that finds nothing, for rules that need one to be valid.
var plantedAnalyzer = &analysis.Analyzer{
	Name: "planted",
	Doc:  "planted finds nothing; it exists so a rule under test has an analyzer",
	Run: func(pass *analysis.Pass) (any, error) {
		return nil, nil
	},
}

// TestValidateAcceptsAWellFormedRule is the control for the refusals below: a house rule and each kind of
// ported rule, written the way the registry will hold them.
func TestValidateAcceptsAWellFormedRule(test *testing.T) {
	test.Parallel()
	cases := []Rule{
		{
			Name:      "cohere-go/performance-require-parallel-test",
			Origin:    OriginHouse,
			TypeAware: true,
			Analyzer:  plantedAnalyzer,
		},
		{
			Name:         "cohere-go/printf",
			Origin:       OriginVet,
			UpstreamName: "printf",
			TypeAware:    true,
			Analyzer:     plantedAnalyzer,
		},
		{Name: "cohere-go/SA4006", Origin: OriginStaticcheck, UpstreamName: "SA4006", Analyzer: plantedAnalyzer},
		{
			Name:         "cohere-go/nilness",
			Origin:       OriginTools,
			UpstreamName: "nilness",
			TypeAware:    true,
			Analyzer:     plantedAnalyzer,
		},
	}
	for _, testCase := range cases {
		if validateError := testCase.Validate(); validateError != nil {
			test.Errorf("%s: refused a well-formed rule: %v", testCase.Name, validateError)
		}
	}
}

// TestValidateRefusesEachMalformedRule plants one defect per case and expects the refusal that names it.
func TestValidateRefusesEachMalformedRule(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name   string
		rule   Rule
		saying string
	}{
		{
			name:   "an empty name",
			rule:   Rule{Origin: OriginHouse, Analyzer: plantedAnalyzer},
			saying: "has no name",
		},
		{
			name:   "a name outside the namespace",
			rule:   Rule{Name: "cohere-swift/force-unwrapping", Origin: OriginHouse, Analyzer: plantedAnalyzer},
			saying: "does not start with",
		},
		{
			name:   "a bare upstream name",
			rule:   Rule{Name: "printf", Origin: OriginVet, UpstreamName: "printf", Analyzer: plantedAnalyzer},
			saying: "does not start with",
		},
		{
			name:   "the namespace alone",
			rule:   Rule{Name: "cohere-go/", Origin: OriginHouse, Analyzer: plantedAnalyzer},
			saying: "the namespace alone",
		},
		{
			name:   "no analyzer",
			rule:   Rule{Name: "cohere-go/consistency-no-long-line", Origin: OriginHouse},
			saying: "has no analyzer",
		},
		{
			name:   "no origin",
			rule:   Rule{Name: "cohere-go/consistency-no-long-line", Analyzer: plantedAnalyzer},
			saying: "which is none of",
		},
		{
			name: "an unknown origin",
			rule: Rule{
				Name:         "cohere-go/force-unwrapping",
				Origin:       "swiftlint",
				UpstreamName: "force_unwrapping",
				Analyzer:     plantedAnalyzer,
			},
			saying: "which is none of",
		},
		{
			name: "an upstream name on a house rule",
			rule: Rule{
				Name:         "cohere-go/consistency-no-long-line",
				Origin:       OriginHouse,
				UpstreamName: "lll",
				Analyzer:     plantedAnalyzer,
			},
			saying: "a house rule has no upstream",
		},
		{
			name:   "a vet rule with no upstream name",
			rule:   Rule{Name: "cohere-go/printf", Origin: OriginVet, Analyzer: plantedAnalyzer},
			saying: "names no upstream rule",
		},
		{
			name:   "a staticcheck rule with no upstream name",
			rule:   Rule{Name: "cohere-go/SA4006", Origin: OriginStaticcheck, Analyzer: plantedAnalyzer},
			saying: "names no upstream rule",
		},
		{
			name:   "a tools rule with no upstream name",
			rule:   Rule{Name: "cohere-go/nilness", Origin: OriginTools, Analyzer: plantedAnalyzer},
			saying: "names no upstream rule",
		},
	}
	for _, testCase := range cases {
		validateError := testCase.rule.Validate()
		if validateError == nil {
			test.Errorf("%s: accepted", testCase.name)
			continue
		}
		if !strings.Contains(validateError.Error(), testCase.saying) {
			test.Errorf("%s: refused with %q, expected it to say %q", testCase.name, validateError, testCase.saying)
		}
	}
}
