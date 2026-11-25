//go:build unix

// The stand-in Swift engine here is a /bin/sh script, as combined_footer_unix_test.go's is, so the file builds on Unix.

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// languagesRun is one run of the binary over a tree: its stdout, stderr and exit code.
type languagesRun struct {
	standardOutput string
	standardError  string
	exitCode       int
}

// record is the run's record of one flag's choice under `--json`, `languages` or `formats`, failing the test when
// there is not exactly one.
func (run languagesRun) record(test *testing.T, kind string) choiceJson {
	test.Helper()
	var found []choiceJson
	scanner := bufio.NewScanner(strings.NewReader(run.standardOutput))
	for scanner.Scan() {
		var line choiceJson
		if json.Unmarshal(scanner.Bytes(), &line) == nil && line.Kind == kind {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		test.Fatalf("the run wrote %d %s records, want one:\n%s", len(found), kind, run.standardOutput)
	}
	return found[0]
}

// summaries counts the run's project summary lines under `--json`.
func (run languagesRun) summaries() int {
	return strings.Count(run.standardOutput, `"kind":"summary"`)
}

// A run naming languages or formats over a tree holding Go, Swift, a TypeScript program with a JSON file in it, and
// Markdown checks exactly what the two flags name, wherever it lives, and names the rest as left out (#s7gt4bj):
// `--languages go` checks the Go module alone; `--formats json` checks the program's JSON through a format project of
// its directory, and finds the file that is not formatted, and never the program's Markdown, which is not formatted
// either; everything here named checks what a plain run checks; a name a flag does not take is refused; a named
// format nothing here is is said; and naming only such formats is refused.
func TestLanguagesCheckWhatTheyNameWhereverItLives(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	fixture, absoluteError := filepath.Abs(filepath.Join("..", "..", "swift", "Contract", "Clean.jsonl"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	swiftEngine := filepath.Join(test.TempDir(), "cohere-swift")
	if writeError := executable_testing.WriteFile(swiftEngine, []byte("#!/bin/sh\ncat '"+fixture+"'\n"),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	writeTree(test, root, map[string]string{
		".gitignore":          ".cache/\n",
		goModuleMarker:        "module example.com/mixed\n\ngo 1.27\n",
		"alpha/alpha.go":      "// Package alpha is one.\npackage alpha\n\n// MaxSize is a size.\nconst MaxSize = 1\n",
		"swift/Package.swift": "// swift-tools-version: 6.0\n",
		"web/tsconfig.json": "{ \"compilerOptions\": { \"strict\": true, \"noEmit\": true }, " +
			"\"include\": [\"source\"] }\n",
		"web/source/Tidy.ts": "export const tidy = 1;\n",
		// Not formatted the house way, so whichever run formats the program's JSON finds it.
		"web/data.json": "{\"size\":1}\n",
		// Not formatted either, and Markdown, so a walk that keeps a language left out finds it.
		"web/Steps.md": "* one\n",
		// The same in capitals, which every walk reads as Markdown too.
		"web/NOTES.MD": "* two\n",
		"Notes.md":     "# Notes\n",
	})
	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if initializeOutput, initializeError := initialize.CombinedOutput(); initializeError != nil {
		test.Fatalf("git init: %v\n%s", initializeError, initializeOutput)
	}
	run := func(test *testing.T, arguments ...string) languagesRun {
		test.Helper()
		command := exec.Command(binary, arguments...)
		command.Dir = root
		command.Env = append(os.Environ(), "COHERE_SWIFT_ENGINE="+swiftEngine, goEngineChildVariable+"=")
		var standardOutput bytes.Buffer
		var standardError bytes.Buffer
		command.Stdout, command.Stderr = &standardOutput, &standardError
		runError := command.Run()
		var exitError *exec.ExitError
		if runError != nil && !errors.As(runError, &exitError) {
			test.Fatalf("cohere did not run: %v", runError)
		}
		return languagesRun{standardOutput.String(), standardError.String(), command.ProcessState.ExitCode()}
	}
	// Not parallel: the subtests share one tree, whose cache each run writes and the next may replay.
	test.Run("go checks the Go module alone", func(test *testing.T) {
		checked := run(test, "--no-fix", "--json", "--languages", "go")
		languagesRecord, formatsRecord := checked.record(test, "languages"), checked.record(test, "formats")
		if checked.exitCode != 0 || checked.summaries() != 1 || strings.Contains(checked.standardOutput, "data.json") ||
			strings.Contains(checked.standardOutput, "Steps.md") ||
			!slices.Equal(languagesRecord.Checked, []string{"go"}) ||
			!slices.Equal(languagesRecord.LeftOut, []string{"typescript", "swift"}) ||
			len(formatsRecord.Named) != 0 || !slices.Equal(formatsRecord.LeftOut, []string{"json", "markdown"}) {
			test.Errorf("exit %d, %d projects, records %+v %+v:\n%s", checked.exitCode, checked.summaries(),
				languagesRecord, formatsRecord, checked.standardOutput)
		}
	})
	// Not parallel: the subtests share one tree, whose cache each run writes and the next may replay.
	test.Run("json checks the program's JSON", func(test *testing.T) {
		checked := run(test, "--no-fix", "--json", "--formats", "json")
		record := checked.record(test, "formats")
		if checked.exitCode != 1 ||
			!strings.Contains(checked.standardOutput, filepath.Join("web", "data.json")) ||
			strings.Contains(checked.standardOutput, "Steps.md") ||
			strings.Contains(checked.standardOutput, "NOTES.MD") ||
			!slices.Equal(record.Checked, []string{"json"}) ||
			!slices.Equal(record.LeftOut, []string{"markdown"}) ||
			!slices.Equal(checked.record(test, "languages").LeftOut, []string{"go", "typescript", "swift"}) {
			test.Errorf("exit %d, record %+v, want 1 naming web/data.json:\n%s\n%s", checked.exitCode, record,
				checked.standardOutput, checked.standardError)
		}
	})
	// Not parallel: the subtests share one tree, whose cache each run writes and the next may replay.
	test.Run("every language here checks what a plain run does", func(test *testing.T) {
		whole := run(test, "--no-fix", "--json")
		named := run(test, "--no-fix", "--json", "--languages", "go,typescript,swift", "--formats", "json,markdown")
		record := named.record(test, "formats")
		if named.exitCode != whole.exitCode || named.summaries() != whole.summaries() ||
			len(record.LeftOut) != 0 || len(record.NotFound) != 0 || len(named.record(test, "languages").LeftOut) != 0 {
			test.Errorf("named exit %d over %d projects, record %+v; plain exit %d over %d", named.exitCode,
				named.summaries(), record, whole.exitCode, whole.summaries())
		}
	})
	// Not parallel: the subtests share one tree, whose cache each run writes and the next may replay.
	test.Run("a run that does not format checks no language only a format walk would", func(test *testing.T) {
		refused := run(test, "--no-fix", "--types", "--formats", "json")
		if refused.exitCode == 0 || !strings.Contains(refused.standardError,
			"This run names json, which a run checks here only by formatting them, and this run does not format") {
			test.Errorf("exit %d:\n%s", refused.exitCode, refused.standardError)
		}
	})
	// Not parallel: the subtests share one tree, whose cache each run writes and the next may replay.
	test.Run("a program run by its directory leaves out what was not named", func(test *testing.T) {
		checked := run(test, "--no-fix", "--languages", "typescript", "--directory", "web")
		if checked.exitCode != 0 || strings.Contains(checked.standardOutput, "Steps.md") ||
			strings.Contains(checked.standardOutput, "data.json") {
			test.Errorf("exit %d, want 0 and neither the Markdown nor the JSON formatted:\n%s", checked.exitCode,
				checked.standardOutput)
		}
	})
	// Not parallel: the subtests share one tree, whose cache each run writes and the next may replay.
	test.Run("a named file in a language not named is refused", func(test *testing.T) {
		refused := run(test, "--no-fix", "--languages", "typescript", "--directory", "web", "Steps.md")
		if refused.exitCode == 0 || strings.Contains(refused.standardOutput, "would-change") || !strings.Contains(
			refused.standardError,
			"This run names typescript, and not markdown, the format of Steps.md",
		) {
			test.Errorf("exit %d:\n%s\n%s", refused.exitCode, refused.standardOutput, refused.standardError)
		}
	})
	// Not parallel: the subtests share one tree, whose cache each run writes and the next may replay.
	test.Run("the footer names what was left out", func(test *testing.T) {
		checked := run(test, "--no-fix", "--languages", "go")
		if !strings.Contains(checked.standardOutput, "⚠ left out: typescript, swift, json, markdown") {
			test.Errorf("the default view does not name what it left out:\n%s", checked.standardOutput)
		}
	})
	// Not parallel: the subtests share one tree, whose cache each run writes and the next may replay.
	test.Run("an unknown language is refused", func(test *testing.T) {
		refused := run(test, "--no-fix", "--languages", "go,kotlin")
		if refused.exitCode == 0 || !strings.Contains(refused.standardError, "names kotlin, which it does not take") {
			test.Errorf("exit %d:\n%s", refused.exitCode, refused.standardError)
		}
	})
	// Not parallel: the subtests share one tree, whose cache each run writes and the next may replay.
	test.Run("a named language nothing here is in is said", func(test *testing.T) {
		checked := run(test, "--no-fix", "--json", "--languages", "go", "--formats", "css")
		if checked.exitCode != 0 || !strings.Contains(checked.standardError,
			"This run names css, and nothing here is any of them, so it checked nothing of them") ||
			!slices.Equal(checked.record(test, "formats").NotFound, []string{"css"}) {
			test.Errorf("exit %d:\n%s", checked.exitCode, checked.standardError)
		}
		refused := run(test, "--no-fix", "--formats", "css,yaml")
		if refused.exitCode == 0 || !strings.Contains(refused.standardError,
			"This run names yaml and css, and nothing here is any of them: here is go, typescript, swift, json and "+
				"markdown") {
			test.Errorf("a run naming only languages nothing here is in exited %d:\n%s", refused.exitCode,
				refused.standardError)
		}
	})
}

// A project's run under `--languages` is that project's plain run, so it replays the plain run's record, while the
// run that named languages, which never replays, names what it left out each time, and the plain run after it names
// nothing left out (#s7gt4bj). So a narrowed run never reads as a whole one, nor a whole one as narrowed.
func TestANarrowedRunNamesWhatItLeftOutWhileItsProjectReplays(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	fixture, absoluteError := filepath.Abs(filepath.Join("..", "..", "swift", "Contract", "Clean.jsonl"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	swiftEngine := filepath.Join(test.TempDir(), "cohere-swift")
	if writeError := executable_testing.WriteFile(swiftEngine, []byte("#!/bin/sh\ncat '"+fixture+"'\n"),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	writeTree(test, root, map[string]string{
		".gitignore":          ".cache/\n",
		goModuleMarker:        "module example.com/replayed\n\ngo 1.27\n",
		"alpha/alpha.go":      "// Package alpha is one.\npackage alpha\n\n// MaxSize is a size.\nconst MaxSize = 1\n",
		"swift/Package.swift": "// swift-tools-version: 6.0\n",
	})
	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if initializeOutput, initializeError := initialize.CombinedOutput(); initializeError != nil {
		test.Fatalf("git init: %v\n%s", initializeError, initializeOutput)
	}
	environment := []string{"COHERE_SWIFT_ENGINE=" + swiftEngine, goEngineChildVariable + "="}
	leftOut := "⚠ left out: swift"
	// Twice: the first plain run's Go project is never recorded, since the Swift project's run beside it makes
	// swift/.cache while the Go census reads that directory, which the Go run then refuses to vouch for. The second
	// finds the directory settled and records.
	for range 2 {
		if output, exitCode := runCohereWithEnvironment(test, binary, root, environment, "--no-fix",
			"--verbose"); exitCode != 0 || strings.Contains(output, leftOut) {
			test.Fatalf("the plain run exited %d, want 0 and nothing left out:\n%s", exitCode, output)
		}
	}
	output, exitCode := runCohereWithEnvironment(test, binary, root, environment, "--no-fix", "--verbose",
		"--languages", "go")
	if exitCode != 0 || !strings.Contains(output, leftOut) || !strings.Contains(output, "• replayed") {
		test.Errorf("the run naming go exited %d, want 0, naming swift left out, its Go project replayed:\n%s",
			exitCode, output)
	}
	if output, exitCode := runCohereWithEnvironment(test, binary, root, environment, "--no-fix",
		"--verbose"); exitCode != 0 || strings.Contains(output, leftOut) {
		test.Errorf("the plain run after it exited %d, want 0 and nothing left out:\n%s", exitCode, output)
	}
}

// A repository that is one TypeScript program runs it as one project of several under `--languages`, never in this
// process, so its format walk leaves out what was not named and the footer names it (#s7gt4bj): the program's
// Markdown is neither formatted nor reported under `typescript`, and its tsconfig, which is JSON, is named left out
// beside it.
func TestASingleProgramUnderLanguagesLeavesOutWhatWasNotNamed(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		".gitignore":     ".cache/\n",
		"tsconfig.json":  "{ \"compilerOptions\": { \"strict\": true, \"noEmit\": true }, \"include\": [\"source\"] }\n",
		"source/Tidy.ts": "export const tidy = 1;\n",
		"Steps.md":       "* one\n",
	})
	output, exitCode := runCohere(test, binary, root, "--no-fix", "--languages", "typescript")
	if exitCode != 0 || strings.Contains(output, "Steps.md") ||
		!strings.Contains(output, "⚠ left out: json, markdown") {
		test.Errorf("exit %d, want 0, the Markdown left out and named:\n%s", exitCode, output)
	}
}

// Adamic's files are TypeScript's until Adamic has a checker of its own (#s7gt4bj, the Square's ruling): `typescript`
// type-checks the program's `.a` files, finding the planted type error between two of them, and `adamic` is no name
// `--languages` takes.
func TestAdamicFilesAreCheckedAsTypeScript(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := adamicProject(test, nil)
	output, exitCode := runCohere(test, binary, root, "--no-fix", "--languages", "typescript")
	if exitCode == 0 || !strings.Contains(output, "src/main.a:3:14") ||
		!strings.Contains(output, "⚠ left out: json") {
		test.Errorf("exit %d, want the type error between the .a files found, and only the JSON left out:\n%s",
			exitCode, output)
	}
	if output, exitCode := runCohere(test, binary, root, "--no-fix", "--languages", "adamic"); exitCode == 0 ||
		!strings.Contains(output, "names adamic, which it does not take") {
		test.Errorf("--languages adamic exited %d:\n%s", exitCode, output)
	}
}
