package load

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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
// `GOGCCFLAGS`, which names a build directory of its own each time, is one cohere-go never asks (goEnvironmentNames),
// and every other field matches. A copy whose go.mod differs keys apart.
func TestAKeptGoEnvironmentMovedToAnotherWorkspaceIsWhatGoEnvironmentAnswersThere(test *testing.T) {
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
			editedKey, editedKeyError := goEnvironmentKey(edited, environment)
			if editedKeyError != nil {
				test.Fatal(editedKeyError)
			}
			if editedKey == fromKey {
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
						test.Errorf("go env answers %s otherwise on each ask, and cohere-go asks it", field)
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

// keyEnvironment is the process's environment with the variables a go env key refuses or reads taken out, then
// `GOENV=off` and added, so a case's key turns on what the case names and never on the machine running it.
func keyEnvironment(added ...string) []string {
	var environment []string
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		if name != "GOENV" && name != "GOWORK" && name != "GOFLAGS" {
			environment = append(environment, variable)
		}
	}
	return append(append(environment, "GOENV=off"), added...)
}

// A go env key holds everything the answer turns on (#2htryg8): two workspaces under one key share an answer, so a
// key that left out the environment, the go.work, a go.mod, or where a go.mod above the workspace is, would hand a
// test's run another's go env. Each case changes one of them from a workspace of two modules and wants another key.
func TestAGoEnvironmentKeyMovesWithEverythingTheAnswerTurnsOn(test *testing.T) {
	test.Parallel()

	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	files := goAnswersModules["workspace"]
	plain := filepath.Join(base, "plain", "project")
	writeListingFiles(test, plain, files)
	plainKey, plainError := goEnvironmentKey(plain, keyEnvironment())
	if plainError != nil {
		test.Fatal(plainError)
	}
	for _, each := range []struct {
		name string
		// edited is the one file of the copy written over, by its path below the workspace, empty for none.
		edited   string
		contents string
		added    []string
	}{
		{name: "a variable of the environment", added: []string{"GOTOOLCHAIN=local+path"}},
		{name: "the go flags", added: []string{"GOFLAGS=-mod=mod"}},
		{name: "the go.work's bytes", edited: "go.work", contents: "go 1.24\n\nuse (\n\t./first\n\t./second\n)\n"},
		{name: "a used module's go.mod", edited: "first/go.mod", contents: "module example.com/first\n\ngo 1.23\n"},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			copied := filepath.Join(base, strings.ReplaceAll(each.name, " ", "-"), "project")
			writeListingFiles(test, copied, files)
			if each.edited != "" {
				writeListingFiles(test, copied, map[string]string{each.edited: each.contents})
			}
			key, keyError := goEnvironmentKey(copied, keyEnvironment(each.added...))
			if keyError != nil {
				test.Fatal(keyError)
			}
			if key == plainKey {
				test.Errorf("a workspace that differs by %s reads the plain workspace's key", each.name)
			}
		})
	}
	test.Run("a module a go.work above the workspace uses", func(test *testing.T) {
		test.Parallel()
		// go resolves a use from the go.work's own directory, so the key reads the used module's go.mod there, never
		// below the workspace, where none is and an edit would move nothing.
		parent := filepath.Join(base, "work-above")
		writeListingFiles(test, parent, map[string]string{
			"go.work":        "go 1.24\n\nuse (\n\t./first\n\t./inner\n)\n",
			"first/go.mod":   "module example.com/first\n\ngo 1.24\n",
			"inner/go.mod":   "module example.com/inner\n\ngo 1.24\n",
			"inner/inner.go": "package inner\n",
		})
		inner := filepath.Join(parent, "inner")
		before, beforeError := goEnvironmentKey(inner, keyEnvironment())
		writeListingFiles(test, parent, map[string]string{"first/go.mod": "module example.com/first\n\ngo 1.23\n"})
		after, afterError := goEnvironmentKey(inner, keyEnvironment())
		if beforeError != nil || afterError != nil {
			test.Fatalf("want a key before and after the edit, got %v and %v", beforeError, afterError)
		}
		if before == after {
			test.Error("an edit to the go.mod of a module the go.work above uses left the key as it was")
		}
	})
	test.Run("a go.mod above the workspace", func(test *testing.T) {
		test.Parallel()
		// The same bytes at the same path above two workspaces: go finds each one's own, so neither answer moves.
		var keys []string
		for _, parent := range []string{"above-first", "above-second"} {
			writeListingFiles(test, filepath.Join(base, parent), map[string]string{
				"go.mod":         "module example.com/above\n\ngo 1.24\n",
				"inner/inner.go": "package inner\n",
			})
			key, keyError := goEnvironmentKey(filepath.Join(base, parent, "inner"), keyEnvironment())
			if keyError != nil {
				test.Fatal(keyError)
			}
			keys = append(keys, key)
		}
		if keys[0] == keys[1] {
			test.Error("two workspaces below go.mod files of their own, each above its workspace, read one key")
		}
	})
}

