package load

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// goAnswersModules are the workspaces a kept go env answer is moved between, by name, each file by its path below the
// workspace: a module alone, and a go.work using two modules below it, one of which reads a godebug line.
var goAnswersModules = map[string]map[string]string{
	"module": {
		"go.mod":   "module example.com/alone\n\ngo 1.24\n",
		"alone.go": "package alone\n",
	},
	"workspace": {
		"go.work":            "go 1.24\n\nuse (\n\t./first\n\t./second\n)\n\ngodebug panicnil=1\n",
		"first/go.mod":       "module example.com/first\n\ngo 1.24\n",
		"first/first.go":     "package first\n",
		"second/go.mod":      "module example.com/second\n\ngo 1.23\n",
		"second/second.go":   "package second\n",
		"second/nested/a.go": "package nested\n",
	},
}

// A go env answer kept for one workspace and moved to another holding the same files under the same key is, field by
// field, the whole `go env -json` go env answers there (#2htryg8): over a module and over a go.work, from a copy to
// another copy deeper below another directory. A field go env answers otherwise on each ask in one place, as
// `GOGCCFLAGS`, which names a build directory of its own each time, is one the engine never asks (goEnvironmentNames),
// and every other field matches. A copy whose go.mod differs keys apart.
func TestAKeptGoEnvironmentMovedToAnotherWorkspaceIsWhatGoEnvAnswersThere(test *testing.T) {
	test.Parallel()

	for name, files := range goAnswersModules {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			base, resolveError := filepath.EvalSymlinks(test.TempDir())
			if resolveError != nil {
				test.Fatal(resolveError)
			}
			from := filepath.Join(base, "project")
			to := filepath.Join(base, "deeper", "below", "project")
			writeListingFiles(test, from, files)
			writeListingFiles(test, to, files)
			environment := append(os.Environ(), "GOENV=off")
			fromKey, fromKeyError := goEnvironmentKey(from, environment)
			toKey, toKeyError := goEnvironmentKey(to, environment)
			if fromKeyError != nil || toKeyError != nil || fromKey != toKey {
				test.Fatalf("want the two copies under one key, got errors %v and %v, keys equal %t", fromKeyError,
					toKeyError, fromKey == toKey)
			}
			edited := filepath.Join(base, "edited", "project")
			writeListingFiles(test, edited, files)
			moduleFile := filepath.Join(edited, "go.mod")
			if name == "workspace" {
				moduleFile = filepath.Join(edited, "second", "go.mod")
			}
			if writeError := os.WriteFile(moduleFile, []byte("module example.com/edited\n\ngo 1.22\n"),
				0o644); writeError != nil {
				test.Fatal(writeError)
			}
			if editedKey, _ := goEnvironmentKey(edited, environment); editedKey == fromKey {
				test.Error("a copy whose go.mod differs reads the same key")
			}

			first := wholeGoEnvironment(test, from, environment)
			again := wholeGoEnvironment(test, from, environment)
			there := wholeGoEnvironment(test, to, environment)
			moved := relocated(first, from, to)
			names := slices.Sorted(maps.Keys(first))
			if !slices.Equal(names, slices.Sorted(maps.Keys(there))) {
				test.Fatalf("go env names\n%q\nthere, want\n%q", slices.Sorted(maps.Keys(there)), names)
			}
			for _, field := range names {
				if first[field] != again[field] {
					if slices.Contains(goEnvironmentNames, field) {
						test.Errorf("go env answers %s otherwise on each ask, and the engine asks it", field)
					}
					continue
				}
				if moved[field] != there[field] {
					test.Errorf("%s moved reads %q, go env answers %q there", field, moved[field], there[field])
				}
			}
		})
	}
}

// wholeGoEnvironment is every field `go env -json` answers in workspace under environment, `PWD` set to it, as
// askGoEnvironment asks.
func wholeGoEnvironment(test *testing.T, workspace string, environment []string) map[string]string {
	test.Helper()
	command := exec.Command("go", "env", "-json")
	command.Dir = workspace
	command.Env = append(slices.Clone(environment), "PWD="+workspace)
	output, runError := command.Output()
	if runError != nil {
		test.Fatalf("go env in %s: %v", workspace, runError)
	}
	answer := map[string]string{}
	if decodeError := json.Unmarshal(output, &answer); decodeError != nil {
		test.Fatalf("decoding go env's answer in %s: %v", workspace, decodeError)
	}
	return answer
}
