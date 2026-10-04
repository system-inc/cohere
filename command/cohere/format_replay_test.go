package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestANoFixFormatRunReplaysUntilWhatItsWalkReadMoves holds the replay of a check that includes formatting
// (#13a63n3): `--no-fix --format` replays an unchanged tree, and every way a tree can change what the format
// phase reports breaks the replay and reports the change.
//
// Each change is one the graph build never sees, which is the point: a file outside the program, a file
// added to a directory holding nothing the build reads, a .gitignore edit that lets a file into the walk,
// and a file inside a nested repository. A replay that recorded only the program's inputs would print the
// old clean verdict over each, so each is checked to report its file, and each is undone and replayed
// again so the next change starts from a replaying tree.
func TestANoFixFormatRunReplaysUntilWhatItsWalkReadMoves(t *testing.T) {
	binary := buildCohere(t)
	home := t.TempDir()
	root := t.TempDir()
	// The program is the root's own TypeScript, so the build lists no subdirectory: quiet/ is a directory only
	// the format walk lists, as most of a real tree's are.
	writeTree(t, root, map[string]string{
		"tsconfig.json":       strings.Replace(fixScopeTsconfig, `"**/*.ts"`, `"*.ts"`, 1),
		"CohereSettings.json": "{ \"extends\": \"./NexusCohereSettings.json\", \"rules\": {} }\n",
		// The shared tsconfig is written in four-space JSON, which the formatter would rewrite.
		"NexusCohereSettings.json": "{ \"format\": { \"ignore\": [\"tsconfig.json\"] } }\n",
		".gitignore":               ".cache/\nignored/\n",
		"Tidy.ts":                  "export const tidy = 1;\n",
		"data.json":                "{ \"a\": 1 }\n",
		"quiet/readme.txt":         "nothing here is formatted\n",
		"ignored/loose.json":       "{ \"b\":   2 }\n",
		".gitmodules":              "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
		"library/.git":             "gitdir: ../.git/modules/library\n",
		"library/inner.json":       "{ \"c\": 3 }\n",
	})
	run := func() (string, int) {
		t.Helper()
		command := exec.Command(binary, "--verbose", "--no-fix", "--format")
		command.Dir = root
		command.Env = append(os.Environ(), "HOME="+home, "XDG_CACHE_HOME=")
		output, err := command.CombinedOutput()
		if exitError, isExit := err.(*exec.ExitError); isExit {
			return string(output), exitError.ExitCode()
		}
		if err != nil {
			t.Fatalf("running cohere: %v\n%s", err, output)
		}
		return string(output), 0
	}
	replays := func(when string) {
		t.Helper()
		run()
		if output, code := run(); !strings.HasPrefix(output, "cached: ") || code != 0 {
			t.Fatalf("%s: a clean unchanged tree did not replay (exit %d):\n%s", when, code, output)
		}
	}
	reports := func(when string, fileName string) {
		t.Helper()
		output, code := run()
		if strings.HasPrefix(output, "cached: ") {
			t.Fatalf("%s: the run replayed the clean verdict over a tree that changed:\n%s", when, output)
		}
		if code == 0 || !strings.Contains(output, fileName) {
			t.Fatalf("%s: the run did not report %s (exit %d):\n%s", when, fileName, code, output)
		}
	}
	write := func(name string, contents string) {
		t.Helper()
		writeTree(t, root, map[string]string{name: contents})
	}

	replays("before any change")

	write("data.json", "{ \"a\":  1 }\n")
	reports("a one-byte whitespace edit to a file outside the program", "data.json")
	write("data.json", "{ \"a\": 1 }\n")
	replays("with the edit undone")

	write("quiet/added.json", "{ \"d\":   4 }\n")
	reports("an unformatted file added where the build reads nothing", "added.json")
	if err := os.Remove(filepath.Join(root, "quiet", "added.json")); err != nil {
		t.Fatal(err)
	}
	replays("with the added file removed")

	write(".gitignore", ".cache/\n")
	reports("a .gitignore edit that lets an unformatted file into the walk", "loose.json")
	write(".gitignore", ".cache/\nignored/\n")
	replays("with the .gitignore restored")

	write("library/inner.json", "{ \"c\":  3 }\n")
	reports("a whitespace edit inside a nested repository", "inner.json")
}
