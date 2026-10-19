//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenFlag(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	for _, test := range []struct {
		name, source, settings string
		args                   []string
		paths                  []string
		none                   bool
	}{
		{"types", "export const a: number = 'x';\nexport const b: number = 'y';\n", `{"rules":{}}`, []string{"--types"}, []string{"A.ts", "B.ts"}, false},
		{"missing editor", "export const value: number = 'x';\n", `{"rules":{}}`, []string{"--types"}, []string{"A.ts", "B.ts"}, false},
		{"failed editor", "export const value: number = 'x';\n", `{"rules":{}}`, []string{"--types"}, []string{"A.ts", "B.ts"}, false},
		{"lint", "debugger;\ndebugger;\nexport {};\n", `{"rules":{"no-debugger":"error"}}`, []string{"--lint"}, []string{"A.ts", "B.ts"}, false},
		{"both", "debugger;\nexport const a: number = 'x';\n", `{"rules":{"no-debugger":"error"}}`, []string{"--no-fix"}, []string{"A.ts", "B.ts"}, false},
		{"clean", "export const value = 1;\n", `{"rules":{}}`, []string{"--lint"}, nil, true},
		{"fixed", "debugger;\nexport {};\n", `{"rules":{"no-debugger":"error"}}`, nil, nil, true},
		{"refused", "export {};\n", `{"rules":`, []string{"--lint"}, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{"tsconfig.json": `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022"},"include":["*.ts"]}`, "CohereSettings.json": test.settings, "A.ts": test.source})
			if len(test.paths) > 0 {
				writeTree(t, root, map[string]string{"B.ts": test.source})
			}
			editor := t.TempDir()
			log := filepath.Join(editor, "opened")
			if err := os.WriteFile(filepath.Join(editor, "code"), []byte("#!/bin/sh\nprintf 'invocation\\n' >> \"$OPEN_LOG\"\nprintf '%s\\n' \"$@\" >> \"$OPEN_LOG\"\n"), 0755); err != nil {
				t.Fatal(err)
			}
			if test.name == "missing editor" {
				if err := os.Remove(filepath.Join(editor, "code")); err != nil {
					t.Fatal(err)
				}
			}
			if test.name == "failed editor" {
				if err := os.WriteFile(filepath.Join(editor, "code"), []byte("#!/bin/sh\nexit 42\n"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			run := func(open bool) (string, int) {
				t.Helper()
				args := append([]string{"--directory", root, "--no-format", "--no-cache", "--json"}, test.args...)
				if open {
					args = append(args, "--open")
				}
				cmd := exec.Command(binary, args...)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "PATH="+editor, "OPEN_LOG="+log)
				output, err := cmd.CombinedOutput()
				if err == nil {
					return string(output), 0
				}
				if exited, ok := err.(*exec.ExitError); ok {
					return string(output), exited.ExitCode()
				}
				t.Fatal(err)
				return "", -1
			}
			_, before := run(false)
			// A fixing run consumes its plant; restore it before checking --open.
			writeTree(t, root, map[string]string{"A.ts": test.source})
			if len(test.paths) > 0 {
				writeTree(t, root, map[string]string{"B.ts": test.source})
			}
			output, code := run(true)
			if code != before {
				t.Errorf("exit changed: %d -> %d\n%s", before, code, output)
			}
			if strings.Contains(output, "No findings.") != test.none || (test.none && strings.Count(output, "No findings.\n") != 1) {
				t.Errorf("No findings mismatch, want %v:\n%s", test.none, output)
			}
			var reported []string
			seen := map[string]bool{}
			for _, line := range strings.Split(output, "\n") {
				var finding struct {
					Kind string `json:"kind"`
					Path string `json:"path"`
				}
				if json.Unmarshal([]byte(line), &finding) == nil && finding.Kind == "finding" && finding.Path != "" && !seen[finding.Path] {
					seen[finding.Path] = true
					reported = append(reported, finding.Path)
				}
			}
			var want []string
			for _, path := range test.paths {
				want = append(want, filepath.Join(root, path))
			}
			if test.name == "missing editor" || test.name == "failed editor" {
				want := "cohere: could not open editor: code is not on PATH\n"
				if test.name == "failed editor" {
					want = "cohere: could not open editor: exit status 42\n"
				}
				if strings.Count(output, want) != 1 {
					t.Errorf("want exactly one %q in %s", want, output)
				}
				return
			}
			contents, err := os.ReadFile(log)
			if len(want) == 0 {
				if err == nil {
					t.Errorf("unexpected editor invocation: %s", contents)
				}
			} else {
				expected := "invocation\n--\n" + strings.Join(want, "\n") + "\n"
				if string(contents) != expected {
					t.Errorf("editor got %q (%v), want %q; reported %v\n%s", contents, err, expected, reported, output)
				}
				if !reflect.DeepEqual(reported, want) {
					t.Errorf("fixture reported %v, want %v", reported, want)
				}
			}
		})
	}
}

