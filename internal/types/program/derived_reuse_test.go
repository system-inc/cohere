package program_test

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

// markerRule is a rule that reads another file: it reports every variable declaration with the text of
// marker.ts, which it finds through the program's file list. fingerprint is its ProgramFingerprint, nil for
// none. runs counts the files it was run on, by base name.
func markerRule(name string, fingerprint func(rule.Program) [sha256.Size]byte, runs *sync.Map) rule.Rule {
	return rule.Rule{
		Name:               name,
		ProgramReads:       rule.ReadsOtherFiles,
		ProgramFingerprint: fingerprint,
		Run: func(ctx rule.Context, options any) rule.Listeners {
			if filepath.Base(ctx.SourceFile.FileName()) == "marker.ts" {
				return nil
			}
			count, _ := runs.LoadOrStore(filepath.Base(ctx.SourceFile.FileName()), new(int))
			*count.(*int)++
			marker := markerText(ctx.Program)
			return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {
				ctx.ReportNode(node, rule.Message{Id: "marker", Description: "the marker says " + marker})
			}}
		},
	}
}

// markerText is marker.ts's text, trimmed, read through the program's file list.
func markerText(program rule.Program) string {
	for _, sourceFile := range program.SourceFiles() {
		if filepath.Base(sourceFile.FileName()) == "marker.ts" {
			return strings.TrimSpace(sourceFile.Text())
		}
	}
	return ""
}

// markerFingerprint is markerRule's complete fingerprint: marker.ts's text is all it reads beyond its file.
func markerFingerprint(program rule.Program) [sha256.Size]byte {
	return sha256.Sum256([]byte(markerText(program)))
}

// runsOf is how many files a markerRule was run on, the marker excluded.
func runsOf(runs *sync.Map) int {
	total := 0
	runs.Range(func(_, count any) bool {
		total += *count.(*int)
		return true
	})
	return total
}

// A derived rule replays on every file whose bytes are unchanged while its program fingerprint is too, runs
// again on every file once the data its fingerprint covers changes, and always agrees with a walk with no
// cache (#kdee854). The control is a fingerprint that misses the data the rule reads: it replays the old marker
// after the marker changed, so the comparison with an uncached walk is shown able to fail.
func TestADerivedRuleReplaysWhileItsProgramFingerprintHolds(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name        string
		fingerprint func(rule.Program) [sha256.Size]byte
		wantStale   bool
	}{
		{"complete fingerprint", markerFingerprint, false},
		{"control: a fingerprint that misses the marker", func(rule.Program) [sha256.Size]byte { return [sha256.Size]byte{7} }, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			root := writeProject(t, map[string]string{
				"tsconfig.json": minimalConfig,
				"marker.ts":     "// red\n",
				"a.ts":          "export const a = 1;\n",
				"b.ts":          "export const b = 2;\n",
			})
			ctx := context.Background()
			key := program.HashRuleSet([]string{"derived"})
			var recorded *program.LintCache
			walk := func(cached bool) (program.Result, *sync.Map) {
				t.Helper()
				graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(root, "tsconfig.json")})
				if err != nil {
					t.Fatal(err)
				}
				runs := &sync.Map{}
				rules := []rule.Rule{markerRule("test-derived", scenario.fingerprint, runs)}
				var reuse *program.FindingsReuse
				if cached {
					reuse = program.NewFindingsReuse(key, recorded, program.PathAnchor{})
					graph.FindingsReuse = reuse
				}
				result, err := graph.Walk(ctx, graph.ProjectFiles(), rules)
				if err != nil {
					t.Fatal(err)
				}
				if cached {
					recorded = roundTripLintCache(t, reuse.Recorded())
				}
				return result, runs
			}
			agrees := func(stage string, warm program.Result) bool {
				t.Helper()
				cold, _ := walk(false)
				return reflect.DeepEqual(diagnosticKeys(warm.Diagnostics), diagnosticKeys(cold.Diagnostics))
			}

			walk(true)
			warm, runs := walk(true)
			if runsOf(runs) != 0 || warm.FilesReplayed != 3 {
				t.Errorf("nothing changed, and the rule ran on %d files with %d replayed; want 0 and all 3", runsOf(runs), warm.FilesReplayed)
			}
			if !agrees("unchanged", warm) {
				t.Errorf("an unchanged tree's replay differs from an uncached walk")
			}

			// An edit outside what the fingerprint covers: the edited file runs, the other replays.
			if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a = 1;\nexport const more = 3;\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			warm, runs = walk(true)
			if runsOf(runs) != 1 {
				t.Errorf("after an edit to a.ts the rule ran on %d files, want a.ts alone", runsOf(runs))
			}
			if !agrees("a.ts edited", warm) {
				t.Errorf("after an edit to a.ts the replay differs from an uncached walk")
			}

			// The data the rule reads changes, and no file it reports in does.
			if err := os.WriteFile(filepath.Join(root, "marker.ts"), []byte("// blue\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			warm, runs = walk(true)
			stale := !agrees("marker edited", warm)
			if stale != scenario.wantStale {
				t.Errorf("after the marker changed the replay is stale: %v, want %v (the rule ran on %d files)", stale, scenario.wantStale,
					runsOf(runs))
			}
			if !scenario.wantStale && runsOf(runs) != 2 {
				t.Errorf("after the marker changed the rule ran on %d files, want both", runsOf(runs))
			}
		})
	}
}

// A rule that reads other files without a program fingerprint is walked on every file, every run, as before:
// the default is the sound one. A fingerprint that panics leaves its rule unkeyed for the run, walked
// everywhere and recorded nowhere, and the walk goes on.
func TestARuleWithNoUsableProgramFingerprintIsWalkedEveryRun(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name        string
		fingerprint func(rule.Program) [sha256.Size]byte
	}{
		{"none declared", nil},
		{"one that panics", func(rule.Program) [sha256.Size]byte { panic("a fingerprint that fails") }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			root := writeProject(t, map[string]string{
				"tsconfig.json": minimalConfig,
				"marker.ts":     "// red\n",
				"a.ts":          "export const a = 1;\n",
			})
			ctx := context.Background()
			key := program.HashRuleSet([]string{"uncacheable"})
			var recorded *program.LintCache
			for run := 1; run <= 2; run++ {
				graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(root, "tsconfig.json")})
				if err != nil {
					t.Fatal(err)
				}
				runs := &sync.Map{}
				reuse := program.NewFindingsReuse(key, recorded, program.PathAnchor{})
				graph.FindingsReuse = reuse
				result, err := graph.Walk(ctx, graph.ProjectFiles(), []rule.Rule{markerRule("test-reads-other-files", scenario.fingerprint, runs)})
				if err != nil {
					t.Fatal(err)
				}
				if runsOf(runs) != 1 {
					t.Errorf("run %d: the rule ran on %d files, want a.ts every run", run, runsOf(runs))
				}
				if len(result.Diagnostics) != 1 {
					t.Errorf("run %d: %d findings, want a.ts's one", run, len(result.Diagnostics))
				}
				recorded = roundTripLintCache(t, reuse.Recorded())
			}
		})
	}
}
