package docs_data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/registry"
)

// swiftVerdictNames are the verdicts swift/HouseRuleVerdicts.json may give, the five Swift's decoding accepts.
var swiftVerdictNames = []string{"Ported", "Port", "Compiler", "Formatter", "NotApplicable"}

// houseRuleNamespaces are the namespaces whose rules each take one Swift verdict, the ones Swift's
// HouseRuleVerdictsTests reads from internal/lint/rules.
var houseRuleNamespaces = []string{"nexus/", "structure/", "base/"}

// isHouseRule reports whether a registered rule takes a Swift verdict.
func isHouseRule(name string) bool {
	return slices.ContainsFunc(houseRuleNamespaces, func(namespace string) bool {
		return strings.HasPrefix(name, namespace)
	})
}

// houseVerdictProblems holds swift/HouseRuleVerdicts.json to the registered house rules, the invariant Swift's
// HouseRuleVerdictsTests.everyHouseRuleHasExactlyOneVerdictAndNothingElse holds: every house rule has exactly
// one row, every row names a registered house rule, the rows are sorted, and each verdict is one of the five.
// It returns each problem as a sentence naming its rule, none when the catalog holds.
func houseVerdictProblems(registered []string, catalog []byte) ([]string, error) {
	var rows []struct {
		TypeScriptRule string `json:"typeScriptRule"`
		Verdict        string `json:"verdict"`
	}
	if decodeError := json.Unmarshal(catalog, &rows); decodeError != nil {
		return nil, fmt.Errorf("reading the Swift verdict catalog: %w", decodeError)
	}
	houseRules := map[string]bool{}
	for _, name := range registered {
		if isHouseRule(name) {
			houseRules[name] = true
		}
	}
	problems := []string{}
	rowsOf := map[string]int{}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		rowsOf[row.TypeScriptRule]++
		names = append(names, row.TypeScriptRule)
		if !houseRules[row.TypeScriptRule] {
			problems = append(problems, row.TypeScriptRule+" has a verdict but is not a registered house rule")
		}
		if !slices.Contains(swiftVerdictNames, row.Verdict) {
			problems = append(problems, fmt.Sprintf("%s has the verdict %q, not one of %s",
				row.TypeScriptRule, row.Verdict, strings.Join(swiftVerdictNames, ", ")))
		}
	}
	for name, count := range rowsOf {
		if count > 1 {
			problems = append(problems, fmt.Sprintf("%s has %d verdicts", name, count))
		}
	}
	for name := range houseRules {
		if rowsOf[name] == 0 {
			problems = append(problems, name+" is a registered house rule with no verdict")
		}
	}
	if !slices.IsSorted(names) {
		problems = append(problems, "the rows are not sorted by typeScriptRule")
	}
	slices.Sort(problems)
	return problems, nil
}

// TestEveryHouseRuleHasOneSwiftVerdict holds the committed swift/HouseRuleVerdicts.json to the live registry
// in the Go gate every landing passes (#c1x3xrp). The invariant is Swift's own test's, and the land gate runs
// none of Swift's tests: lint landed a house rule at ab511a115 with no row, and Swift's suite was red on main
// for about five hours until Swift added it.
func TestEveryHouseRuleHasOneSwiftVerdict(test *testing.T) {
	test.Parallel()
	catalog, readError := os.ReadFile(filepath.Join(moduleRoot, "swift", "HouseRuleVerdicts.json"))
	if readError != nil {
		test.Fatal(readError)
	}
	registered := registry.Names()
	house := 0
	for _, name := range registered {
		if isHouseRule(name) {
			house++
		}
	}
	if house == 0 {
		test.Fatal("no house rule is registered, so the verdicts were checked against nothing")
	}
	problems, problemsError := houseVerdictProblems(registered, catalog)
	if problemsError != nil {
		test.Fatal(problemsError)
	}
	for _, problem := range problems {
		test.Errorf("swift/HouseRuleVerdicts.json: %s; give it a row, or say why it has none in Swift", problem)
	}
}

// Each way the catalog can drift from the registry is named by its rule: a house rule with no row, a row for
// a rule not registered, a rule with two rows, rows out of order and an unknown verdict, and a rule outside the
// house namespaces needs no row.
func TestHouseVerdictProblemsNameEachDrift(test *testing.T) {
	test.Parallel()
	registered := []string{"base/consistency-a", "nexus/correctness-b", "structure/react-c", "no-var"}
	catalog := func(rows ...string) []byte {
		entries := make([]string, len(rows))
		for index, row := range rows {
			name, verdict, _ := strings.Cut(row, "=")
			entries[index] = fmt.Sprintf(`{"typeScriptRule": %q, "verdict": %q}`, name, verdict)
		}
		return []byte("[" + strings.Join(entries, ",") + "]")
	}
	for _, row := range []struct {
		catalog []byte
		want    []string
	}{
		{catalog("base/consistency-a=Ported", "nexus/correctness-b=Port", "structure/react-c=NotApplicable"), nil},
		{
			catalog("base/consistency-a=Ported", "structure/react-c=NotApplicable"),
			[]string{"nexus/correctness-b is a registered house rule with no verdict"},
		},
		{
			catalog("base/consistency-a=Ported", "nexus/correctness-b=Port", "nexus/gone=Port",
				"structure/react-c=NotApplicable"),
			[]string{"nexus/gone has a verdict but is not a registered house rule"},
		},
		{
			catalog("base/consistency-a=Ported", "nexus/correctness-b=Port", "nexus/correctness-b=Port",
				"structure/react-c=NotApplicable"),
			[]string{"nexus/correctness-b has 2 verdicts"},
		},
		{
			catalog("nexus/correctness-b=Port", "base/consistency-a=Ported", "structure/react-c=NotApplicable"),
			[]string{"the rows are not sorted by typeScriptRule"},
		},
		{
			catalog("base/consistency-a=Ported", "nexus/correctness-b=Maybe", "structure/react-c=NotApplicable"),
			[]string{`nexus/correctness-b has the verdict "Maybe", not one of Ported, Port, Compiler, Formatter, ` +
				`NotApplicable`},
		},
	} {
		problems, problemsError := houseVerdictProblems(registered, row.catalog)
		if problemsError != nil {
			test.Fatal(problemsError)
		}
		if !slices.Equal(problems, row.want) && (len(problems) != 0 || len(row.want) != 0) {
			test.Errorf("%s gave %q, want %q", row.catalog, problems, row.want)
		}
	}
}