// The collector's mixed stream includes both phases even though a single TypeScript project
// currently gates lint behind types. The injected launcher also proves no editor runs on emptiness.
func TestOpenReport(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		paths       []string
		checked     bool
		launchError error
		wantPaths   []string
		wantOutput  string
	}{
		{"mixed order and duplicates", []string{"types.ts", "lint.ts", "types.ts", "other.ts", "lint.ts", ""}, true, nil, []string{"types.ts", "lint.ts", "other.ts"}, ""},
		{"none", nil, true, nil, nil, "No findings.\n"},
		{"unchecked", nil, false, nil, nil, ""},
		{"global finding", []string{""}, true, nil, nil, ""},
		{"editor failure", []string{"types.ts"}, true, errors.New("planted failure"), []string{"types.ts"}, "cohere: could not open editor: planted failure\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			report := &openReport{Checked: test.checked}
			for _, path := range test.paths {
				report.add(path)
			}
			var output bytes.Buffer
			var opened []string
			calls := 0
			report.open(&output, func(paths []string) error { calls++; opened = append(opened, paths...); return test.launchError })
			var want []string
			for _, path := range test.wantPaths {
				absolute, err := filepath.Abs(path)
				if err != nil {
					t.Fatal(err)
				}
				want = append(want, absolute)
			}
			wantCalls := 0
			if len(want) > 0 {
				wantCalls = 1
			}
			if !reflect.DeepEqual(opened, want) || calls != wantCalls || output.String() != test.wantOutput {
				t.Fatalf("opened %v in %d calls, output %q; want %v in %d calls, output %q", opened, calls, output.String(), want, wantCalls, test.wantOutput)
			}
		})
	}
}

func TestOpenDiscoveredProjects(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	// cohere reports real paths, and on macOS the temp directory sits behind the /tmp symlink.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{
		"types/tsconfig.json":       `{"compilerOptions":{"strict":true,"noEmit":true},"include":["*.ts"]}`,
		"types/CohereSettings.json": `{"rules":{}}`,
		"types/Types.ts":            "export const value: number = 'x';\n",
		"lint/tsconfig.json":        `{"compilerOptions":{"noEmit":true},"include":["*.ts"]}`,
		"lint/CohereSettings.json":  `{"rules":{"no-debugger":"error"}}`,
		"lint/Lint.ts":              "debugger;\nexport {};\n",
	})
	editor := t.TempDir()
	log := filepath.Join(editor, "opened")
	if err := os.WriteFile(filepath.Join(editor, "code"), []byte("#!/bin/sh\nprintf 'invocation\\n' >> \"$OPEN_LOG\"\nprintf '%s\\n' \"$@\" >> \"$OPEN_LOG\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "--no-fix", "--no-format", "--no-cache", "--json", "--open")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+editor, "OPEN_LOG="+log)
	output, err := cmd.CombinedOutput()
	if exited, ok := err.(*exec.ExitError); !ok || exited.ExitCode() != 1 {
		t.Fatalf("exit %v: %s", err, output)
	}
	contents, err := os.ReadFile(log)
	want := "invocation\n--\n" + filepath.Join(root, "lint", "Lint.ts") + "\n" + filepath.Join(root, "types", "Types.ts") + "\n"
	if err != nil || string(contents) != want {
		t.Fatalf("editor got %q (%v), want %q\n%s", contents, err, want, output)
	}
	if !strings.Contains(string(output), "TS2322") || !strings.Contains(string(output), "no-debugger") {
		t.Fatalf("missing the planted type or lint finding: %s", output)
	}
}

func TestOpenCrash(t *testing.T) {
	t.Parallel()
	binary := filepath.Join(t.TempDir(), "cohere")
	build := exec.Command("go", "build", "-buildvcs=false", "-tags", "cohere_planted_panic", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	root := t.TempDir()
	writeTree(t, root, map[string]string{"tsconfig.json": fixScopeTsconfig, "CohereSettings.json": `{"rules":{"cohere-test/panics-on-marked-file":"error"}}`})
	for _, plant := range []bool{true, false} {
		source := "export {};\n"
		if plant {
			source = "// cohere-test: panic here\n" + source
		}
		writeTree(t, root, map[string]string{"Crash.ts": source})
		cmd := exec.Command(binary, "--directory", root, "--lint", "--no-format", "--no-cache", "--open")
		cmd.Env = append(os.Environ(), "PATH="+t.TempDir())
		output, err := cmd.CombinedOutput()
		if plant {
			if exited, ok := err.(*exec.ExitError); !ok || exited.ExitCode() != 1 || !strings.Contains(string(output), "planted:") {
				t.Fatalf("crash plant: %v\n%s", err, output)
			}
		} else if err != nil {
			t.Fatalf("clean control: %v\n%s", err, output)
		}
		if strings.Contains(string(output), "No findings.") == plant {
			t.Errorf("plant=%v, wrong empty report: %s", plant, output)
		}
	}
}

func TestOpenSwiftArguments(t *testing.T) {
	t.Parallel()
	location := projectLocation{Root: "/work/project"}
	baseline, mode, err := swiftEngineArguments(location, map[string]bool{}, func(string) string { return "true" }, nil)
	if err != nil {
		t.Fatal(err)
	}
	arguments, openMode, err := swiftEngineArguments(location, map[string]bool{"open": true}, func(string) string { return "true" }, nil)
	if err != nil || mode != openMode || !reflect.DeepEqual(arguments, baseline) {
		t.Fatalf("--open must be handled by the Go front door: %v, %v, %v", arguments, openMode, err)
	}
}
