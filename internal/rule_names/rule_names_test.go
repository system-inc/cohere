package rule_names

import (
	"slices"
	"testing"
)

// The names are every language's: a TypeScript port's upstream name, a house rule of each language, and as many as
// documentation/data/rules.json counts. That they are the names the registries render now, in rules.json's order,
// is internal/documentation_data's freshness check, which holds this package's generated file as it holds the
// website's data.
func TestRuleNamesAreEveryLanguagesRules(test *testing.T) {
	test.Parallel()
	names := RuleNames()
	for _, name := range []string{
		"max-lines",
		"nexus/consistency-no-abbreviated-file-name",
		"cohere-go/consistency-no-abbreviated-file-name",
		"cohere-swift/consistency-no-abbreviated-file-name",
	} {
		if !slices.Contains(names, name) {
			test.Errorf("the rule names lack %s", name)
		}
	}
	if len(names) < 700 {
		test.Errorf("the rule names count %d, fewer than the registries hold", len(names))
	}
}

// A caller gets its own copy, so changing it never changes what the next caller reads.
func TestRuleNamesIsACopy(test *testing.T) {
	test.Parallel()
	first := RuleNames()
	first[0] = "changed"
	if RuleNames()[0] == "changed" {
		test.Error("RuleNames returned the package's own slice")
	}
}
