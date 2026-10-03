package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every file walked on a checker other than its own finds exactly what its own checker finds: the same
// findings and the same type diagnostics. The walk lets a worker whose group is done take files from
// another group and walk them on its own checker (program.walkQueue); this is the instrument that proves a
// file's answers do not depend on which checker gives them, by forcing every file to be stolen.
//
// A fixture with type-aware findings and one with a type error always run. COHERE_FOREIGN_TREES, a list of
// project roots separated by the platform's list separator, runs the same comparison on real trees.
func TestEveryFileWalkedOnAForeignCheckerFindsTheSame(t *testing.T) {
	binary := buildCohere(t)
	fixture := func(files map[string]string) string {
		root := t.TempDir()
		for name, contents := range files {
			path := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	common := map[string]string{
		"tsconfig.json": `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler"},"include":["source"]}`,
		"CohereSettings.json": `{"rules":{"@typescript-eslint/no-floating-promises":"error","@typescript-eslint/no-unsafe-assignment":"error",` +
			`"@typescript-eslint/no-misused-promises":"error","@typescript-eslint/no-redundant-type-constituents":"error"}}`,
	}
	findingsTree := map[string]string{}
	typeErrorTree := map[string]string{}
	for name, contents := range common {
		findingsTree[name], typeErrorTree[name] = contents, contents
	}
	// Enough files that every checker has a group, each reaching types through an import.
	for index := range 48 {
		name := filepath.Join("source", "f"+string(rune('a'+index%26))+string(rune('a'+index/26))+".ts")
		findingsTree[name] = "import { load } from './shared';\nexport async function run(): Promise<void> {\n    load();\n    const value: any = JSON.parse('1');\n    const kept: number = value;\n    if (load()) {\n        void kept;\n    }\n}\nexport type Either = string | 'literal';\n"
		typeErrorTree[name] = "import { load } from './shared';\nexport const wrong: number = load();\n"
	}
	findingsTree["source/shared.ts"] = "export async function load(): Promise<number> {\n    return 1;\n}\n"
	typeErrorTree["source/shared.ts"] = findingsTree["source/shared.ts"]

	roots := map[string]string{"type-aware findings": fixture(findingsTree), "type errors": fixture(typeErrorTree)}
	for index, root := range filepath.SplitList(os.Getenv("COHERE_FOREIGN_TREES")) {
		roots["tree "+string(rune('1'+index))+" "+filepath.Base(root)] = root
	}

	reported := regexp.MustCompile(`(?m)^\S+:\d+:\d+ - .*$`)
	moved := regexp.MustCompile(`foreign checkers: (\d+) of (\d+) files walked`)
	run := func(t *testing.T, root string, foreign bool) []string {
		t.Helper()
		command := exec.Command(binary, "--no-fix", "--no-cache")
		command.Dir = root
		command.Env = append(os.Environ(), "COHERE_TEST_FOREIGN_CHECKERS=")
		if foreign {
			command.Env = append(command.Env, "COHERE_TEST_FOREIGN_CHECKERS=1")
		}
		output, _ := command.CombinedOutput()
		if foreign {
			// The instrument has to have moved the files, or an identical run proves nothing.
			if match := moved.FindStringSubmatch(string(output)); match == nil || match[1] == "0" || match[1] != match[2] {
				t.Fatalf("the foreign run did not walk every file on a foreign checker (%v):\n%s", match, output)
			}
		}
		return reported.FindAllString(string(output), -1)
	}
	for name, root := range roots {
		t.Run(name, func(t *testing.T) {
			home, foreign := run(t, root, false), run(t, root, true)
			if len(home) == 0 {
				t.Fatalf("the home run reported nothing, so an identical foreign run proves nothing")
			}
			if strings.Join(home, "\n") != strings.Join(foreign, "\n") {
				t.Fatalf("walked on foreign checkers, %d lines differ from %d at home\n--- home\n%s\n--- foreign\n%s",
					len(foreign), len(home), strings.Join(home, "\n"), strings.Join(foreign, "\n"))
			}
			t.Logf("%d findings and type diagnostics, identical on foreign checkers", len(home))
		})
	}
}
