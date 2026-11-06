//go:build unix

// Run through runWithEngine, which projects_test.go defines for Unix alone (#tejf9bc).

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// A project whose tsconfig cannot be read, inside a root whose tsconfig includes its files, is judged by
// its load failure and by nothing else (#0xg4y8w). At ahra's root, two untracked copies of a project whose
// Structure base was missing read 170,000 findings: the root linted their files in its own program, where
// their imports resolve to nothing, so every type-aware rule fired. The run fails naming the error with its
// text, the unloaded project's files carry no finding of any rule, and the root's own file is still judged.
func TestAProjectWhoseTsconfigFailsToLoadIsJudgedByThatFailureAlone(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := newRepository(test, map[string]string{
		"tsconfig.json": `{` + ownershipOptions + `,"include":["**/*.ts"]}`,
		"CohereSettings.json": `{"rules":{"no-debugger":"error","@typescript-eslint/no-unsafe-call":"error",` +
			`"@typescript-eslint/no-unsafe-member-access":"error"}}`,
		"index.ts":           "export function value(): number {\n  debugger;\n  return 1;\n}\n",
		"wire/tsconfig.json": `{"extends":"./libraries/structure/TypeScriptConfiguration.jsonc"}`,
		"wire/source/server.ts": "import { start } from \"absent-package\";\n\nexport function serve(): void {\n" +
			"  debugger;\n  start().listen.now();\n}\n",
	})
	output, exitCode := runWithEngine(test, binary, "", root, "--no-fix", "--no-format")
	if exitCode == 0 {
		test.Fatalf("a project whose tsconfig does not load passed the run:\n%s", output)
	}
	if !strings.Contains(output, "TS6053: File '") ||
		!strings.Contains(output, "TypeScriptConfiguration.jsonc' not found") {
		test.Errorf("the run does not name the load failure with its text:\n%s", output)
	}
	if strings.Contains(output, "server.ts:") {
		test.Errorf("the unloaded project's file was judged by another project's program:\n%s", output)
	}
	if !strings.Contains(output, "index.ts:2:3") {
		test.Errorf("the root's own file was not judged, so the run proves nothing about the other:\n%s", output)
	}
}

// runForJson runs the binary with --json and keeps its stdout and its stderr apart, since the contract is
// that stdout is JSON lines alone.
func runForJson(test *testing.T, binary string, directory string, arguments ...string) (string, string, int) {
	test.Helper()
	command := exec.Command(binary, append([]string{"--json"}, arguments...)...)
	command.Dir = directory
	command.Env = append(os.Environ(), "COHERE_VERDICT_FD=")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	runError := command.Run()
	if runError == nil {
		return stdout.String(), stderr.String(), 0
	}
	exited, isExit := runError.(*exec.ExitError)
	if !isExit {
		test.Fatalf("running cohere: %v\n%s", runError, stderr.String())
	}
	return stdout.String(), stderr.String(), childExitCode(test, exited)
}

// summariesOf parses every line of a --json stdout, failing on any line that is not JSON, and returns the
// summary lines by label, each label once.
func summariesOf(test *testing.T, stdout string) map[string]map[string]any {
	test.Helper()
	summaries := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		var record map[string]any
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatalf("a line of --json stdout is not JSON (%v): %q\nstdout:\n%s", decodeError, line, stdout)
		}
		if record["kind"] != "summary" {
			continue
		}
		label, _ := record["label"].(string)
		if _, seen := summaries[label]; seen {
			test.Fatalf("two summary lines for %q:\n%s", label, stdout)
		}
		summaries[label] = record
	}
	return summaries
}

// Under --json a run whose type graph does not build ends on a summary of its own, verdict fail, with the
// error's words, and its stdout is JSON lines alone (#9zqayzh). Before, the error was a line of plain text
// on stderr and no record, and inside a repository with several projects that line landed in stdout, among
// the other projects' records: a program reading the stream saw one project fewer and nothing failing.
func TestAjsonRunWhoseGraphDoesNotBuildEndsOnAFailedSummary(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	broken := `{"extends":"./libraries/structure/TypeScriptConfiguration.jsonc"}`
	const missing = "TS6053: File './libraries/structure/TypeScriptConfiguration.jsonc' not found."

	test.Run("alone", func(test *testing.T) {
		test.Parallel()
		root := newRepository(test, map[string]string{"tsconfig.json": broken, "index.ts": "export const index = 1;\n"})
		stdout, stderr, exitCode := runForJson(test, binary, root, "--no-fix")
		summaries := summariesOf(test, stdout)
		summary, found := summaries[""]
		if exitCode == 0 || len(summaries) != 1 || !found {
			test.Fatalf("want one summary and a failing exit, got exit %d:\n%s", exitCode, stdout)
		}
		if message, _ := summary["error"].(string); summary["verdict"] != "fail" ||
			!strings.Contains(message, missing) {
			test.Errorf("the summary does not fail naming the load error: %v", summary)
		}
		if !strings.HasSuffix(strings.TrimSpace(stdout), "}") || !strings.Contains(
			stdout[strings.LastIndex(strings.TrimSuffix(stdout, "\n"), "\n")+1:],
			`"kind":"summary"`,
		) {
			test.Errorf("the summary is not the last line:\n%s", stdout)
		}
		if !strings.Contains(stderr, missing) {
			test.Errorf("stderr lost the error line a person reads:\n%s", stderr)
		}
	})

	test.Run("one of several projects", func(test *testing.T) {
		test.Parallel()
		root := newRepository(test, map[string]string{
			"tsconfig.json":       `{` + ownershipOptions + `,"include":["**/*.ts"]}`,
			"index.ts":            "export const index = 1;\n",
			"wire/tsconfig.json":  broken,
			"wire/source/wire.ts": "export const wire = 1;\n",
		})
		stdout, stderr, exitCode := runForJson(test, binary, root, "--no-fix", "--no-format")
		summaries := summariesOf(test, stdout)
		if exitCode == 0 || len(summaries) != 2 {
			test.Fatalf(
				"want a summary for each of the two projects and a failing exit, got exit %d:\n%s",
				exitCode,
				stdout,
			)
		}
		if summaries["."]["verdict"] != "pass" {
			test.Errorf("the root, which loads, did not pass: %v", summaries["."])
		}
		wire := summaries["wire"]
		if message, _ := wire["error"].(string); wire["verdict"] != "fail" || !strings.Contains(message, missing) {
			test.Errorf(
				"the project that does not load has no failed summary naming why: %v\nstdout:\n%s",
				wire,
				stdout,
			)
		}
		if !strings.Contains(stderr, missing) {
			test.Errorf("stderr lost the error line a person reads:\n%s", stderr)
		}
	})
}
