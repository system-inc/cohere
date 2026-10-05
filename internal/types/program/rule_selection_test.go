package program

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// TestFilesThatResolveAlikeShareOneSelectionAndStillCountPerFile covers the selection memo (#9jpmqm9).
// Files that resolved the same way share one selection, so it is made once; a file that resolved
// differently gets its own; and the scoped-off and unconfigured counts, which coverage reports per file,
// are still counted once for every file rather than once for every selection.
func TestFilesThatResolveAlikeShareOneSelectionAndStillCountPerFile(t *testing.T) {
	t.Parallel()
	graph := &Graph{LintConfig: &configuration.Config{
		Rules: map[string]configuration.RuleSetting{
			"runs": {Severity: configuration.SeverityError},
			"off":  {Severity: configuration.SeverityOff},
		},
		Overrides: []configuration.Override{
			{Files: []string{"special/**"}, Rules: map[string]configuration.RuleSetting{"runs": {Severity: configuration.SeverityOff}}},
		},
	}}
	rules := []rule.Rule{{Name: "runs"}, {Name: "off"}, {Name: "nobody-configured"}}
	selections := map[any]*ruleSelection{}
	scopedOff, unconfigured := map[string]int{}, map[string]int{}

	first, _ := graph.rulesFor("source/a.ts", rules, selections, scopedOff, unconfigured)
	second, _ := graph.rulesFor("source/b.ts", rules, selections, scopedOff, unconfigured)
	special, _ := graph.rulesFor("special/c.ts", rules, selections, scopedOff, unconfigured)

	if first != second {
		t.Fatal("two files that resolved alike made two selections")
	}
	if special == first {
		t.Fatal("a file an override changed shared the selection of files it did not change")
	}
	if len(first.applicable) != 1 || first.applicable[0].Name != "runs" || len(special.applicable) != 0 {
		t.Fatalf("the selections apply %v and %v, want [runs] and none", ruleNames(first.applicable), ruleNames(special.applicable))
	}
	if scopedOff["off"] != 3 || scopedOff["runs"] != 1 || unconfigured["nobody-configured"] != 3 {
		t.Fatalf("counts are scoped off %v and unconfigured %v, want off 3, runs 1 and nobody-configured 3",
			scopedOff, unconfigured)
	}
	if len(selections) != 2 {
		t.Fatalf("three files in two resolutions made %d selections", len(selections))
	}
}
