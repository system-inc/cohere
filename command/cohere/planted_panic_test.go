package main

import (
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/build_inputs"
)

// A rule's crash is cohere's bug, and the run that meets one fails, names it in every view, and finishes
// everything else (#v1ah2qq). TanStack/query's run met six react-hooks crashes and ended with a footer
// marker as the only trace in the default view, and with no findings beside them it would have ended green.
//
// The rule that crashes is planted: planted_panic_rule.go, compiled into this test's binary alone by the
// cohere_planted_panic tag. Two projects. In the first a real rule, no-empty, runs beside the planted one,
// and must still report on both files, the crashed file included. In the second the planted rule runs
// alone, so nothing but the crash can fail the run.
func TestARuleCrashFailsTheRunAndNamesItselfAsCohere(test *testing.T) {
	test.Parallel()
	binary := filepath.Join(test.TempDir(), "cohere")
	build_inputs.Record(test, ".", "-tags", "cohere_planted_panic", ".")
	build := exec.Command("go", "build", "-buildvcs=false", "-tags", "cohere_planted_panic", "-o", binary, ".")
	if output, outputError := build.CombinedOutput(); outputError != nil {
		test.Fatalf("cannot build cohere with the planted rule: %v\n%s", outputError, output)
	}

	// The settings name the planted rule on top of the house's, so a body other rules have nothing to say
	// about is the only way to leave the crash as the run's one problem.
	project := func(rules string, body string) string {
		test.Helper()
		root := test.TempDir()
		writeTree(test, root, map[string]string{
			"tsconfig.json":       fixScopeTsconfig,
			"CohereSettings.json": `{"rules":{` + rules + `}}`,
			"crashes.ts":          "// cohere-test: panic here\n" + body,
			"survives.ts":         body,
		})
		return root
	}
	run := func(root string, arguments ...string) (string, int) {
		test.Helper()
		command := exec.Command(binary, append([]string{"--no-format"}, arguments...)...)
		command.Dir = root
		output, outputError := command.CombinedOutput()
		var exitError *exec.ExitError
		switch {
		case outputError == nil:
			return string(output), 0
		case errors.As(outputError, &exitError):
			return string(output), childExitCode(test, exitError)
		}
		test.Fatalf("running cohere: %v\n%s", outputError, output)
		return "", 0
	}
	const planted = "cohere-test/panics-on-marked-file"

	beside := project(
		`"`+planted+`":"error","no-empty":"error"`,
		"export function check(flag: boolean): void {\n\tif (flag) {}\n}\n",
	)
	output, code := run(beside, "--no-cache")
	if code != 1 {
		test.Errorf("exit %d with a crash beside findings, want 1:\n%s", code, output)
	}
	// The crashed file's empty block is a line lower, under the marker comment.
	for _, location := range []string{"crashes.ts:3:12", "survives.ts:2:12"} {
		if !strings.Contains(output, location+" error no-empty") {
			test.Errorf(
				"the rule beside the crash did not report at %s, so the crash cost more than its own verdict:\n%s",
				location,
				output,
			)
		}
	}

	alone := project(`"`+planted+`":"error"`, "export function check(flag: boolean): boolean {\n\treturn !flag;\n}\n")
	// The default view, then its replay: a crashed run ends in a verdict, so the run cache records it, and the
	// replay prints the recorded crash line rather than the footer's count alone.
	for _, pass := range []string{"cold", "replayed"} {
		output, code = run(alone)
		if code != 1 {
			test.Errorf(
				"%s: exit %d from a run whose only problem is a crash, want 1, since a missing verdict is not a pass:\n%s",
				pass,
				code,
				output,
			)
		}
		lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
		crashLines := []string{}
		for _, line := range lines {
			if strings.HasPrefix(line, "  crashed: ") {
				crashLines = append(crashLines, line)
			}
		}
		if len(crashLines) != 1 {
			test.Fatalf(
				"%s: the default view printed %d crash lines for one crash, want 1:\n%s",
				pass,
				len(crashLines),
				output,
			)
		}
		for _, want := range []string{
			"  crashed: " + filepath.Join(alone, "crashes.ts") + ": cohere's rule " + planted + " crashed on this file",
			"This is a bug in cohere, not in your code",
			reportBugsAt,
			"Unhandled case in Node.Text",
		} {
			if !strings.Contains(crashLines[0], want) {
				test.Errorf("%s: the crash line lacks %q:\n%s", pass, want, crashLines[0])
			}
		}
		if footer := lines[len(
			lines,
		)-1]; !strings.HasPrefix(footer, "✗ ☠️ ") || !strings.Contains(footer, "1 rule crash") {
			test.Errorf(
				"%s: the footer of a run that lost a verdict to a crash is %q, want a failing verdict that names the crash",
				pass,
				footer,
			)
		}
	}

	// And under --json, cold and then replayed, which the summary says it was: the crash line, and the summary
	// listing the crash beside its count.
	for _, wantReplayed := range []bool{false, true} {
		output, code = run(alone, "--json")
		if code != 1 {
			test.Errorf("exit %d under --json, want 1", code)
		}
		var crashes int
		var summaries int
		for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
			var record struct {
				Kind    string `json:"kind"`
				Path    string `json:"path"`
				Rule    string `json:"rule"`
				Cause   string `json:"cause"`
				Verdict string `json:"verdict"`
				Cache   struct {
					Replayed bool `json:"replayed"`
				} `json:"cache"`
				Gaps struct {
					RuleCrashes int        `json:"ruleCrashes"`
					Crashes     []runCrash `json:"crashes"`
				} `json:"gaps"`
			}
			if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
				test.Fatalf("a --json line is not JSON: %v\n%s", decodeError, line)
			}
			switch record.Kind {
			case "crash":
				crashes++
				if record.Rule != planted ||
					filepath.Base(record.Path) != "crashes.ts" ||
					!strings.Contains(record.Cause, "Unhandled case") {
					test.Errorf("the crash record names %s on %s with cause %q", record.Rule, record.Path, record.Cause)
				}
			case "summary":
				summaries++
				if record.Verdict != "fail" || record.Gaps.RuleCrashes != 1 {
					test.Errorf(
						"the summary says %s with %d rule crashes, want fail and 1",
						record.Verdict,
						record.Gaps.RuleCrashes,
					)
				}
				if record.Cache.Replayed != wantReplayed {
					test.Errorf("the summary says replayed %t, want %t", record.Cache.Replayed, wantReplayed)
				}
				if listed := record.Gaps.Crashes; len(listed) != 1 || listed[0].Rule != planted ||
					listed[0].Path != filepath.Join(alone, "crashes.ts") ||
					!strings.Contains(listed[0].Cause, "Unhandled case") {
					test.Errorf(
						"replayed %t: the summary lists the crashes as %+v, want the one crash with its rule and cause",
						wantReplayed,
						listed,
					)
				}
			}
		}
		if crashes != 1 || summaries != 1 {
			test.Errorf(
				"--json printed %d crash records and %d summaries, want 1 of each:\n%s",
				crashes,
				summaries,
				output,
			)
		}
	}
}
