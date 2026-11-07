package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/build_inputs"
)

// A test that builds a binary and judges it reruns after an edit only that binary reaches, under cohere-dev
// test's own environment, once it records the build's inputs (#4p82x8d). Before, Go keyed the result on the test
// binary alone, which the edit left byte-identical, and replayed the pass: a mutant of command/cohere's
// json_output.go read `ok (cached)` on 2026-11-07.
//
// The fixture is a module of three packages: a tool that prints a word, the word, which only the tool imports,
// and a check whose test builds the tool, runs it and wants the word. The check records the tool's build through
// a copy of internal/build_inputs, read from the real file, so a mutant of the real file is a mutant here.
func TestAnEditOnlyABuiltBinaryReachesMissesTheTestCache(test *testing.T) {
	test.Parallel()
	recorder, readError := os.ReadFile(filepath.Join("..", "..", "internal", "build_inputs", "build_inputs.go"))
	if readError != nil {
		test.Fatal(readError)
	}
	module := test.TempDir()
	// The nonce makes this run's check a test binary no earlier run built, so its first run cannot be a replay
	// of one: with -trimpath the fixture's binary is otherwise the same in every temporary directory.
	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
	files := map[string]string{
		"go.mod":                 "module probe\n\ngo 1.27\n",
		"inputs/build_inputs.go": string(recorder),
		"tool/main.go":           "package main\n\nimport (\n\t\"fmt\"\n\n\t\"probe/word\"\n)\n\nfunc main() {\n\tfmt.Print(word.Word)\n}\n",
		"word/word.go":           "package word\n\nconst Word = \"before\"\n",
		"check/check_test.go":    checkTest(nonce),
	}
	hourAgo := time.Now().Add(-time.Hour)
	for name, content := range files {
		writeAged(test, filepath.Join(module, name), content, hourAgo)
	}
	// Go caches no result that opened a file changed in the last two seconds (modTimeCutoff in
	// cmd/go/internal/test), so the fixture's directories and files are an hour old.
	for _, directory := range []string{".", "inputs", "tool", "word", "check"} {
		if changeTimesError := os.Chtimes(filepath.Join(module, directory), hourAgo, hourAgo); changeTimesError != nil {
			test.Fatal(changeTimesError)
		}
	}
	environment := append(
		withoutVcsStamping(budgetEnvironment(os.Environ(), 1, poolShape{tokens: 1, packages: 2, threads: 2})),
		"GOWORK=off",
	)
	run := func() (string, bool) {
		command := exec.Command("go", "test", "./check")
		command.Dir = build_inputs.OutsideModule(test, module)
		command.Env = environment
		output, runError := command.CombinedOutput()
		return string(output), runError == nil
	}

	if output, passed := run(); !passed || strings.Contains(output, "(cached)") {
		test.Fatalf("the fixture's first run did not run and pass:\n%s", output)
	}
	// Proof the cache is live for this fixture, so the miss below is the edit's doing.
	if output, passed := run(); !passed || !strings.Contains(output, "(cached)") {
		test.Fatalf("the fixture's second run, nothing changed, was not a cached pass:\n%s", output)
	}
	writeAged(test, filepath.Join(module, "word", "word.go"), "package word\n\nconst Word = \"after\"\n",
		hourAgo.Add(30*time.Minute))
	output, passed := run()
	if strings.Contains(output, "(cached)") {
		test.Fatalf("after an edit only the built tool reaches, the check replayed its pass:\n%s", output)
	}
	if passed || !strings.Contains(output, `"after"`) {
		test.Errorf("after the edit the check ran but did not fail on the tool's new word:\n%s", output)
	}
}

// checkTest is the fixture's test: build the tool, run it, want "before". nonce goes into the test binary.
func checkTest(nonce string) string {
	return `package check

import (
	"os/exec"
	"path/filepath"
	"testing"

	build_inputs "probe/inputs"
)

const nonce = "` + nonce + `"

func TestTheToolPrintsItsWord(test *testing.T) {
	test.Log(nonce)
	build_inputs.Record(test, "..", "./tool")
	binary := filepath.Join(test.TempDir(), "tool")
	build := exec.Command("go", "build", "-o", binary, "./tool")
	build.Dir = ".."
	if output, buildError := build.CombinedOutput(); buildError != nil {
		test.Fatalf("%v\n%s", buildError, output)
	}
	output, runError := exec.Command(binary).Output()
	if runError != nil {
		test.Fatal(runError)
	}
	if string(output) != "before" {
		test.Fatalf("the tool printed %q", output)
	}
}
`
}

// writeAged writes a file, making its directory, and sets its times to modified.
func writeAged(test *testing.T, path string, content string, modified time.Time) {
	test.Helper()
	if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(path, []byte(content), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if changeTimesError := os.Chtimes(path, modified, modified); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
}
