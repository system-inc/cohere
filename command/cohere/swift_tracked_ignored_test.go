//go:build darwin

// The real Swift engine runs only on macOS, where it ships.

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/release/dispatch"
)

// A Swift run replays over an index git rewrote without changing what it decides, and runs again over one that newly
// tracks a file the ignore rules exclude (#jem2gsa), as TestARunReplaysThroughAGitStatusAndNotThroughAGitAddForce
// proves a TypeScript run does. The real engine names the index it read under `trackedIgnored`, so the front door keys
// it on gitignore.TrackedIgnoredDigest over the scope the file set asked rather than on bytes every `git status`
// rewrites. Once with the package at the repository's root, and once below it, scoped to its own directory.
//
// Scratch.swift is a source of the package's one target that `.gitignore` excludes, so the run leaves it out; written
// unformatted, the run reports it once `git add -f` tracks it. `.notes/README.txt` is tracked and read by no check,
// in a hidden directory no walk of the package names a file of, so touching it moves only the index, which
// `git status` rewrites.
//
// It builds the engine when this checkout's Swift sources changed since the last build, which takes minutes, so -short
// leaves it out.
func TestASwiftRunReplaysThroughAGitStatusAndNotThroughAGitAddForce(test *testing.T) {
	test.Parallel()
	if testing.Short() {
		test.Skip("runs the real Swift engine, built from this checkout when its sources changed")
	}
	binary := buildCohere(test)
	environment := []string{
		dispatch.SwiftEngineOverrideVariable + "=" + builtSwiftEngine(test),
		"PATH=" + toolsWithoutGit(test),
	}
	for _, layout := range []struct {
		name      string
		directory string
	}{
		{name: "at the repository's root", directory: ""},
		{name: "below the repository's root", directory: "App"},
	} {
		test.Run(layout.name, func(test *testing.T) {
			test.Parallel()
			repository, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
			if resolveSymlinksError != nil {
				test.Fatal(resolveSymlinksError)
			}
			root := filepath.Join(repository, layout.directory)
			writeTree(test, root, map[string]string{
				"Package.swift":             swiftNothingCheckedManifest,
				".gitignore":                ".cache/\nSources/App/Scratch.swift\n",
				"Sources/App/App.swift":     "struct App {}\n",
				"Sources/App/Scratch.swift": "struct   Scratch {}\n",
			})
			writeTree(test, repository, map[string]string{".notes/README.txt": "Read me.\n"})
			git := func(arguments ...string) {
				test.Helper()
				command := exec.Command("git", append([]string{"-c", "user.email=fixture@example.com", "-c",
					"user.name=fixture"}, arguments...)...)
				command.Dir = repository
				command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
				if output, outputError := command.CombinedOutput(); outputError != nil {
					test.Fatalf("git %v: %v\n%s", arguments, outputError, output)
				}
			}
			git("init", "--quiet")
			git("add", ".")
			git("commit", "--quiet", "-m", "package")
			index := filepath.Join(repository, ".git", "index")
			run := func(arguments ...string) string {
				test.Helper()
				output, _ := runCohereWithEnvironment(test, binary, root, environment,
					append([]string{"--no-fix"}, arguments...)...)
				return output
			}

			if recorded := run(); !strings.Contains(recorded, "Scratch.swift (ignored by git)") {
				test.Fatalf("the first run did not leave out Scratch.swift as ignored, so nothing below proves "+
					"anything:\n%s", recorded)
			}
			if replayed := run(); !strings.HasPrefix(replayed, "cached: ") {
				test.Fatalf("an unchanged package did not replay, so nothing below proves anything:\n%s", replayed)
			}

			before, readError := os.ReadFile(index)
			if readError != nil {
				test.Fatal(readError)
			}
			later := time.Now().Add(2 * time.Second)
			if changeTimesError := os.Chtimes(filepath.Join(repository, ".notes", "README.txt"), later,
				later); changeTimesError != nil {
				test.Fatal(changeTimesError)
			}
			git("status", "--porcelain")
			after, readError := os.ReadFile(index)
			if readError != nil {
				test.Fatal(readError)
			}
			if bytes.Equal(before, after) {
				test.Fatal("`git status` left the index as it was, so this case proves nothing")
			}
			if replayed := run(); !strings.HasPrefix(replayed, "cached: ") {
				test.Errorf("a `git status` that rewrote the index and decided nothing new kept the run from "+
					"replaying:\n%s", replayed)
			}

			git("add", "-f", filepath.Join(layout.directory, "Sources", "App", "Scratch.swift"))
			warm := run()
			if strings.HasPrefix(warm, "cached: ") {
				test.Fatalf("a run replayed over an index that newly tracks Scratch.swift, which it now checks:\n%s",
					warm)
			}
			// The miss is the digest's, which proves the index was keyed on it and not on its bytes.
			if !strings.Contains(warm, "changed which paths its ignore rules exclude are tracked") {
				test.Errorf("the run after `git add -f` missed for another reason than the index's digest:\n%s", warm)
			}
			cold := run("--no-cache")
			for name, output := range map[string]string{"warm": warm, "cold": cold} {
				if !strings.Contains(output, "Scratch.swift:1:1 - not formatted") {
					test.Errorf("the %s run after `git add -f` does not report Scratch.swift, now tracked and "+
						"unformatted:\n%s", name, output)
				}
			}
		})
	}
}
