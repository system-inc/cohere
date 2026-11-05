package rules

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"
	"golang.org/x/tools/go/analysis/suite/vet"

	"github.com/system-inc/cohere/internal/goengine/rule"
)

// registry is All() built once, for every test in the package to read.
var registry = All()

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

// registeredRuleNamed finds a rule in the registry by the name a finding prints.
func registeredRuleNamed(name string) (rule.Rule, bool) {
	for _, registeredRule := range registry {
		if registeredRule.Name == name {
			return registeredRule, true
		}
	}
	return rule.Rule{}, false
}

// TestEveryRegisteredRuleValidatesAndHasItsOwnName holds the registry to well-formed, uniquely named
// rules. The check also runs over a planted list, to prove it can fail.
func TestEveryRegisteredRuleValidatesAndHasItsOwnName(test *testing.T) {
	test.Parallel()
	for _, problem := range registryProblems(registry) {
		test.Error(problem)
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
		test.Fatalf(
			"the planted registry has %d problems, expected %d: %q",
			len(plantedProblems), len(expected), plantedProblems,
		)
	}
	for index, saying := range expected {
		if !strings.Contains(plantedProblems[index], saying) {
			test.Errorf("planted problem %d is %q, expected it to say %q", index, plantedProblems[index], saying)
		}
	}
}

// TestVetsSuiteOpensTheRegistryInItsOwnOrder holds the promise that `cohere` is never weaker than
// `go vet`: every pass vet runs is a rule, under vet's name, in vet's order, running vet's own analyzer.
func TestVetsSuiteOpensTheRegistryInItsOwnOrder(test *testing.T) {
	test.Parallel()
	if len(registry) < len(vet.Suite) {
		test.Fatalf("the registry has %d rules, fewer than vet's %d passes", len(registry), len(vet.Suite))
	}
	for index, analyzer := range vet.Suite {
		registeredRule := registry[index]
		if registeredRule.Analyzer != analyzer {
			test.Errorf("rule %d runs %q, expected vet's pass %q", index, registeredRule.Analyzer.Name, analyzer.Name)
			continue
		}
		if registeredRule.Name != rule.Namespace+analyzer.Name {
			test.Errorf("vet's pass %q is registered as %q", analyzer.Name, registeredRule.Name)
		}
		if registeredRule.Origin != rule.OriginVet {
			test.Errorf("vet's pass %q has origin %q", analyzer.Name, registeredRule.Origin)
		}
		if registeredRule.UpstreamName != analyzer.Name {
			test.Errorf("vet's pass %q names upstream %q", analyzer.Name, registeredRule.UpstreamName)
		}
	}
}

// TestNilnessAndUnusedwriteFollowVetsSuite holds the two x/tools passes vet leaves out to the registry,
// after vet's suite, as tools rules running their own analyzers.
func TestNilnessAndUnusedwriteFollowVetsSuite(test *testing.T) {
	test.Parallel()
	expected := []*analysis.Analyzer{nilness.Analyzer, unusedwrite.Analyzer}
	if len(registry) != len(vet.Suite)+len(expected) {
		test.Fatalf(
			"the registry has %d rules, expected vet's %d passes and %d more",
			len(registry), len(vet.Suite), len(expected),
		)
	}
	for offset, analyzer := range expected {
		index := len(vet.Suite) + offset
		registeredRule := registry[index]
		if registeredRule.Analyzer != analyzer {
			test.Errorf("rule %d runs %q, expected %q", index, registeredRule.Analyzer.Name, analyzer.Name)
			continue
		}
		if registeredRule.Name != rule.Namespace+analyzer.Name || registeredRule.UpstreamName != analyzer.Name {
			test.Errorf(
				"%q is registered as %q with upstream %q",
				analyzer.Name, registeredRule.Name, registeredRule.UpstreamName,
			)
		}
		if registeredRule.Origin != rule.OriginTools {
			test.Errorf("%q has origin %q, expected %q", analyzer.Name, registeredRule.Origin, rule.OriginTools)
		}
		if !registeredRule.TypeAware {
			test.Errorf("%q reads SSA built from type information, so it is type aware", analyzer.Name)
		}
	}
}

// TestTheRegisteredAnalyzersValidateTogether runs x/tools' own check over the whole set, which refuses a
// cycle in the Requires graph and a fact type an analyzer uses without declaring, either of which would
// fail the engine's run rather than one rule.
func TestTheRegisteredAnalyzersValidateTogether(test *testing.T) {
	test.Parallel()
	analyzers := []*analysis.Analyzer{}
	for _, registeredRule := range registry {
		analyzers = append(analyzers, registeredRule.Analyzer)
	}
	if validateError := analysis.Validate(analyzers); validateError != nil {
		test.Error(validateError)
	}
}

// TestTypeAwareFollowsTheSyntaxOnlyTable proves the table is read the right way round, with one pass
// from each side of it, and that no entry names a pass the registry does not hold, which would mean a
// rename upstream left a decision about nothing.
func TestTypeAwareFollowsTheSyntaxOnlyTable(test *testing.T) {
	test.Parallel()
	expected := map[string]bool{"cohere-go/printf": true, "cohere-go/buildtag": false}
	for name, typeAware := range expected {
		registeredRule, found := registeredRuleNamed(name)
		if !found {
			test.Errorf("%s is not registered", name)
			continue
		}
		if registeredRule.TypeAware != typeAware {
			test.Errorf("%s has TypeAware %t, expected %t", name, registeredRule.TypeAware, typeAware)
		}
	}
	for upstreamName := range syntaxOnly {
		if _, found := registeredRuleNamed(rule.Namespace + upstreamName); !found {
			test.Errorf("the syntax-only table names %q, which is not a registered rule", upstreamName)
		}
	}
}
