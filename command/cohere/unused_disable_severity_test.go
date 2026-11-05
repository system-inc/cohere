package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func unusedDisableRecords(test *testing.T, output string) ([]findingJSON, summaryJSON) {
	test.Helper()
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
			if decodeError := json.Unmarshal([]byte(line), &finding); decodeError != nil {
				test.Fatal(decodeError)
			}
			findings = append(findings, finding)
		case "summary":
			if decodeError := json.Unmarshal([]byte(line), &summary); decodeError != nil {
				test.Fatal(decodeError)
			}
		}
	}
	if summary.Kind != "summary" {
		test.Fatalf("no summary in %s", output)
	}
	return findings, summary
}

// The CLI is the contract: a dead directive fails lint and --no-fix by default (#9137dhp), and
// --fix removes it. Warning-only directives fail neither, though --fix still removes them. Explicit
// warn, error and off, and per-file overrides, are controls in both directions.
func TestUnusedDisableSeverity(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	for _, fixture := range []struct {
		name, level, override, wantSeverity string
		wantExit                            int
	}{
		{"default", "", "", "error", 1},
		{"warn", "warn", "", "warn", 0},
		{"error", "error", "", "error", 1},
		{"off", "off", "", "", 0},
		{"override off", "error", "off", "", 0},
		{"override error", "off", "error", "error", 1},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			test.Parallel()
			project := newRunCacheFixture(test, binary)
			rules := map[string]string{
				"nexus/consistency-no-ambiguous-identifier": "off", "@typescript-eslint/no-inferrable-types": "off",
				"no-debugger": "error",
			}
			if fixture.level != "" {
				rules["unused-disable"] = fixture.level
			}
			settings := map[string]any{"rules": rules}
			if fixture.override != "" {
				settings["overrides"] = []any{map[string]any{"files": []string{"source/a.ts"}, "rules": map[string]string{"unused-disable": fixture.override}}}
			}
			// Ignored: maps and lists of strings, which always encode.
			encoded, _ := json.Marshal(settings)
			project.write("CohereSettings.json", string(encoded))
			source := "// cohere-disable-next-line no-debugger -- stale\nexport const a: number = 1;\n"
			project.write("source/a.ts", source)
			output, exit := runCohere(test, binary, project.root, "--format-only", "--no-cache")
			if exit != 0 {
				test.Fatalf("format fixture exit %d:\n%s", exit, output)
			}
			for _, mode := range []string{"--lint", "--no-fix"} {
				output, exit := runCohere(test, binary, project.root, mode, "--no-cache", "--json")
				findings, summary := unusedDisableRecords(test, output)
				if exit != fixture.wantExit || (summary.Verdict == "fail") != (fixture.wantExit != 0) {
					test.Fatalf("%s exit=%d verdict=%s, want exit %d:\n%s", mode, exit, summary.Verdict, fixture.wantExit, output)
				}
				count := 0
				for _, finding := range findings {
					if finding.Rule == "unused-disable" {
						count++
						if finding.Severity != fixture.wantSeverity {
							test.Fatalf("%s severity=%s, want %s", mode, finding.Severity, fixture.wantSeverity)
						}
					} else if fixture.wantExit == 0 {
						test.Fatalf("unexpected finding: %+v", finding)
					}
				}
				want := 0
				if fixture.wantSeverity != "" {
					want = 1
				}
				if count != want {
					test.Fatalf("%s reported %d unused directives, want %d", mode, count, want)
				}
				// At error the check does not withhold the repair: --no-fix names the file --fix would
				// rewrite. At warn it does, which the unexpected-finding test above already holds.
				if mode == "--no-fix" && fixture.wantSeverity == "error" {
					wouldChange := false
					for _, finding := range findings {
						wouldChange = wouldChange || finding.Rule == "fix" && strings.HasSuffix(finding.Path, "source/a.ts")
					}
					if !wouldChange {
						test.Fatalf("--no-fix withheld the repair at error:\n%s", output)
					}
				}
				bytes, readError := os.ReadFile(filepath.Join(project.root, "source/a.ts"))
				if readError != nil {
					test.Fatal(readError)
				}
				if string(bytes) != source {
					test.Fatal("read-only run rewrote the comment")
				}
			}
			for pass := 0; pass < 2; pass++ {
				output, exit := runCohere(test, binary, project.root, "--no-fix", "--json")
				_, summary := unusedDisableRecords(test, output)
				if exit != fixture.wantExit || (summary.Verdict == "fail") != (fixture.wantExit != 0) {
					test.Fatalf("cached check exit=%d verdict=%s, want %d:\n%s", exit, summary.Verdict, fixture.wantExit, output)
				}
				if pass == 1 && !summary.Cache.Replayed {
					test.Fatalf("second check did not replay:\n%s", output)
				}
			}
			verbose, _ := runCohere(test, binary, project.root, "--lint", "--coverage", "--verbose", "--no-cache")
			if !strings.Contains(verbose, "1 disable comments silenced nothing while their rule ran") {
				test.Fatalf("coverage lost the dead count:\n%s", verbose)
			}
			if strings.Contains(verbose, `key "unused-disable" matches no registered rule`) || strings.Contains(verbose, "parity:") {
				test.Fatalf("engine setting was treated as unported:\n%s", verbose)
			}
			output, exit = runCohere(test, binary, project.root, "--fix", "--no-format", "--no-cache", "--json")
			if exit != 0 {
				test.Fatalf("--fix exit %d:\n%s", exit, output)
			}
			bytes, readError := os.ReadFile(filepath.Join(project.root, "source/a.ts"))
			if readError != nil {
				test.Fatal(readError)
			}
			want := strings.TrimPrefix(source, "// cohere-disable-next-line no-debugger -- stale")
			if fixture.wantSeverity == "" {
				want = source
			}
			if string(bytes) != want {
				test.Fatalf("--fix wrote %q, want %q", bytes, want)
			}
			if fixture.wantSeverity == "" {
				project.write("source/a.ts", source+"debugger;\n")
				_, exit = runCohere(test, binary, project.root, "--fix", "--no-format", "--no-cache", "--json")
				bytes, readError = os.ReadFile(filepath.Join(project.root, "source/a.ts"))
				if readError != nil {
					test.Fatal(readError)
				}
				if exit != 0 || !strings.HasPrefix(string(bytes), source) || strings.Contains(string(bytes), "debugger;") {
					test.Fatalf("off did not survive relint: exit=%d text=%q", exit, bytes)
				}
			}
		})
	}
}

// SeverityWarn's report-without-failing contract applies to ordinary lint diagnostics too.
func TestLintWarningVerdict(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	for _, level := range []string{"warn", "error"} {
		test.Run(level, func(test *testing.T) {
			test.Parallel()
			project := newRunCacheFixture(test, binary)
			project.write("CohereSettings.json", fmt.Sprintf(`{"rules":{"nexus/consistency-no-ambiguous-identifier":"off","@typescript-eslint/no-inferrable-types":"off","no-debugger":%q}}`, level))
			project.write("source/a.ts", "export const a: number = 1;\ndebugger;\n")
			output, exit := runCohere(test, binary, project.root, "--lint", "--no-cache", "--json")
			findings, summary := unusedDisableRecords(test, output)
			wantExit := 0
			if level == "error" {
				wantExit = 1
			}
			if exit != wantExit || len(findings) != 1 || findings[0].Severity != level || (summary.Verdict == "fail") != (wantExit == 1) {
				test.Fatalf("%s exit=%d findings=%+v verdict=%s", level, exit, findings, summary.Verdict)
			}
		})
	}
}
