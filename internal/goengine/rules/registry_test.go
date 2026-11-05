package rules

import (
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"
	"golang.org/x/tools/go/analysis/suite/vet"
	"honnef.co/go/tools/simple"
	"honnef.co/go/tools/staticcheck"
	"honnef.co/go/tools/stylecheck"

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

// expectBlock holds the registry, from offset on, to one rule per analyzer in order, each named for its
// analyzer, of the given origin, and running that very analyzer rather than a copy that could drift.
func expectBlock(test *testing.T, offset int, analyzers []*analysis.Analyzer, origin rule.Origin) {
	test.Helper()
	if len(registry) < offset+len(analyzers) {
		test.Fatalf(
			"the registry has %d rules, too few to hold %d %s rules from rule %d",
			len(registry), len(analyzers), origin, offset,
		)
	}
	for position, analyzer := range analyzers {
		index := offset + position
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
		if registeredRule.Origin != origin {
			test.Errorf("%q has origin %q, expected %q", analyzer.Name, registeredRule.Origin, origin)
		}
	}
}

// ruledStylecheck is the ST checks ruled on, written out here rather than read from the registry's own
// table, so a check added to or dropped from that table by mistake fails the test.
var ruledStylecheck = []string{
	"ST1001", "ST1006", "ST1008", "ST1011", "ST1012", "ST1013", "ST1016", "ST1017", "ST1018", "ST1019",
}

// staticcheckAnalyzers is every SA and S check staticcheck runs by default, in its order, which is the order
// they follow the x/tools passes in.
func staticcheckAnalyzers() []*analysis.Analyzer {
	analyzers := []*analysis.Analyzer{}
	for _, wrapped := range slices.Concat(staticcheck.Analyzers, simple.Analyzers) {
		if wrapped.Doc != nil && wrapped.Doc.NonDefault || duplicatesOfVet[wrapped.Analyzer.Name] != "" {
			continue
		}
		analyzers = append(analyzers, wrapped.Analyzer)
	}
	return analyzers
}

// stylecheckAnalyzers is the ruled ST checks, in stylecheck's order, which is the order they follow the SA
// and S checks in.
func stylecheckAnalyzers() []*analysis.Analyzer {
	analyzers := []*analysis.Analyzer{}
	for _, wrapped := range stylecheck.Analyzers {
		if slices.Contains(ruledStylecheck, wrapped.Analyzer.Name) {
			analyzers = append(analyzers, wrapped.Analyzer)
		}
	}
	return analyzers
}

// TestVetsSuiteOpensTheRegistryInItsOwnOrder holds the promise that `cohere` is never weaker than
// `go vet`: every pass vet runs is a rule, under vet's name, in vet's order, running vet's own analyzer.
func TestVetsSuiteOpensTheRegistryInItsOwnOrder(test *testing.T) {
	test.Parallel()
	expectBlock(test, 0, vet.Suite, rule.OriginVet)
}

// TestNilnessAndUnusedwriteFollowVetsSuite holds the two x/tools passes vet leaves out to the registry,
// after vet's suite, as tools rules running their own analyzers.
func TestNilnessAndUnusedwriteFollowVetsSuite(test *testing.T) {
	test.Parallel()
	expected := []*analysis.Analyzer{nilness.Analyzer, unusedwrite.Analyzer}
	expectBlock(test, len(vet.Suite), expected, rule.OriginTools)
	for _, analyzer := range expected {
		registeredRule, found := registeredRuleNamed(rule.Namespace + analyzer.Name)
		if found && !registeredRule.TypeAware {
			test.Errorf("%q reads SSA built from type information, so it is type aware", analyzer.Name)
		}
	}
}

// TestStaticcheckFollowsTheToolsPasses holds the registry, after the x/tools passes, to every default SA
// and S check, then the ruled ST checks, then U1000 through the analyzer that reports it, then the house
// rules, and nothing else, which also keeps out every QF check, since a quick fix is not a finding.
func TestStaticcheckFollowsTheToolsPasses(test *testing.T) {
	test.Parallel()
	offset := len(vet.Suite) + 2
	checks := staticcheckAnalyzers()
	expectBlock(test, offset, checks, rule.OriginStaticcheck)
	styleChecks := stylecheckAnalyzers()
	expectBlock(test, offset+len(checks), styleChecks, rule.OriginStaticcheck)

	unusedIndex := offset + len(checks) + len(styleChecks)
	expectedCount := unusedIndex + 1 + len(House())
	if len(registry) != expectedCount {
		test.Fatalf("the registry has %d rules, expected %d", len(registry), expectedCount)
	}
	expectedUnused := rule.Rule{
		Name:         "cohere-go/U1000",
		Origin:       rule.OriginStaticcheck,
		UpstreamName: "U1000",
		TypeAware:    true,
		Analyzer:     unusedReport,
	}
	// reflect.DeepEqual because ConfiguredBy is a slice, which leaves rule.Rule without ==.
	if unusedRule := registry[unusedIndex]; !reflect.DeepEqual(unusedRule, expectedUnused) {
		test.Errorf("rule %d is %+v, expected %+v", unusedIndex, unusedRule, expectedUnused)
	}
	for index, houseRule := range House() {
		registeredRule := registry[unusedIndex+1+index]
		if registeredRule.Name != houseRule.Name || registeredRule.Origin != rule.OriginHouse {
			test.Errorf("rule %d is %q (%s), expected the house rule %q", unusedIndex+1+index, registeredRule.Name,
				registeredRule.Origin, houseRule.Name)
		}
	}
}

// TestExactlyTheRuledStylecheckChecksAreRegistered holds the ST rules to the ruling, both ways: every ruled
// check is registered, and no other ST check is, the ruled-off ST1003 and ST1005 above all.
func TestExactlyTheRuledStylecheckChecksAreRegistered(test *testing.T) {
	test.Parallel()
	registeredStylecheck := []string{}
	for _, registeredRule := range registry {
		if strings.HasPrefix(registeredRule.UpstreamName, "ST") {
			registeredStylecheck = append(registeredStylecheck, registeredRule.UpstreamName)
		}
	}
	slices.Sort(registeredStylecheck)
	if !slices.Equal(registeredStylecheck, ruledStylecheck) {
		test.Errorf("the registered ST checks are %q, expected exactly %q", registeredStylecheck, ruledStylecheck)
	}
}

// TestNoDefaultOffSAOrSCheckIsRegistered holds the registry to staticcheck's own defaults for SA and S: a
// check its documentation marks NonDefault stays off until it is ruled on. SA9003 is named, so the test
// cannot pass by staticcheck marking nothing.
func TestNoDefaultOffSAOrSCheckIsRegistered(test *testing.T) {
	test.Parallel()
	nonDefault := []string{}
	for _, wrapped := range slices.Concat(staticcheck.Analyzers, simple.Analyzers) {
		if wrapped.Doc != nil && wrapped.Doc.NonDefault {
			nonDefault = append(nonDefault, wrapped.Analyzer.Name)
		}
	}
	if !slices.Contains(nonDefault, "SA9003") {
		test.Errorf("staticcheck's default-off SA and S checks are %q, which no longer holds SA9003", nonDefault)
	}
	for _, code := range nonDefault {
		if _, found := registeredRuleNamed(rule.Namespace + code); found {
			test.Errorf("%s is registered, and staticcheck leaves it off by default", code)
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
	expected := map[string]bool{
		"cohere-go/printf":   true,
		"cohere-go/buildtag": false,
		"cohere-go/SA4006":   true,
		"cohere-go/SA9009":   false,
		"cohere-go/ST1008":   true,
		"cohere-go/ST1018":   false,
	}
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

// TestNoCheckDuplicatesAVetPass holds the registry to one rule per judgment: a staticcheck check that judges
// what a vet pass judges is left out while that pass is registered. SA5009 is named, so the test cannot pass
// by the table emptying.
func TestNoCheckDuplicatesAVetPass(test *testing.T) {
	test.Parallel()
	if duplicatesOfVet["SA5009"] != "printf" {
		test.Errorf("SA5009 is no longer recorded as printf's twin: %q", duplicatesOfVet)
	}
	for code, vetPass := range duplicatesOfVet {
		if _, found := registeredRuleNamed(rule.Namespace + vetPass); !found {
			test.Errorf("%s is left out as %s's twin, and %s is not registered", code, vetPass, vetPass)
		}
		if _, found := registeredRuleNamed(rule.Namespace + code); found {
			test.Errorf("%s is registered beside %s, so one mistake would be two findings", code, vetPass)
		}
	}
}

// sharedReaders is the packages under rules/ that several rules read through and that hold no rule of their own:
// marker reads the reasoned marker comments, inertvalue judges a value that cannot carry syntax, markertype judges a
// sum type's variant whose methods only mark it, and served finds the types a file serves for the two rules that
// count them.
var sharedReaders = map[string]bool{"marker": true, "inertvalue": true, "markertype": true, "served": true}

// TestEveryRulePackageIsInHouse holds the registry to the rule packages on disk: each directory under rules/, but
// testdata and the shared readers, is one house rule named for it, and House lists it. A rule package nobody
// registered would build, pass its own fixtures and run on nothing.
func TestEveryRulePackageIsInHouse(test *testing.T) {
	test.Parallel()
	entries, readError := os.ReadDir(".")
	if readError != nil {
		test.Fatalf("reading the rules directory: %v", readError)
	}
	registeredHouse := map[string]bool{}
	for _, houseRule := range House() {
		registeredHouse[houseRule.Name] = true
	}
	packages := 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "testdata" || sharedReaders[entry.Name()] {
			continue
		}
		packages++
		name := rule.Namespace + strings.ReplaceAll(entry.Name(), "_", "-")
		if !registeredHouse[name] {
			test.Errorf("rules/%s holds a rule, and House does not list %s", entry.Name(), name)
		}
	}
	if packages != len(House()) {
		test.Errorf("rules/ holds %d rule packages, and House lists %d rules", packages, len(House()))
	}
}

// ST1015 judges a misplaced default the way the house rule consistency-require-default-last does, so it stays out
// of the registry while that rule is in the house, and a misplaced default is one finding, not two.
func TestStylecheckLeavesTheDefaultClauseToTheHouseRule(test *testing.T) {
	test.Parallel()
	for _, registered := range All() {
		if registered.Name == rule.Namespace+"ST1015" {
			test.Fatalf("%s is registered beside cohere-go/consistency-require-default-last, which judges the same "+
				"default", registered.Name)
		}
	}
}
