//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/build_inputs"
	"github.com/system-inc/cohere/internal/executable_testing"
)

func TestOpenFlag(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	for _, testCase := range []struct {
		name, source, settings string
		commandArguments       []string
		paths                  []string
		none                   bool
	}{
		{
			"types",
			"export const a: number = 'x';\nexport const b: number = 'y';\n",
			`{"rules":{}}`,
			[]string{"--types"},
			[]string{"A.ts", "B.ts"},
			false,
		},
		{
			"missing editor",
			"export const value: number = 'x';\n",
			`{"rules":{}}`,
			[]string{"--types"},
			[]string{"A.ts", "B.ts"},
			false,
		},
		{
			"failed editor",
			"export const value: number = 'x';\n",
			`{"rules":{}}`,
			[]string{"--types"},
			[]string{"A.ts", "B.ts"},
			false,
		},
		{
			"lint",
			"debugger;\ndebugger;\nexport {};\n",
			`{"rules":{"no-debugger":"error"}}`,
			[]string{"--lint"},
			[]string{"A.ts", "B.ts"},
			false,
		},
		{
			"both",
			"debugger;\nexport const a: number = 'x';\n",
			`{"rules":{"no-debugger":"error"}}`,
			[]string{"--no-fix"},
			[]string{"A.ts", "B.ts"},
			false,
		},
		{"clean", "export const value = 1;\n", `{"rules":{}}`, []string{"--lint"}, nil, true},
		{"fixed", "debugger;\nexport {};\n", `{"rules":{"no-debugger":"error"}}`, nil, nil, true},
		{"refused", "export {};\n", `{"rules":`, []string{"--lint"}, nil, false},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			writeTree(test, root, map[string]string{
				"tsconfig.json":       `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022"},"include":["*.ts"]}`,
				"CohereSettings.json": testCase.settings,
				"A.ts":                testCase.source,
			})
			if len(testCase.paths) > 0 {
				writeTree(test, root, map[string]string{"B.ts": testCase.source})
			}
			editor := test.TempDir()
			log := filepath.Join(editor, "opened")
			if writeError := executable_testing.WriteFile(
				filepath.Join(editor, "code"),
				[]byte("#!/bin/sh\nprintf 'invocation\\n' >> \"$OPEN_LOG\"\nprintf '%s\\n' \"$@\" >> \"$OPEN_LOG\"\n"),
				0o755,
			); writeError != nil {
				test.Fatal(writeError)
			}
			if testCase.name == "missing editor" {
				if removeError := os.Remove(filepath.Join(editor, "code")); removeError != nil {
					test.Fatal(removeError)
				}
			}
			if testCase.name == "failed editor" {
				if writeError := executable_testing.WriteFile(
					filepath.Join(editor, "code"),
					[]byte("#!/bin/sh\nexit 42\n"),
					0o755,
				); writeError != nil {
					test.Fatal(writeError)
				}
			}
			run := func(open bool) (string, int) {
				test.Helper()
				commandArguments := append(
					[]string{"--directory", root, "--no-format", "--no-cache", "--json"},
					testCase.commandArguments...,
				)
				if open {
					commandArguments = append(commandArguments, "--open")
				}
				command := exec.Command(binary, commandArguments...)
				command.Dir = root
				command.Env = append(os.Environ(), "PATH="+editor, "OPEN_LOG="+log)
				output, outputError := command.CombinedOutput()
				if outputError == nil {
					return string(output), 0
				}
				if exited, ok := outputError.(*exec.ExitError); ok {
					return string(output), exited.ExitCode()
				}
				test.Fatal(outputError)
				return "", -1
			}
			_, before := run(false)
			// A fixing run consumes its plant; restore it before checking --open.
			writeTree(test, root, map[string]string{"A.ts": testCase.source})
			if len(testCase.paths) > 0 {
				writeTree(test, root, map[string]string{"B.ts": testCase.source})
			}
			output, code := run(true)
			if code != before {
				test.Errorf("exit changed: %d -> %d\n%s", before, code, output)
			}
			if strings.Contains(output, "No findings.") != testCase.none ||
				(testCase.none && strings.Count(output, "No findings.\n") != 1) {
				test.Errorf("No findings mismatch, want %v:\n%s", testCase.none, output)
			}
			var reported []string
			seen := map[string]bool{}
			for _, line := range strings.Split(output, "\n") {
				var finding struct {
					Kind string `json:"kind"`
					Path string `json:"path"`
				}
				if json.Unmarshal([]byte(line), &finding) == nil &&
					finding.Kind == "finding" &&
					finding.Path != "" &&
					!seen[finding.Path] {
					seen[finding.Path] = true
					reported = append(reported, finding.Path)
				}
			}
			var want []string
			for _, path := range testCase.paths {
				want = append(want, filepath.Join(root, path))
			}
			if testCase.name == "missing editor" || testCase.name == "failed editor" {
				want := "cohere: Could not open editor: code is not on PATH.\n"
				if testCase.name == "failed editor" {
					want = "cohere: Could not open editor: exit status 42.\n"
				}
				if strings.Count(output, want) != 1 {
					test.Errorf("want exactly one %q in %s", want, output)
				}
				return
			}
			contents, readError := os.ReadFile(log)
			if len(want) == 0 {
				if readError == nil {
					test.Errorf("unexpected editor invocation: %s", contents)
				}
			} else {
				expected := "invocation\n--\n" + strings.Join(want, "\n") + "\n"
				if string(contents) != expected {
					test.Errorf(
						"editor got %q (%v), want %q; reported %v\n%s",
						contents,
						readError,
						expected,
						reported,
						output,
					)
				}
				if !reflect.DeepEqual(reported, want) {
					test.Errorf("fixture reported %v, want %v", reported, want)
				}
			}
		})
	}
}

