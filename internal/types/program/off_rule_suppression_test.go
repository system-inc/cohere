package program_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

// A directive naming a rule the config turns off is dead, whether or not cohere ports the rule.
//
// It silences nothing, because nothing that is off can report. The "not ported yet" excuse is for a
// rule the config wants on that this binary lacks: had it been ported, the directive might have
// withheld something. ESLint flagged 13 `no-await-in-loop` directives as unused in ahra, and it was
// right; cohere excused them as unported (#ezhwsbc). Walked through a real config, one file per
// shape, so each count below is one directive's verdict.
func TestADirectiveNamingAnOffRuleIsDead(t *testing.T) {
	quiet := rule.Rule{
		Name: "test-quiet",
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {}}
		},
	}
	registeredOff := rule.Rule{
		Name: "test-registered-off",
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {}}
		},
	}
	// Registered with its plugin, as every typescript-eslint, nexus and structure rule is. A walk that
	// keyed the rules it ran by full name and looked directives up bare filed this one's dead
	// directives as unported (#6dc4f7k).
	prefixed := rule.Rule{
		Name: "plugin/test-prefixed",
		Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {}}
		},
	}
	settings := `{"rules": {
		"plugin/test-prefixed": "error",
		"test-quiet": "error",
		"test-registered-off": "off",
		"no-await-in-loop": "off",
		"plugin/unported-on": "error"
	}}`

	for _, shape := range []struct {
		name            string
		directive       string
		wantDead        int
		wantForUnported int
	}{
		{"an off rule cohere does not register", "no-await-in-loop", 1, 0},
		{"an off rule cohere registers", "test-registered-off", 1, 0},
		{"a registered rule that ran and matched nothing", "test-quiet", 1, 0},
		{"a prefixed registered rule that ran and matched nothing", "plugin/test-prefixed", 1, 0},
		{"the same rule named bare in the directive", "test-prefixed", 1, 0},
		{"an unported rule the config turns on", "plugin/unported-on", 0, 1},
		{"a rule nobody configured or ported", "plugin/never-mentioned", 0, 1},
		{"an off rule beside an unported one the config turns on", "no-await-in-loop, plugin/unported-on", 0, 1},
	} {
		t.Run(shape.name, func(t *testing.T) {
			directory := writeProject(t, map[string]string{
				"tsconfig.json":       minimalConfig,
				"CohereSettings.json": settings,
				"main.ts":             "// eslint-disable-next-line " + shape.directive + " -- the reason\nconst a = 1;\n",
			})
			graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
			if err != nil {
				t.Fatalf("building: %v", err)
			}
			graph.LintConfig, err = configuration.Load(filepath.Join(directory, "CohereSettings.json"))
			if err != nil {
				t.Fatalf("loading the config: %v", err)
			}

			result, err := graph.Walk(context.Background(), graph.ProjectFiles(), []rule.Rule{quiet, registeredOff, prefixed})
			if err != nil {
				t.Fatalf("walking: %v", err)
			}
			coverage := result.Coverage
			if coverage.UnusedSuppressions != 1 {
				t.Fatalf("%d unused directives, want the one in the file", coverage.UnusedSuppressions)
			}
			dead := coverage.UnusedSuppressions - coverage.UnusedSuppressionsForUnrunRules
			if dead != shape.wantDead || coverage.UnusedSuppressionsForUnrunRules != shape.wantForUnported {
				t.Errorf("counted %d dead and %d for unported rules, want %d and %d",
					dead, coverage.UnusedSuppressionsForUnrunRules, shape.wantDead, shape.wantForUnported)
			}
		})
	}
}
