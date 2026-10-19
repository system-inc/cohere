package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func unusedDisableRecords(t *testing.T, output string) ([]findingJSON, summaryJSON) {
	t.Helper()
	var findings []findingJSON
	var summary summaryJSON
	for _, line := range strings.Split(output, "\n") {
		var kind struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal([]byte(line), &kind) != nil {
			continue
		}
		switch kind.Kind {
		case "finding":
			var finding findingJSON
			if err := json.Unmarshal([]byte(line), &finding); err != nil {
				t.Fatal(err)
			}
			findings = append(findings, finding)
		case "summary":
			if err := json.Unmarshal([]byte(line), &summary); err != nil {
				t.Fatal(err)
			}
		}
	}
	if summary.Kind != "summary" {
		t.Fatalf("no summary in %s", output)
	}
	return findings, summary
}

// The CLI is the contract: warning-only directives neither fail lint nor --no-fix, even though
// --fix removes them. Explicit error and off, and per-file overrides, are controls in both directions.
func TestUnusedDisableSeverity(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	for _, fixture := range []struct {
		name, level, override, wantSeverity string
		wantExit                            int
	}{
		{"default", "", "", "warn", 0},
		{"warn", "warn", "", "warn", 0},
		{"error", "error", "", "error", 1},
		{"off", "off", "", "", 0},
		{"override off", "error", "off", "", 0},
		{"override error", "off", "error", "error", 1},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			project := newRunCacheFixture(t, binary)
			settings := map[string]any{"rules": map[string]string{"nexus/consistency-no-ambiguous-identifier": "off", "@typescript-eslint/no-inferrable-types": "off", "no-debugger": "error"}}
			if fixture.level != "" {
				settings["rules"].(map[string]string)["unused-disable"] = fixture.level
			}
			if fixture.override != "" {
				settings["overrides"] = []any{map[string]any{"files": []string{"source/a.ts"}, "rules": map[string]string{"unused-disable": fixture.override}}}
			}
			encoded, _ := json.Marshal(settings)
			project.write("CohereSettings.json", string(encoded))
			source := "// cohere-disable-next-line no-debugger -- stale\nexport const a: number = 1;\n"
			project.write("source/a.ts", source)
			output, exit := runCohere(t, binary, project.root, "--format-only", "--no-cache")
			if exit != 0 {
				t.Fatalf("format fixture exit %d:\n%s", exit, output)
			}
			for _, mode := range []string{"--lint", "--no-fix"} {
				output, exit := runCohere(t, binary, project.root, mode, "--no-cache", "--json")
				findings, summary := unusedDisableRecords(t, output)
				if exit != fixture.wantExit || (summary.Verdict == "fail") != (fixture.wantExit != 0) {
					t.Fatalf("%s exit=%d verdict=%s, want exit %d:\n%s", mode, exit, summary.Verdict, fixture.wantExit, output)
				}
				count := 0
				for _, finding := range findings {
					if finding.Rule == "unused-disable" {
						count++
						if finding.Severity != fixture.wantSeverity {
							t.Fatalf("%s severity=%s, want %s", mode, finding.Severity, fixture.wantSeverity)
						}
					} else if fixture.wantExit == 0 {
						t.Fatalf("unexpected finding: %+v", finding)
					}
				}
				want := 0
				if fixture.wantSeverity != "" {
					want = 1
				}
				if count != want {
					t.Fatalf("%s reported %d unused directives, want %d", mode, count, want)
				}
				bytes, err := os.ReadFile(filepath.Join(project.root, "source/a.ts"))
				if err != nil {
					t.Fatal(err)
				}
				if string(bytes) != source {
					t.Fatal("read-only run rewrote the comment")
				}
			}
			for pass := 0; pass < 2; pass++ {
				output, exit := runCohere(t, binary, project.root, "--no-fix", "--json")
				_, summary := unusedDisableRecords(t, output)
				if exit != fixture.wantExit || (summary.Verdict == "fail") != (fixture.wantExit != 0) {
					t.Fatalf("cached check exit=%d verdict=%s, want %d:\n%s", exit, summary.Verdict, fixture.wantExit, output)
				}
				if pass == 1 && !summary.Cache.Replayed {
					t.Fatalf("second check did not replay:\n%s", output)
				}
			}
			verbose, _ := runCohere(t, binary, project.root, "--lint", "--coverage", "--verbose", "--no-cache")
			if !strings.Contains(verbose, "1 disable comments silenced nothing while their rule ran") {
				t.Fatalf("coverage lost the dead count:\n%s", verbose)
			}
			if strings.Contains(verbose, `key "unused-disable" matches no registered rule`) || strings.Contains(verbose, "parity:") {
				t.Fatalf("engine setting was treated as unported:\n%s", verbose)
			}
			output, exit = runCohere(t, binary, project.root, "--fix", "--no-format", "--no-cache", "--json")
			if exit != 0 {
				t.Fatalf("--fix exit %d:\n%s", exit, output)
			}
			bytes, _ := os.ReadFile(filepath.Join(project.root, "source/a.ts"))
			want := strings.TrimPrefix(source, "// cohere-disable-next-line no-debugger -- stale")
			if fixture.wantSeverity == "" {
				want = source
			}
			if string(bytes) != want {
				t.Fatalf("--fix wrote %q, want %q", bytes, want)
			}
			if fixture.wantSeverity == "" {
				project.write("source/a.ts", source+"debugger;\n")
				_, exit = runCohere(t, binary, project.root, "--fix", "--no-format", "--no-cache", "--json")
				bytes, _ = os.ReadFile(filepath.Join(project.root, "source/a.ts"))
				if exit != 0 || !strings.HasPrefix(string(bytes), source) || strings.Contains(string(bytes), "debugger;") {
					t.Fatalf("off did not survive relint: exit=%d text=%q", exit, bytes)
				}
			}
		})
	}
}

// SeverityWarn's report-without-failing contract applies to ordinary lint diagnostics too.
func TestLintWarningVerdict(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	for _, level := range []string{"warn", "error"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			project := newRunCacheFixture(t, binary)
			project.write("CohereSettings.json", fmt.Sprintf(`{"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off","no-debugger":%q}}`, level))
			project.write("source/a.ts", "export const a: number = 1;\ndebugger;\n")
			output, exit := runCohere(t, binary, project.root, "--lint", "--no-cache", "--json")
			findings, summary := unusedDisableRecords(t, output)
			wantExit := 0
			if level == "error" {
				wantExit = 1
			}
			if exit != wantExit || len(findings) != 1 || findings[0].Severity != level || (summary.Verdict == "fail") != (wantExit == 1) {
				t.Fatalf("%s exit=%d findings=%+v verdict=%s", level, exit, findings, summary.Verdict)
			}
		})
	}
}