// An environment whose answer turns on a file the key holds no bytes of gets no key, and its go env is asked every
// time (#2htryg8): a go env file the person's, a go.work the environment names, a go.mod the go flags name.
// `GOWORK=off` names no file, and keys.
func TestAnEnvironmentThatReadsAFileNoKeyHoldsIsAskedEveryTime(test *testing.T) {
	test.Parallel()

	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	workspace := filepath.Join(base, "project")
	writeListingFiles(test, workspace, goAnswersModules["module"])
	withoutOff := slices.DeleteFunc(keyEnvironment(), func(variable string) bool { return variable == "GOENV=off" })
	for name, environment := range map[string][]string{
		"no GOENV":           withoutOff,
		"a go env file":      append(slices.Clone(withoutOff), "GOENV="+filepath.Join(base, "go.env")),
		"a go.work named":    keyEnvironment("GOWORK=" + filepath.Join(base, "go.work")),
		"a -modfile go flag": keyEnvironment("GOFLAGS=-modfile=" + filepath.Join(base, "other.mod")),
	} {
		if key, keyError := goEnvironmentKey(workspace, environment); keyError == nil {
			test.Errorf("under %s, want no key, got one of %d bytes", name, len(key))
		}
		answers := NewGoAnswers()
		asked := 0
		for range 2 {
			_, askError := answers.goEnvironment(workspace, environment, func() (map[string]string, error) {
				asked++
				return map[string]string{"GOOS": "plan9"}, nil
			})
			if askError != nil {
				test.Fatal(askError)
			}
		}
		if asked != 2 {
			test.Errorf("under %s, go env was asked %d times in 2 calls, want every time", name, asked)
		}
	}
	if _, keyError := goEnvironmentKey(workspace, keyEnvironment("GOWORK=off")); keyError != nil {
		test.Errorf("under GOWORK=off, which names no file, want a key, got %v", keyError)
	}
}

// A failed ask is never kept, and a kept answer is never the asker's to change (#2htryg8): go env's answer and the
// toolchain's ports are each asked again after a failure, asked once after a success, and a caller that writes to the
// answer it was handed, as a test's hook does, leaves the next asker's as go env gave it. Another key asks for itself.
func TestAFailedAskIsNeverKeptAndAKeptAnswerIsNeverTheAskersToChange(test *testing.T) {
	test.Parallel()

	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	workspace := filepath.Join(base, "project")
	writeListingFiles(test, workspace, goAnswersModules["module"])
	answers := NewGoAnswers()
	failed := errors.New("the ask failed")
	asked := 0
	ask := func() (map[string]string, error) {
		asked++
		if asked == 1 {
			return nil, failed
		}
		return map[string]string{"GOMOD": filepath.Join(workspace, "go.mod"), "GOOS": "plan9"}, nil
	}
	if _, askError := answers.goEnvironment(workspace, keyEnvironment(), ask); !errors.Is(askError, failed) {
		test.Fatalf("want the first ask's failure, got %v", askError)
	}
	for range 3 {
		answer, askError := answers.goEnvironment(workspace, keyEnvironment(), ask)
		if askError != nil || answer["GOOS"] != "plan9" || answer["GOMOD"] != filepath.Join(workspace, "go.mod") {
			test.Fatalf("want go env's answer as it gave it, got %v and %v", answer, askError)
		}
		// Written to as a test's hook writes to the answer it is given.
		answer["GOOS"] = "changed"
	}
	if asked != 2 {
		test.Errorf("go env was asked %d times, want 2: once failing, kept by nothing, and once kept", asked)
	}

	listed := 0
	list := func() ([]byte, error) {
		listed++
		if listed == 1 {
			return nil, failed
		}
		return []byte("ports"), nil
	}
	if _, listError := answers.Ports("toolchain", list); !errors.Is(listError, failed) {
		test.Fatalf("want the first listing's failure, got %v", listError)
	}
	for _, key := range []string{"toolchain", "toolchain", "another toolchain"} {
		if output, listError := answers.Ports(key, list); listError != nil || string(output) != "ports" {
			test.Fatalf("want the ports under %q, got %q and %v", key, output, listError)
		}
	}
	if listed != 3 {
		test.Errorf("the ports were listed %d times, want 3: once failing, once kept, and once under another key",
			listed)
	}
}

// A kept answer moved to another workspace moves the two values go makes from where the workspace is, and no other
// (#2htryg8): `GOMOD` and `GOWORK` name the asker's files, while a build cache or a compiler the environment points
// below the first workspace is that same path for every asker under the key, as go env answers it.
func TestAMovedAnswerMovesOnlyItsGoModAndItsGoWork(test *testing.T) {
	test.Parallel()

	from, to := filepath.Join("/", "projects", "first"), filepath.Join("/", "projects", "second")
	moved := relocated(map[string]string{
		"GOMOD":   filepath.Join(from, "go.mod"),
		"GOWORK":  filepath.Join(from, "go.work"),
		"GOCACHE": filepath.Join(from, "cache"),
		"CC":      filepath.Join(from, "tools", "cc"),
		"GOPATH":  from,
		"GOOS":    "plan9",
	}, from, to)
	want := map[string]string{
		"GOMOD":   filepath.Join(to, "go.mod"),
		"GOWORK":  filepath.Join(to, "go.work"),
		"GOCACHE": filepath.Join(from, "cache"),
		"CC":      filepath.Join(from, "tools", "cc"),
		"GOPATH":  from,
		"GOOS":    "plan9",
	}
	if !maps.Equal(moved, want) {
		test.Errorf("moved from %s to %s, the answer reads\n%v\nwant\n%v", from, to, moved, want)
	}
}
