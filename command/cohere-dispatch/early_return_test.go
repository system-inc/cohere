//go:build !windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// A run that returns early gives its caller exactly what a run that waits gives: every byte of stdout and
// stderr and the exit code, on a clean tree, a tree with findings, one with a type error, and a run whose
// fixes rewrite a file. Only what the caller does not wait for moves: the engine's cache write, which this
// also waits for and reads back (#zqsdzbq, early return).
func TestReturningEarlyChangesNothingTheCallerSees(t *testing.T) {
	binaries := t.TempDir()
	build := func(output string, packagePath string) string {
		binary := filepath.Join(binaries, output)
		if combined, err := exec.Command("go", "build", "-o", binary, packagePath).CombinedOutput(); err != nil {
			t.Fatalf("building %s: %v\n%s", packagePath, err, combined)
		}
		return binary
	}
	dispatcher := build("cohere-dispatch", ".")
	engine := build("cohere", "../cohere")

	settings := `{"extends":"./NexusCohereSettings.json","rules":{"no-debugger":"error"}}`
	tsconfig := `{"compilerOptions":{"target":"ES2022","module":"esnext","moduleResolution":"bundler","strict":true,"noEmit":true},"include":["**/*.ts"]}`
	scenarios := []struct {
		name      string
		files     map[string]string
		arguments []string
	}{
		{"clean", map[string]string{"Clean.ts": "export const clean = 1;\n"}, []string{"--no-fix"}},
		{"findings", map[string]string{"Producer.ts": "export function value(): number {\n    debugger;\n    return 1;\n}\n"}, []string{"--no-fix"}},
		{"a type error", map[string]string{"Broken.ts": "export const broken: number = \"text\";\n"}, []string{"--no-fix"}},
		{"a rewrite", map[string]string{"Producer.ts": "export function value(): number {\n    debugger;\n    return 1;\n}\n"}, nil},
	}
	durations := regexp.MustCompile(`\d+(\.\d+)?(ms|s|µs| GB)\b`)
	clock := regexp.MustCompile(`\d\d:\d\d:\d\d`)
	invocationLines := regexp.MustCompile(`(?m)^ *(total|memory:) .*\n`)

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			type outcome struct {
				stdout, stderr string
				code           int
				root           string
			}
			run := func(wait bool) outcome {
				root := t.TempDir()
				files := map[string]string{"tsconfig.json": tsconfig, "CohereSettings.json": settings, "NexusCohereSettings.json": `{"format":{}}`}
				for name, contents := range scenario.files {
					files[name] = contents
				}
				for name, contents := range files {
					if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				command := exec.Command(dispatcher, scenario.arguments...)
				command.Dir = root
				command.Env = append(os.Environ(), "COHERE_BINARY="+engine, "CI=", "COHERE_WAIT=")
				if wait {
					command.Env = append(command.Env, "COHERE_WAIT=1")
				}
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				code := 0
				if err := command.Run(); err != nil {
					exitError, isExit := err.(*exec.ExitError)
					if !isExit {
						t.Fatalf("running the dispatcher: %v", err)
					}
					code = exitError.ExitCode()
				}
				normalize := func(text string) string {
					text = invocationLines.ReplaceAllString(text, "")
					text = bytes.NewBufferString(text).String()
					resolved, _ := filepath.EvalSymlinks(root)
					text = regexp.MustCompile(regexp.QuoteMeta(resolved)).ReplaceAllString(text, "ROOT")
					text = regexp.MustCompile(regexp.QuoteMeta(root)).ReplaceAllString(text, "ROOT")
					return clock.ReplaceAllString(durations.ReplaceAllString(text, "D"), "T")
				}
				return outcome{normalize(stdout.String()), normalize(stderr.String()), code, root}
			}

			waited := run(true)
			early := run(false)
			if early.stdout != waited.stdout || early.stderr != waited.stderr || early.code != waited.code {
				t.Fatalf("returning early changed what the caller sees:\n--- waited (exit %d)\n%s\n%s\n--- early (exit %d)\n%s\n%s",
					waited.code, waited.stdout, waited.stderr, early.code, early.stdout, early.stderr)
			}

			// The engine finishes alone after an early return, and what it finishes is the cache table.
			table := filepath.Join(early.root, ".cache", "cohere", "table.gob")
			deadline := time.Now().Add(30 * time.Second)
			for {
				if _, err := os.Stat(table); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the engine that returned early never wrote its cache table")
				}
				time.Sleep(20 * time.Millisecond)
			}
			if _, err := os.Stat(filepath.Join(early.root, ".cache", "cohere", "notes-after-return.txt")); err == nil {
				t.Error("the engine left a note after returning, so something it did in the background failed")
			}
		})
	}
}
