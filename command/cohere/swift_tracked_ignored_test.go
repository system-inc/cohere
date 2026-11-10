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

// In a linked worktree, a Swift run misses on the index's digest once `git add -f` tracks a file that only the
// repository's shared info/exclude ignores (#dznkem5). A worktree's `.git` is a file, and git reads the info/exclude in
// the common directory it shares with the main checkout. The engine excludes by that file and names the worktree's
// own index under `trackedIgnored`, and since #vdbcswp gitignore.TrackedIgnoredDigest reads the same file, so tracking
// a path it excludes moves the digest. A digest that did not read it would move nothing there, and the run would
// replay a verdict that never checked the file.
//
// Scratch.swift is listed in the main checkout's `.git/info/exclude` alone, and written unformatted in the worktree,
// so the run reports it once it is tracked. As above, a `git status` over a touched `.notes/README.txt` rewrites the
// worktree's index without deciding anything, and the run replays through it.
func TestASwiftRunInAWorktreeMissesOnAGitAddForceOfAFileOnlyItsSharedExcludeIgnores(test *testing.T) {
	test.Parallel()
	if testing.Short() {
		test.Skip("runs the real Swift engine, built from this checkout when its sources changed")
	}
	binary := buildCohere(test)
	environment := []string{
		dispatch.SwiftEngineOverrideVariable + "=" + builtSwiftEngine(test),
		"PATH=" + toolsWithoutGit(test),
	}
	temporary, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	checkout := filepath.Join(temporary, "checkout")
	worktree := filepath.Join(temporary, "feature")
	writeTree(test, checkout, map[string]string{
		"Package.swift":         swiftNothingCheckedManifest,
		".gitignore":            ".cache/\n",
		"Sources/App/App.swift": "struct App {}\n",
		".notes/README.txt":     "Read me.\n",
	})
	git := func(directory string, arguments ...string) {
		test.Helper()
		command := exec.Command("git", append([]string{"-c", "user.email=fixture@example.com", "-c",
			"user.name=fixture"}, arguments...)...)
		command.Dir = directory
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if output, outputError := command.CombinedOutput(); outputError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, outputError, output)
		}
	}
	git(checkout, "init", "--quiet")
	git(checkout, "add", ".")
	git(checkout, "commit", "--quiet", "-m", "package")
	// Relative, since git spells an absolute pointer by its real path, `/private/tmp`, where the engine's record spells
	// the index as Foundation does, `/tmp`, and the front door keys an index on its digest only when the two agree.
	git(checkout, "worktree", "add", "--quiet", "--relative-paths", worktree)
	sharedExclude := filepath.Join(checkout, ".git", "info", "exclude")
	if writeError := os.WriteFile(sharedExclude, []byte("Sources/App/Scratch.swift\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	writeTree(test, worktree, map[string]string{"Sources/App/Scratch.swift": "struct   Scratch {}\n"})
	if information, statError := os.Stat(filepath.Join(worktree, ".git")); statError != nil || information.IsDir() {
		test.Fatalf("the worktree's .git is not a file, so this case proves nothing: %v", statError)
	}
	index := filepath.Join(checkout, ".git", "worktrees", "feature", "index")
	run := func(arguments ...string) string {
		test.Helper()
		output, _ := runCohereWithEnvironment(test, binary, worktree, environment,
			append([]string{"--no-fix"}, arguments...)...)
		return output
	}

	if recorded := run(); !strings.Contains(recorded, "Scratch.swift (ignored by git)") {
		test.Fatalf("the first run did not leave out Scratch.swift as ignored, so nothing below proves anything:\n%s",
			recorded)
	}
	if replayed := run(); !strings.HasPrefix(replayed, "cached: ") {
		test.Fatalf("an unchanged package did not replay, so nothing below proves anything:\n%s", replayed)
	}

	before, readError := os.ReadFile(index)
	if readError != nil {
		test.Fatal(readError)
	}
	later := time.Now().Add(2 * time.Second)
	if changeTimesError := os.Chtimes(filepath.Join(worktree, ".notes", "README.txt"), later,
		later); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
	git(worktree, "status", "--porcelain")
	after, readError := os.ReadFile(index)
	if readError != nil {
		test.Fatal(readError)
	}
	if bytes.Equal(before, after) {
		test.Fatal("`git status` left the worktree's index as it was, so this case proves nothing")
	}
	if replayed := run(); !strings.HasPrefix(replayed, "cached: ") {
		test.Errorf("a `git status` that rewrote the worktree's index and decided nothing new kept the run from "+
			"replaying, so the index was keyed on its bytes:\n%s", replayed)
	}

	git(worktree, "add", "-f", filepath.Join("Sources", "App", "Scratch.swift"))
	warm := run()
	if strings.HasPrefix(warm, "cached: ") {
		test.Fatalf("a run replayed over a worktree index that newly tracks Scratch.swift, which it now checks:\n%s",
			warm)
	}
	// The miss is the digest's, which proves the digest read the shared exclude, not that the index's bytes moved.
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
}
