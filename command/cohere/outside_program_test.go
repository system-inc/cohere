package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestExplainNamedPathsOutsideProgramSaysWhy pins the reason a named-path run that reached no
// program file now gives, one sentence per path.
//
// Before it existed the refusal was the walk's "nothing to walk: the file set is empty (the program
// holds N files)", measured on ahra for `--lint modules/tasks/data/attachments/pt64fq7/score.mjs`,
// a file the tsconfig excludes and git ignores. Nothing said which. Each row is one of the three
// reasons, and the gitignore row asserts git's own file, line and pattern come through.
func TestExplainNamedPathsOutsideProgramSaysWhy(t *testing.T) {
	t.Parallel()

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
	write(".gitignore", "node_modules/\nmodules/*/data/\n")
	write("tsconfig.json", `{"include": ["source/**/*.ts"]}`)
	write("modules/tasks/data/score.mjs", "export const score = 1;\n")
	write("scripts/Excluded.ts", "export const excluded = 1;\n")
	write("NOTES.md", "# notes\n")

	initialize := exec.Command("git", "init", "--quiet")
	initialize.Dir = root
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}

	location := projectLocation{
		Root:           root,
		ConfigFileName: filepath.Join(root, "tsconfig.json"),
		ArgumentBase:   root,
	}

	for _, testCase := range []struct {
		name string
		path string
		want string
	}{
		{"excluded by the tsconfig and ignored by git", "modules/tasks/data/score.mjs",
			"nothing to check: none of the named paths is in the program. modules/tasks/data/score.mjs is not one " +
				"of the program's files, because the tsconfig at tsconfig.json leaves it out with its `include` " +
				"and `exclude` lists, and git ignores it too (.gitignore:2 `modules/*/data/`)."},
		{"excluded by the tsconfig alone", "scripts/Excluded.ts",
			"nothing to check: none of the named paths is in the program. scripts/Excluded.ts is not one of the " +
				"program's files, because the tsconfig at tsconfig.json leaves it out with its `include` and " +
				"`exclude` lists."},
		{"not a language a program holds", "NOTES.md",
			"nothing to check: none of the named paths is in the program. NOTES.md is not a TypeScript or " +
				"JavaScript file, so no tsconfig can put it in the program."},
		{"a directory", "modules/tasks/data",
			"nothing to check: none of the named paths is in the program. modules/tasks/data is a directory " +
				"holding no file the program contains, because the tsconfig at tsconfig.json leaves every file " +
				"in it out with its `include` and `exclude` lists, and git ignores it too (.gitignore:2 " +
				"`modules/*/data/`)."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := explainNamedPathsOutsideProgram(location, []string{testCase.path}); got != testCase.want {
				t.Errorf("explanation is\n%q\nwant\n%q", got, testCase.want)
			}
		})
	}
}
