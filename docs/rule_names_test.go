package docs

import (
	"slices"
	"testing"
)

// The names are every engine's: a TypeScript port's upstream name, a house rule of each engine, and as many as
// data/rules.json counts.
func TestRuleNamesAreEveryEnginesRules(test *testing.T) {
	test.Parallel()
	names := RuleNames()
	for _, name := range []string{
		"max-lines",
		"nexus/consistency-no-abbreviated-file-name",
		"cohere-go/consistency-no-abbreviated-file-name",
		"cohere-swift/consistency-no-abbreviated-file-name",
	} {
		if !slices.Contains(names, name) {
			test.Errorf("data/rules.json's names lack %s", name)
		}
	}
	if len(names) < 700 {
		test.Errorf("data/rules.json names %d rules, fewer than the registries hold", len(names))
	}
}