// The collector's mixed stream includes both phases even though a single TypeScript project
// currently gates lint behind types. The injected launcher also proves no editor runs on emptiness.
func TestOpenReport(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name        string
		paths       []string
		checked     bool
		launchError error
		wantPaths   []string
		wantOutput  string
	}{
		{
			"mixed order and duplicates",
			[]string{"types.ts", "lint.ts", "types.ts", "other.ts", "lint.ts", ""},
			true,
			nil,
			[]string{"types.ts", "lint.ts", "other.ts"},
			"",
		},
		{"none", nil, true, nil, nil, "No findings.\n"},
		{"unchecked", nil, false, nil, nil, ""},
		{"global finding", []string{""}, true, nil, nil, ""},
		{
			"editor failure",
			[]string{"types.ts"},
			true,
			errors.New("planted failure"),
			[]string{"types.ts"},
			"cohere: Could not open editor: planted failure.\n",
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			report := &openReport{Checked: testCase.checked}
			for _, path := range testCase.paths {
				report.add(path)
			}
			var output bytes.Buffer
			var opened []string
			calls := 0
			stream := bufio.NewWriter(&output)
			report.open(
				stream,
				func(paths []string) error { calls++; opened = append(opened, paths...); return testCase.launchError },
			)
			flushStream(test, stream)
			var want []string
			for _, path := range testCase.wantPaths {
				absolute, absolutePathError := filepath.Abs(path)
				if absolutePathError != nil {
					test.Fatal(absolutePathError)
				}
				want = append(want, absolute)
			}
			wantCalls := 0
			if len(want) > 0 {
				wantCalls = 1
			}
			if !reflect.DeepEqual(opened, want) || calls != wantCalls || output.String() != testCase.wantOutput {
				test.Fatalf(
					"opened %v in %d calls, output %q; want %v in %d calls, output %q",
					opened,
					calls,
					output.String(),
					want,
					wantCalls,
					testCase.wantOutput,
				)
			}
		})
	}
}

