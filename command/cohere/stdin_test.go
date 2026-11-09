package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/native"
)

// runCohereWithStdin runs the binary from a directory with text on stdin, and returns stdout, stderr
// and the exit code separately: stdout is the file's text in this mode, and mixing a note into it would
// corrupt the save.
func runCohereWithStdin(
	test *testing.T,
	binary string,
	directory string,
	stdin string,
	arguments ...string,
) (string, string, int) {
	test.Helper()
	command := exec.Command(binary, arguments...)
	command.Dir = directory
	command.Stdin = strings.NewReader(stdin)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runError := command.Run()
	if runError == nil {
		return stdout.String(), stderr.String(), 0
	}
	exitError, isExit := runError.(*exec.ExitError)
	if !isExit {
		test.Fatalf("running cohere: %v\n%s", runError, stderr.String())
	}
	return stdout.String(), stderr.String(), childExitCode(test, exitError)
}

// TestStdinAnswersWhatTheGateWrites is the editor save's contract: for a buffer at a path, the stdin
// mode prints exactly what `cohere --fix --format` writes when the same text is on disk at that path,
// and writes nothing itself.
//
// The TypeScript buffer differs from the file on disk and carries a fixable finding the disk version
// does not have, so the answer has to come from a graph that holds the buffer: computed from the disk,
// the fix's offsets would land in the wrong text. The markdown buffer has no program behind it, which
// is the format-only path, and the one the gate itself used to bail on.
func TestStdinAnswersWhatTheGateWrites(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)

	for _, testCase := range []struct {
		name     string
		buffer   string
		contains string
	}{
		{
			name:     "Producer.ts",
			buffer:   "export function value(): number {\n  const   first = 1\n  debugger;\n    return first }\n",
			contains: "const first = 1;\n",
		},
		{
			name:     "Notes.md",
			buffer:   "Title\n=====\n\n*  one\n*  two\n",
			contains: "- one\n- two\n",
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			fixScopeProject(test, root, map[string]string{"Notes.md": "# Notes\n"})
			path := filepath.Join(root, testCase.name)
			onDisk := readForTest(test, path)

			arguments := []string{"--fix", "--format"}
			saved, stderr, code := runCohereWithStdin(test, binary, root, testCase.buffer,
				append(arguments, "--stdin-filepath", testCase.name)...)
			if code != 0 {
				test.Fatalf("exit %d:\n%s", code, stderr)
			}
			if after := readForTest(test, path); after != onDisk {
				test.Fatalf("the stdin mode wrote to disk:\n%s", after)
			}
			if !strings.Contains(saved, testCase.contains) || strings.Contains(saved, "debugger") {
				test.Fatalf("the buffer was not fixed and formatted, wanted %q in:\n%s", testCase.contains, saved)
			}

			writeTree(test, root, map[string]string{testCase.name: testCase.buffer})
			output, gateCode := runCohere(test, binary, root, append(arguments, testCase.name)...)
			if gateCode != 0 {
				test.Fatalf("the gate exited %d:\n%s", gateCode, output)
			}
			if written := readForTest(test, path); written != saved {
				test.Fatalf("the save and the gate disagree\n--- gate\n%s--- save\n%s", written, saved)
			}
		})
	}
}

// TestStdinDeclinesWithoutAComplaint covers the two quiet answers: a buffer that does not parse comes
// back as typed, and a file type nothing formats comes back as typed. A save is not where a typo is
// reported, and a file cohere does not handle saves untouched.
func TestStdinDeclinesWithoutAComplaint(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	fixScopeProject(test, root, map[string]string{"Data.txt": "plain\n"})

	for name, buffer := range map[string]string{
		"Producer.ts": "export function value(: number {\n",
		"Data.txt":    "plain   text\n",
	} {
		saved, stderr, code := runCohereWithStdin(test, binary, root, buffer,
			"--fix", "--format", "--stdin-filepath", name)
		if code != 0 || saved != buffer {
			test.Errorf("%s: exit %d, wanted the buffer back unchanged:\n%s\nstderr:\n%s", name, code, saved, stderr)
		}
	}
}

// TestStdinNeedsFix: the mode answers what --fix writes, so without --fix there is no question.
func TestStdinNeedsFix(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	fixScopeProject(test, root, nil)
	for _, arguments := range [][]string{
		{"--format", "--stdin-filepath", "Producer.ts"},
		{"--fix", "--no-fix", "--stdin-filepath", "Producer.ts"},
		{"--fix", "--stdin-filepath", "Producer.ts", "Sibling.ts"},
	} {
		if saved, stderr, code := runCohereWithStdin(
			test,
			binary,
			root,
			"",
			arguments...,
		); code == 0 || saved != "" || stderr == "" {
			test.Errorf("%v: exit %d, wanted a refusal on stderr and nothing on stdout:\n%s", arguments, code, stderr)
		}
	}
}

// TestTheEditorSavesWhatStdinFormats holds the VS Code extension's list (editors/vscode/cohere-save.json) to
// this binary, so the two cannot drift. The extension registers its formatter for the extensions listed,
// and they must be exactly the ones the stdin mode formats: it decides by the file name's extension, through
// native's printers, and a file type left off would never format on save while one added that cohere does
// not format would be handed to it for nothing. Go is not among them until #qtna8m4 gives it a stdin path,
// and this test is widened then to read Go's.
//
// The refusal is the start of what the front door says when the project a file is in has no stdin path,
// which the extension takes as a decline rather than an error, so it must begin the sentence every engine
// without one prints.
//
// Read from Go, not from node: native's registry is the set, and reading it here needs nothing but the file.
func TestTheEditorSavesWhatStdinFormats(test *testing.T) {
	test.Parallel()
	contents, readError := os.ReadFile(filepath.Join("..", "..", "editors", "vscode", "cohere-save.json"))
	if readError != nil {
		test.Fatal(readError)
	}
	var save struct {
		Extensions []string `json:"extensions"`
		Refusal    string   `json:"refusal"`
	}
	if unmarshalError := json.Unmarshal(contents, &save); unmarshalError != nil {
		test.Fatal(unmarshalError)
	}

	if formatted := native.Extensions(); !slices.Equal(save.Extensions, formatted) {
		test.Errorf("editors/vscode/cohere-save.json lists %q, and --stdin-filepath formats %q",
			save.Extensions, formatted)
	}

	if save.Refusal == "" {
		test.Fatal("editors/vscode/cohere-save.json names no refusal")
	}
	given := map[string]bool{"fix": true, "format": true, "stdin-filepath": true}
	value := func(name string) string {
		if name == "stdin-filepath" {
			return "/work/project/Probe.ts"
		}
		return "true"
	}
	for _, engine := range []struct {
		language engineLanguage
		flags    engineFlags
	}{
		{goLanguage("module"), goFlags},
		{swiftLanguage, swiftFlags},
	} {
		location := projectLocation{Root: "/work/project", ArgumentBase: "/work/project"}
		_, _, engineArgumentsError := engineArguments(engine.language, engine.flags, location, given, value, nil)
		if engineArgumentsError == nil || !strings.HasPrefix(engineArgumentsError.Error(), save.Refusal) {
			test.Errorf("%s's refusal of a save does not begin %q: %v",
				engine.language.Name, save.Refusal, engineArgumentsError)
		}
	}
}
