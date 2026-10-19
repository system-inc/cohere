package program_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/rules/nexus"
	"github.com/system-inc/cohere/internal/types/program"
)

func deadDirectiveRules() []rule.Rule {
	return []rule.Rule{
		{Name: "test-quiet", Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) {}}
		}},
		{Name: "test-live", Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{ast.KindDebuggerStatement: func(node *ast.Node) {
				ctx.ReportNode(node, rule.Message{Id: "debugger", Description: "debugger"})
			}}
		}},
	}
}

func deadDirectiveGraph(t *testing.T, root, source string) *program.Graph {
	t.Helper()
	graph, err := program.Build(program.Options{
		ConfigFileName: "tsconfig.json", CurrentDirectory: root,
		FileSystem: program.NewMemoryFS(map[string]string{
			filepath.Join(root, "tsconfig.json"): minimalConfig,
			filepath.Join(root, "main.ts"):       source,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func walkDeadDirectiveFixture(t *testing.T, graph *program.Graph, rules []rule.Rule) program.Result {
	t.Helper()
	result, err := graph.Walk(context.Background(), graph.ProjectFiles(), rules)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Each shape checks the finding's own comment range and the edit engine's exact output. The
// filtered and live shapes share the dead fixture's rule set; mutants removing either eligibility
// or usage checks must turn those negative fixtures red. Fixes are re-walked to prove convergence.
func TestDeadDisableFindingsAndFixes(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name, source, fixed string
		filtered            bool
		want                int
	}{
		{"dead full run", "// eslint-disable-next-line test-quiet -- stale\nconst a = 1;\n", "\nconst a = 1;\n", false, 1},
		{"same directive filtered run", "// eslint-disable-next-line test-quiet -- stale\nconst a = 1;\n", "// eslint-disable-next-line test-quiet -- stale\nconst a = 1;\n", true, 0},
		{"live", "// eslint-disable-next-line test-live -- needed\ndebugger;\n", "// eslint-disable-next-line test-live -- needed\ndebugger;\n", false, 0},
		{"dead beside live", "// eslint-disable-next-line test-quiet, test-live -- needed\ndebugger;\n", "// eslint-disable-next-line test-live -- needed\ndebugger;\n", false, 1},
		{"live beside dead", "// eslint-disable-next-line test-live, test-quiet -- needed\ndebugger;\n", "// eslint-disable-next-line test-live -- needed\ndebugger;\n", false, 1},
		{"dead beside unselected", "// eslint-disable-next-line test-quiet, plugin/not-selected -- keep\nconst a = 1;\n", "// eslint-disable-next-line plugin/not-selected -- keep\nconst a = 1;\n", false, 1},
		{"unselected beside dead", "/* cohere-disable plugin/not-selected, test-quiet -- keep */\nconst a = 1;\n", "/* cohere-disable plugin/not-selected -- keep */\nconst a = 1;\n", false, 1},
		{"all names dead", "// cohere-disable-next-line test-live, test-quiet\nconst a = 1;\n", "\nconst a = 1;\n", false, 1},
		{"same line", "const a = 1; // cohere-disable-line test-quiet\n", "const a = 1; \n", false, 1},
		{"block between tokens", "const/* cohere-disable test-quiet */a = 1;\n", "const a = 1;\n", false, 1},
		{"multiline block keeps line breaks", "/* eslint-disable\n test-quiet\n -- stale */\nconst a = 1;\n", "\n\n\nconst a = 1;\n", false, 1},
		{"multiline partial keeps line breaks", "/* cohere-disable test-quiet,\n plugin/not-selected\n -- keep */\nconst a = 1;\n", "/* cohere-disable plugin/not-selected\n\n -- keep */\nconst a = 1;\n", false, 1},
		{"blanket dead", "/* eslint-disable */\nconst a = 1;\n", " \nconst a = 1;\n", false, 1},
		{"blanket live", "/* eslint-disable */\ndebugger;\n", "/* eslint-disable */\ndebugger;\n", false, 0},
		{"unicode whitespace", "/* cohere-disable\u00a0test-quiet,\uFEFFplugin/not-selected -- keep */\nconst a = 1;\n", "/* cohere-disable\u00a0plugin/not-selected -- keep */\nconst a = 1;\n", false, 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			rules := deadDirectiveRules()
			if fixture.filtered {
				rules = rules[1:]
			}
			lint := func(source string) []rule.Diagnostic {
				return walkDeadDirectiveFixture(t, deadDirectiveGraph(t, root, source), rules).Diagnostics
			}
			findings := lint(fixture.source)
			if len(findings) != fixture.want {
				t.Fatalf("got %d findings, want %d: %v", len(findings), fixture.want, diagnosticKeys(findings))
			}
			for _, finding := range findings {
				if finding.RuleName != "unused-disable" || finding.Message.Id != "unusedDisable" || len(finding.Fixes) != 1 {
					t.Fatalf("want one fixable unused-disable finding, got %+v", finding)
				}
				comment := fixture.source[finding.Range.Pos():finding.Range.End()]
				if !strings.HasPrefix(comment, "//") && !strings.HasPrefix(comment, "/*") {
					t.Fatalf("range is not the comment: %q", comment)
				}
			}
			fixed, err := edit.FixText(filepath.Join(root, "main.ts"), fixture.source, func(fileName, text string) ([]edit.Proposal, error) {
				return edit.ProposalsFrom(lint(text)), nil
			}, 2)
			if err != nil {
				t.Fatal(err)
			}
			if fixed.Text != fixture.fixed {
				t.Fatalf("fixed text\n got %q\nwant %q", fixed.Text, fixture.fixed)
			}
			if after := lint(fixed.Text); len(after) != 0 {
				t.Fatalf("%d findings after fix, want 0", len(after))
			}
		})
	}
}

// A cached live name remains live when the same comment has a dead name. The cold run plants
// a withheld finding, and the warm run must replay it by rule, not merely mark the whole comment used.
func TestDeadDisableFindingsSurviveReplay(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := "// cohere-disable-next-line test-quiet, test-live -- needed\ndebugger;\n"
	graph := deadDirectiveGraph(t, root, source)
	rules := deadDirectiveRules()
	key := program.HashRuleSet([]string{"dead-directive-fixture"})
	recording := program.NewFindingsReuse(key, nil, program.PathAnchor{})
	graph.FindingsReuse = recording
	cold := walkDeadDirectiveFixture(t, graph, rules)
	cache := recording.Recorded()
	if len(cache.Entries) != 1 || len(cache.Entries[0].Withheld) != 1 {
		t.Fatalf("want 1 cached file with 1 withheld finding, got %+v", cache.Entries)
	}
	graph.FindingsReuse = program.NewFindingsReuse(key, cache, program.PathAnchor{})
	warm := walkDeadDirectiveFixture(t, graph, rules)
	if warm.FilesReplayed != 1 {
		t.Fatalf("replayed %d files, want 1", warm.FilesReplayed)
	}
	for _, result := range []program.Result{cold, warm} {
		if result.Coverage.Suppressed != 1 || len(result.Diagnostics) != 1 {
			t.Fatalf("want 1 withheld and 1 dead finding, got %+v", result)
		}
		fixed, err := edit.FixText(filepath.Join(root, "main.ts"), source, func(fileName, text string) ([]edit.Proposal, error) {
			if text != source {
				return nil, nil
			}
			return edit.ProposalsFrom(result.Diagnostics), nil
		}, 2)
		if err != nil {
			t.Fatal(err)
		}
		if want := "// cohere-disable-next-line test-live -- needed\ndebugger;\n"; fixed.Text != want {
			t.Fatalf("replay fixed %q, want %q", fixed.Text, want)
		}
	}
}

// These are the wire keys from the adjudication. The real abbreviation rule exempts them, so
// their directives are dead, while a planted abbreviated binding proves the selected rule fires.
func TestDeadDisableWireKeysUseTheRealAbbreviationRule(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := "export const payload = {\n" +
		"// cohere-disable-next-line nexus/consistency-no-abbreviated-identifier -- wire spelling\nlast_modified: 1,\n" +
		"// cohere-disable-next-line nexus/consistency-no-abbreviated-identifier -- wire spelling\ndurationMs: 2,\n" +
		"// cohere-disable-next-line nexus/consistency-no-abbreviated-identifier -- wire spelling\nmax: 3,\n};\n"
	subject := nexus.ConsistencyNoAbbreviatedIdentifier
	full := walkDeadDirectiveFixture(t, deadDirectiveGraph(t, root, source), []rule.Rule{subject})
	if len(full.Diagnostics) != 3 {
		t.Fatalf("got %d wire-key findings, want 3", len(full.Diagnostics))
	}
	for _, finding := range full.Diagnostics {
		if finding.RuleName != "unused-disable" || len(finding.Fixes) != 1 {
			t.Fatalf("want a fixable dead directive, got %+v", finding)
		}
	}
	filtered := walkDeadDirectiveFixture(t, deadDirectiveGraph(t, root, source), deadDirectiveRules()[:1])
	if len(filtered.Diagnostics) != 0 {
		t.Fatalf("filtered run reported %d findings, want 0", len(filtered.Diagnostics))
	}
	live := "// cohere-disable-next-line nexus/consistency-no-abbreviated-identifier -- needed\nconst max = 1;\n"
	suppressed := walkDeadDirectiveFixture(t, deadDirectiveGraph(t, root, live), []rule.Rule{subject})
	if len(suppressed.Diagnostics) != 0 || suppressed.Coverage.Suppressed != 1 {
		t.Fatalf("live directive: %d findings, %d withheld; want 0 and 1", len(suppressed.Diagnostics), suppressed.Coverage.Suppressed)
	}
	control := walkDeadDirectiveFixture(t, deadDirectiveGraph(t, root, "const max = 1;\n"), []rule.Rule{subject})
	if len(control.Diagnostics) != 1 || control.Diagnostics[0].RuleName != subject.Name {
		t.Fatalf("stripped binding control: want 1 abbreviation finding, got %+v", control.Diagnostics)
	}
}