func TestOpenDiscoveredProjects(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	// cohere reports real paths, and on macOS the temp directory sits behind the /tmp symlink.
	root, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	writeTree(test, root, map[string]string{
		"types/tsconfig.json":       `{"compilerOptions":{"strict":true,"noEmit":true},"include":["*.ts"]}`,
		"types/CohereSettings.json": `{"rules":{}}`,
		"types/Types.ts":            "export const value: number = 'x';\n",
		"lint/tsconfig.json":        `{"compilerOptions":{"noEmit":true},"include":["*.ts"]}`,
		"lint/CohereSettings.json":  `{"rules":{"no-debugger":"error"}}`,
		"lint/Lint.ts":              "debugger;\nexport {};\n",
	})
	editor := test.TempDir()
	log := filepath.Join(editor, "opened")
	if writeError := executable_testing.WriteFile(
		filepath.Join(editor, "code"),
		[]byte("#!/bin/sh\nprintf 'invocation\\n' >> \"$OPEN_LOG\"\nprintf '%s\\n' \"$@\" >> \"$OPEN_LOG\"\n"),
		0o755,
	); writeError != nil {
		test.Fatal(writeError)
	}
	command := exec.Command(binary, "--no-fix", "--no-format", "--no-cache", "--json", "--open")
	command.Dir = root
	command.Env = append(os.Environ(), "PATH="+editor, "OPEN_LOG="+log)
	output, outputError := command.CombinedOutput()
	if exited, ok := outputError.(*exec.ExitError); !ok || exited.ExitCode() != 1 {
		test.Fatalf("exit %v: %s", outputError, output)
	}
	contents, readError := os.ReadFile(log)
	want := "invocation\n--\n" +
		filepath.Join(root, "lint", "Lint.ts") +
		"\n" +
		filepath.Join(root, "types", "Types.ts") +
		"\n"
	if readError != nil || string(contents) != want {
		test.Fatalf("editor got %q (%v), want %q\n%s", contents, readError, want, output)
	}
	if !strings.Contains(string(output), "TS2322") || !strings.Contains(string(output), "no-debugger") {
		test.Fatalf("missing the planted type or lint finding: %s", output)
	}
}

func TestOpenCrash(test *testing.T) {
	test.Parallel()
	binary := filepath.Join(test.TempDir(), "cohere")
	build_inputs.Record(test, ".", "-tags", "cohere_planted_panic", ".")
	build := exec.Command("go", "build", "-buildvcs=false", "-tags", "cohere_planted_panic", "-o", binary, ".")
	if output, outputError := build.CombinedOutput(); outputError != nil {
		test.Fatalf("build: %v\n%s", outputError, output)
	}
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":       fixScopeTsconfig,
		"CohereSettings.json": `{"rules":{"cohere-test/panics-on-marked-file":"error"}}`,
	})
	for _, plant := range []bool{true, false} {
		source := "export {};\n"
		if plant {
			source = "// cohere-test: panic here\n" + source
		}
		writeTree(test, root, map[string]string{"Crash.ts": source})
		command := exec.Command(binary, "--directory", root, "--lint", "--no-format", "--no-cache", "--open")
		command.Env = append(os.Environ(), "PATH="+test.TempDir())
		output, outputError := command.CombinedOutput()
		if plant {
			if exited, ok := outputError.(*exec.ExitError); !ok ||
				exited.ExitCode() != 1 ||
				!strings.Contains(string(output), "planted:") {
				test.Fatalf("crash plant: %v\n%s", outputError, output)
			}
		} else if outputError != nil {
			test.Fatalf("clean control: %v\n%s", outputError, output)
		}
		if strings.Contains(string(output), "No findings.") == plant {
			test.Errorf("plant=%v, wrong empty report: %s", plant, output)
		}
	}
}

func TestOpenSwiftArguments(test *testing.T) {
	test.Parallel()
	location := projectLocation{Root: "/work/project"}
	baseline, mode, engineArgumentsError := engineArguments(
		swiftLanguage,
		swiftFlags,
		location,
		map[string]bool{},
		func(string) string { return "true" },
		nil,
	)
	if engineArgumentsError != nil {
		test.Fatal(engineArgumentsError)
	}
	arguments, openMode, engineArgumentsError := engineArguments(
		swiftLanguage,
		swiftFlags,
		location,
		map[string]bool{"open": true},
		func(string) string { return "true" },
		nil,
	)
	if engineArgumentsError != nil || mode != openMode || !reflect.DeepEqual(arguments, baseline) {
		test.Fatalf(
			"--open must be handled by the Go front door: %v, %v, %v",
			arguments,
			openMode,
			engineArgumentsError,
		)
	}
}
