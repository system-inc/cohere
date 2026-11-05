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
func TestValidateAcceptsAWellFormedRule(t *testing.T) {
	t.Parallel()
	cases := []Rule{
		{Name: "cohere-go/performance-require-parallel-test", Origin: OriginHouse, TypeAware: true, Analyzer: plantedAnalyzer},
		{Name: "cohere-go/printf", Origin: OriginVet, UpstreamName: "printf", TypeAware: true, Analyzer: plantedAnalyzer},
		{Name: "cohere-go/SA4006", Origin: OriginStaticcheck, UpstreamName: "SA4006", Analyzer: plantedAnalyzer},
		{Name: "cohere-go/nilness", Origin: OriginTools, UpstreamName: "nilness", TypeAware: true, Analyzer: plantedAnalyzer},
	}
	for _, testCase := range cases {
		if validateError := testCase.Validate(); validateError != nil {
			t.Errorf("%s: refused a well-formed rule: %v", testCase.Name, validateError)
		}
	}
}

// TestValidateRefusesEachMalformedRule plants one defect per case and expects the refusal that names it.
func TestValidateRefusesEachMalformedRule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		rule   Rule
		saying string
	}{
		{"an empty name", Rule{Origin: OriginHouse, Analyzer: plantedAnalyzer}, "has no name"},
		{"a name outside the namespace", Rule{Name: "cohere-swift/force-unwrapping", Origin: OriginHouse, Analyzer: plantedAnalyzer}, "does not start with"},
		{"a bare upstream name", Rule{Name: "printf", Origin: OriginVet, UpstreamName: "printf", Analyzer: plantedAnalyzer}, "does not start with"},
		{"the namespace alone", Rule{Name: "cohere-go/", Origin: OriginHouse, Analyzer: plantedAnalyzer}, "the namespace alone"},
		{"no analyzer", Rule{Name: "cohere-go/consistency-no-long-line", Origin: OriginHouse}, "has no analyzer"},
		{"no origin", Rule{Name: "cohere-go/consistency-no-long-line", Analyzer: plantedAnalyzer}, "which is none of"},
		{"an unknown origin", Rule{Name: "cohere-go/force-unwrapping", Origin: "swiftlint", UpstreamName: "force_unwrapping", Analyzer: plantedAnalyzer}, "which is none of"},
		{"an upstream name on a house rule", Rule{Name: "cohere-go/consistency-no-long-line", Origin: OriginHouse, UpstreamName: "lll", Analyzer: plantedAnalyzer}, "a house rule has no upstream"},
		{"a vet rule with no upstream name", Rule{Name: "cohere-go/printf", Origin: OriginVet, Analyzer: plantedAnalyzer}, "names no upstream rule"},
		{"a staticcheck rule with no upstream name", Rule{Name: "cohere-go/SA4006", Origin: OriginStaticcheck, Analyzer: plantedAnalyzer}, "names no upstream rule"},
		{"a tools rule with no upstream name", Rule{Name: "cohere-go/nilness", Origin: OriginTools, Analyzer: plantedAnalyzer}, "names no upstream rule"},
	}
	for _, testCase := range cases {
		validateError := testCase.rule.Validate()
		if validateError == nil {
			t.Errorf("%s: accepted", testCase.name)
			continue
		}
		if !strings.Contains(validateError.Error(), testCase.saying) {
			t.Errorf("%s: refused with %q, expected it to say %q", testCase.name, validateError, testCase.saying)
		}
	}
}
