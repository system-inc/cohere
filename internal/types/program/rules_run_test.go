package program_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
	"github.com/system-inc/cohere/internal/types/program"
)

// The footer's rule count is the rules that ran, as the README says, not the rules the walk was handed:
// every registered rule. TanStack/query's config turned most of them off and the footer still read
// "484 rules" when 118 ran (#v1ah2qq). A rule off everywhere and a rule nobody configured did not run; a
// rule offered a file that it declined did, and so did one offered only some files.
func TestRulesRunCountsOnlyTheRulesOfferedAFile(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"CohereSettings.json": `{"rules": {"test-listens": "error", "test-declines": "error", "test-off": "off",
			"test-only-in-special": "off"},
			"overrides": [{"files": ["special.ts"], "rules": {"test-only-in-special": "error"}}]}`,
		"main.ts":    "export const value = 1;\n",
		"special.ts": "export const other = 2;\n",
	})
	graph, buildError := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}
	var loadError error
	graph.LintConfiguration, loadError = configuration.Load(filepath.Join(directory, "CohereSettings.json"))
	if loadError != nil {
		test.Fatalf("loading the config: %v", loadError)
	}

	listens := func(name string) rule.Rule[rule.NoOptions] {
		return rule.Rule[rule.NoOptions]{
			Name: name,
			Run: func(rule.Context, rule.Given[rule.NoOptions]) rule.Listeners {
				return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {
					// Listens and reports nothing: the test counts which rules were offered a file, not what they
					// found.
				}}
			},
		}
	}
	declines := rule.Rule[rule.NoOptions]{
		Name: "test-declines",
		Run:  func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners { return nil },
	}
	rules := []rule.Rule[rule.NoOptions]{
		listens("test-listens"),
		declines,
		listens("test-off"),
		listens("test-only-in-special"),
		listens("test-nobody-configured"),
	}

	result, walkError := graph.Walk(context.Background(), graph.ProjectFiles(), rule_testing.EntriesOf(test, rules...))
	if walkError != nil {
		test.Fatalf("walking: %v", walkError)
	}
	if result.Coverage.RulesRun != 3 {
		test.Errorf(
			"RulesRun is %d of the %d rules handed in, want 3: test-listens, test-declines and test-only-in-special",
			result.Coverage.RulesRun,
			len(rules),
		)
	}
}
