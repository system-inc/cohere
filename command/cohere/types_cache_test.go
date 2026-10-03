package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A warm run's type diagnostics are a cold run's, through every kind of edit: one inside a body, which
// re-checks only the edited file; one to an export, which must re-check the files importing it; and a type
// error in the edited file itself, each introduced and then removed (#zqsdzbq).
//
// b.ts imports a.ts and five other files import nothing, so a body edit to a.ts leaves the other six
// replayable, and an export edit leaves only the five. Enough of them that re-checking a.ts and b.ts is
// under half the project, where the full check would take over.
func TestTypeDiagnosticsReplayOnlyWhatAnEditCannotReach(t *testing.T) {
	binary := buildCohere(t)
	root := t.TempDir()
	write := func(name string, contents string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tsconfig.json", `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler"},"include":["source"]}`)
	write("CohereSettings.json", `{"rules":{"no-debugger":"error"}}`)
	write("package.json", `{"name":"fixture","private":true,"type":"module"}`)
	write(".gitignore", ".cache/\nnode_modules/\n")
	const original = "export function value(): number {\n    return 1;\n}\n"
	write("source/a.ts", original)
	write("source/b.ts", "import { value } from './a';\nexport const b: number = value();\n")
	for index := range 5 {
		write(fmt.Sprintf("source/c%d.ts", index), fmt.Sprintf("export const c%d: number = %d;\n", index, index))
	}

	typeError := regexp.MustCompile(`(?m)^\S+:\d+:\d+ - error TS\d+: .*$`)
	replayed := regexp.MustCompile(`; (\d+) of 7 files' semantic diagnostics replayed from cache`)
	run := func(arguments ...string) string {
		t.Helper()
		output, _ := runCohere(t, binary, root, append([]string{"--no-fix"}, arguments...)...)
		return output
	}
	run()

	for _, step := range []struct {
		name         string
		aTS          string
		wantErrors   int
		mostReplayed int
	}{
		{"a body edit", "export function value(): number {\n    return 2;\n}\n", 0, 6},
		{"an export edit its importer cannot take", "export function value(): string {\n    return 'two';\n}\n", 1, 5},
		{"the export edit undone", original, 0, 5},
		{"a type error in the edited file", "export function value(): number {\n    return 'one';\n}\n", 1, 6},
		{"the type error removed", original, 0, 6},
	} {
		write("source/a.ts", step.aTS)
		warm, cold := run(), run("--no-cache")
		warmErrors, coldErrors := typeError.FindAllString(warm, -1), typeError.FindAllString(cold, -1)
		if strings.Join(warmErrors, "\n") != strings.Join(coldErrors, "\n") || len(coldErrors) != step.wantErrors {
			t.Fatalf("%s: warm reports %d type errors and cold %d, want %d\n--- warm\n%s\n--- cold\n%s",
				step.name, len(warmErrors), len(coldErrors), step.wantErrors, warm, cold)
		}
		if strings.HasPrefix(warm, "cached: ") {
			t.Fatalf("%s: the run after the edit replayed the run before it:\n%s", step.name, warm)
		}
		match := replayed.FindStringSubmatch(warm)
		if match == nil {
			t.Fatalf("%s: the warm run replayed no file's semantic diagnostics, so it proves nothing about replaying:\n%s", step.name, warm)
		}
		if count, _ := strconv.Atoi(match[1]); count > step.mostReplayed {
			t.Fatalf("%s: %d files replayed, at most %d may: an edit that reaches an importer must re-check it\n%s",
				step.name, count, step.mostReplayed, warm)
		}
	}
}
