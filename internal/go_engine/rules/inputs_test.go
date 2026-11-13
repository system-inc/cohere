package rules

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// mainRuleSize is unsafe.Sizeof(rule.Rule{}) on main before block 3 (#h350vqr) gave it Inputs and FullKind, on a
// 64-bit target: three strings, a bool, the analyzer, ConfiguredBy, the listener and MergeIfAll, 104 bytes.
const mainRuleSize = 104

// mainRuleFields are rule.Rule's fields on main, in order. The size alone would let a bool back in: MergeIfAll ends the
// struct in seven bytes of padding, which a bool after it fills without changing the size, so the fields are held too.
var mainRuleFields = []string{"Name", "Origin", "UpstreamName", "TypeAware", "Analyzer", "ConfiguredBy", "Listener",
	"MergeIfAll"}

// A rule.Rule is main's, byte for byte (#qr19hvc): every plain run copies the value through the registry, the settings
// and the checker, and two fields more on it, which no plain run reads, leaned a long Shackler pair toward one more
// garbage collection. A rule's declaration lives in the side table (inputs.go) instead, so a field added back to
// rule.Rule for it, or for anything else a plain run does not read, fails here, where the reason is: by its size, or,
// for a field that fits in the padding, by its fields.
func TestARuleIsMainsSize(test *testing.T) {
	test.Parallel()
	if size := unsafe.Sizeof(rule.Rule{}); size != mainRuleSize {
		test.Errorf("rule.Rule is %d bytes, main's is %d: a field a plain run does not read belongs in a side table, "+
			"as the rules' inputs are (inputs.go)", size, mainRuleSize)
	}
	ruleType := reflect.TypeFor[rule.Rule]()
	fields := make([]string, 0, ruleType.NumField())
	for index := range ruleType.NumField() {
		fields = append(fields, ruleType.Field(index).Name)
	}
	if !slices.Equal(fields, mainRuleFields) {
		test.Errorf("rule.Rule's fields are %q, main's %q: a field a plain run does not read belongs in a side table, "+
			"as the rules' inputs are (inputs.go)", fields, mainRuleFields)
	}
}

// The table parses, every line a rule's name, one of the three inputs and optionally FullKind, so DeclaredInputs never
// answers an error to the tests, the land gate or block 4.
func TestTheInputsTableParses(test *testing.T) {
	test.Parallel()
	declared, parseError := DeclaredInputs()
	if parseError != nil {
		test.Fatal(parseError)
	}
	counts := map[Inputs]int{}
	fullKind := 0
	for _, read := range declared {
		counts[read.Inputs]++
		if read.FullKind {
			fullKind++
		}
	}
	test.Logf("%d rules: %d PerUnit, %d FileSyntax, %d Composes, %d FullKind", len(declared), counts[InputsPerUnit],
		counts[InputsFileSyntax], counts[InputsComposes], fullKind)
}

// tableDisagreements is every way registered disagrees with declared: a rule with no line, and a line naming no rule.
func tableDisagreements(registered []rule.Rule, declared map[string]Declared) []string {
	var disagreements []string
	named := map[string]bool{}
	for _, registeredRule := range registered {
		named[registeredRule.Name] = true
		if _, isListed := declared[registeredRule.Name]; !isListed {
			disagreements = append(disagreements, fmt.Sprintf("%s has no line in rule_inputs.txt, so nobody has read "+
				"what its findings are a function of; read it and add its line", registeredRule.Name))
		}
	}
	for name := range declared {
		if !named[name] {
			disagreements = append(disagreements, fmt.Sprintf("rule_inputs.txt has a line for %s, which no rule is "+
				"registered as", name))
		}
	}
	slices.Sort(disagreements)
	return disagreements
}

// Every registered rule has its line in the table, and the table names no rule the registry lacks: a rule added with no
// line fails, so every rule is read for its inputs before it lands, and so does a line a removed rule left. The check
// also runs over a planted registry and table, to prove each disagreement fails.
func TestEveryRegisteredRuleHasItsInputsInTheTable(test *testing.T) {
	test.Parallel()
	declared, parseError := DeclaredInputs()
	if parseError != nil {
		test.Fatal(parseError)
	}
	for _, disagreement := range tableDisagreements(registry, declared) {
		test.Error(disagreement)
	}

	planted := []rule.Rule{{Name: "cohere-go/S1000"}, {Name: "cohere-go/consistency-no-new-rule"}}
	plantedTable := map[string]Declared{
		"cohere-go/S1000":   {Inputs: InputsPerUnit},
		"cohere-go/retired": {Inputs: InputsPerUnit},
	}
	expected := []string{
		"cohere-go/consistency-no-new-rule has no line in rule_inputs.txt",
		"rule_inputs.txt has a line for cohere-go/retired",
	}
	disagreements := tableDisagreements(planted, plantedTable)
	if len(disagreements) != len(expected) {
		test.Fatalf("the planted registry has %d disagreements, expected %d: %q", len(disagreements), len(expected),
			disagreements)
	}
	for index, saying := range expected {
		if !strings.HasPrefix(disagreements[index], saying) {
			test.Errorf("planted disagreement %d is %q, expected it to start %q", index, disagreements[index], saying)
		}
	}
}

// The table is read strictly, so a typo in a line fails rather than reading as a rule with no line: inputs that are
// none of the three, a third field that is not FullKind, a name alone, and a rule listed twice.
func TestTheInputsTableRefusesAMalformedLine(test *testing.T) {
	test.Parallel()
	for _, malformed := range []struct {
		name   string
		text   string
		saying string
	}{
		{"unknown inputs", "cohere-go/S1000 per-unit\n", "none of the three inputs"},
		{"a third field that is not FullKind", "cohere-go/S1000 PerUnit Full\n", "optionally FullKind"},
		{"a name alone", "cohere-go/S1000\n", "optionally FullKind"},
		{"a rule twice", "cohere-go/S1000 PerUnit\ncohere-go/S1000 Composes\n", "has a line already"},
	} {
		_, parseError := ParseInputs("# a comment\n\n" + malformed.text)
		if parseError == nil || !strings.Contains(parseError.Error(), malformed.saying) {
			test.Errorf("%s: parsed with %v, expected it to say %q", malformed.name, parseError, malformed.saying)
		}
	}
	declared, parseError := ParseInputs("# a comment\n\ncohere-go/S1000 PerUnit\ncohere-go/U1000 Composes FullKind\n")
	if parseError != nil || declared["cohere-go/U1000"] != (Declared{Inputs: InputsComposes, FullKind: true}) ||
		declared["cohere-go/S1000"] != (Declared{Inputs: InputsPerUnit}) {
		test.Errorf("a well-formed table parsed as %v (%v)", declared, parseError)
	}
}
