package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A source is copied into a bare repository of the command's own, its objects copied rather than linked, a commit no
// branch holds among them, as a pin can be, and a layer checks out from the copy at that commit.
func TestCopySourceMakesAPrivateBareCopyALayerChecksOutFrom(test *testing.T) {
	test.Parallel()
	source := filepath.Join(test.TempDir(), "source")
	first := makeRepository(test, source, map[string]string{"go.mod": "module source\n"}, nil)
	// A second commit on no branch: a local clone copies the whole object store, so it is in the copy too.
	unbranched := runGit(test, source, "commit-tree", runGit(test, source, "rev-parse", first+"^{tree}"), "-p", first,
		"-m", "unbranched")

	copies := test.TempDir()
	copied, copyError := copySource(source, filepath.Join(copies, "source.git"), progress{writer: io.Discard})
	if copyError != nil {
		test.Fatal(copyError)
	}
	if bare := runGit(test, copied, "rev-parse", "--is-bare-repository"); bare != "true" {
		test.Errorf("the copy is not bare: %s", bare)
	}
	for _, commit := range []string{first, unbranched} {
		if existsError := commitExists(copied, commit); existsError != nil {
			test.Errorf("the copy lacks %s: %v", commit, existsError)
		}
	}
	objectPath := filepath.Join("objects", first[:2], first[2:])
	sourceObject, sourceStatError := os.Stat(filepath.Join(source, ".git", objectPath))
	copiedObject, copiedStatError := os.Stat(filepath.Join(copied, objectPath))
	if sourceStatError != nil || copiedStatError != nil {
		test.Fatalf("reading the commit's object in both: %v, %v", sourceStatError, copiedStatError)
	}
	if os.SameFile(sourceObject, copiedObject) {
		test.Error("the copy's object is a hard link to the source's, so it is still the source owner's file")
	}

	root := filepath.Join(test.TempDir(), "corpus")
	if checkOutError := checkOutLayer(root, corpusLayer{repository: copied, commit: unbranched}); checkOutError != nil {
		test.Fatal(checkOutError)
	}
	if head := runGit(test, root, "rev-parse", "HEAD"); head != unbranched {
		test.Errorf("the layer is at %s, want %s", head, unbranched)
	}
	if contents, readError := os.ReadFile(filepath.Join(root, "go.mod")); readError != nil ||
		string(contents) != "module source\n" {
		test.Errorf("the layer's go.mod reads %q, %v", contents, readError)
	}
}

// A source another user owns, as a rented machine's root-owned mirror is, is refused by git as "dubious ownership"
// and copied all the same, since the copy names it safe. A test cannot make a repository another user owns, so git's
// own switch for testing this, GIT_TEST_ASSUME_DIFFERENT_OWNER, has git take every repository for another user's.
// Not parallel: it sets the switch in the process's environment, which every git it starts reads.
func TestCopySourceReadsARepositoryAnotherUserOwns(test *testing.T) {
	checkout := filepath.Join(test.TempDir(), "checkout")
	commit := makeRepository(test, checkout, map[string]string{"go.mod": "module mirror\n"}, nil)
	// A bare mirror, as the rented machine's are, and a checkout, as a house box's sources are.
	mirror := filepath.Join(test.TempDir(), "mirror.git")
	runGit(test, checkout, "clone", "--quiet", "--bare", checkout, mirror)
	copies := test.TempDir()
	test.Setenv("GIT_TEST_ASSUME_DIFFERENT_OWNER", "1")
	for index, source := range []string{mirror, checkout} {
		destination := filepath.Join(copies, fmt.Sprintf("%d", index))
		if _, unaidedError := gitOutput(copies, "clone", "--quiet", "--bare", "--no-hardlinks", source,
			destination+"-unaided.git"); unaidedError == nil {
			test.Skip("this git does not take GIT_TEST_ASSUME_DIFFERENT_OWNER, so it cannot be shown another user's " +
				"repository")
		} else if !strings.Contains(unaidedError.Error(), "dubious ownership") {
			test.Fatalf("a clone of %s failed otherwise than as dubious ownership: %v", source, unaidedError)
		}
		copied, copyError := copySource(source, destination+".git", progress{writer: io.Discard})
		if copyError != nil {
			test.Fatalf("the copy of %s, another user's, was refused: %v", source, copyError)
		}
		// The copy is this user's, and the switch would take it for another's, so it is read with the switch off.
		head, headError := gitBytesIn(copied, []string{"GIT_TEST_ASSUME_DIFFERENT_OWNER=0"}, "rev-parse", "main")
		if headError != nil || strings.TrimSpace(string(head)) != commit {
			test.Errorf("the copy of %s has main at %q, %v, want %s", source, head, headError, commit)
		}
	}
}

// The file copySource hands git names the repository and its .git, and git reads each path back whole: one with a
// space, a quote, a semicolon, a hash and a backslash in it comes back exactly as written.
func TestSafeDirectoryConfigurationIsReadBackWhole(test *testing.T) {
	test.Parallel()
	repository := filepath.Join(test.TempDir(), `odd name "with" ;semicolon #hash \back`)
	file := filepath.Join(test.TempDir(), "safe.gitconfig")
	if writeError := os.WriteFile(file, []byte(safeDirectoryConfiguration(repository)), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	read := runGit(test, test.TempDir(), "config", "--file", file, "--get-all", "safe.directory")
	if want := repository + "\n" + filepath.Join(repository, ".git"); read != want {
		test.Errorf("git read back\n%q\nwant\n%q", read, want)
	}
}

// Where the command runs in no checkout, as on a rented machine, every source the checkout would have given must be
// named, and the refusal names each flag at once.
func TestResolveSourcesNamesEveryFlagWithNoCheckout(test *testing.T) {
	test.Parallel()
	_, resolveError := resolveSources(commandLine{}, "")
	if resolveError == nil {
		test.Fatal("sources resolved with no checkout and no flags")
	}
	for _, flag := range []string{"--cohere-repository", "--typescript-repository", "--adamic-repository",
		"--loom-repository", "--corpora"} {
		if !strings.Contains(resolveError.Error(), flag) {
			test.Errorf("the refusal does not name %s: %v", flag, resolveError)
		}
	}
	resolved, flaggedError := resolveSources(commandLine{
		cohereRepository:     "/mirrors/cohere.git",
		typeScriptRepository: "/mirrors/typescript.git",
		corpora:              "/corpora",
	}, "")
	if flaggedError != nil {
		test.Fatal(flaggedError)
	}
	if want := (sourceRepositories{cohere: "/mirrors/cohere.git", typeScript: "/mirrors/typescript.git",
		adamic: "/corpora/adamic", loom: "/corpora/loom"}); resolved != want {
		test.Errorf("resolved %+v, want %+v", resolved, want)
	}
}

// With no checkout there is no `HEAD`, so the preflight refuses a measurement without --branch, naming the flag, and
// takes the one given as it is.
func TestBranchToMeasureIsRequiredWithNoCheckout(test *testing.T) {
	test.Parallel()
	if _, branchError := branchToMeasure(commandLine{}, ""); branchError == nil ||
		!strings.Contains(branchError.Error(), "--branch is required") {
		test.Errorf("no checkout and no --branch refused with %v, want one naming --branch", branchError)
	}
	if branch, branchError := branchToMeasure(commandLine{branch: "abc123"}, ""); branchError != nil ||
		branch != "abc123" {
		test.Errorf("--branch abc123 resolved to %q, %v", branch, branchError)
	}
}

// A shallow mirror, as a feed fetched at depth one is, copies shallow, and a pin outside its depth is refused naming
// the shallowness and how to deepen the source, rather than as a commit that may not exist. A pin inside its depth is
// held, so a shallow source that reaches the pin measures as any other.
func TestCopyOfAShallowSourceSaysItIsShallowWhenItLacksAPin(test *testing.T) {
	test.Parallel()
	source := filepath.Join(test.TempDir(), "source")
	older := makeRepository(test, source, map[string]string{"go.mod": "module source\n"}, nil)
	runGit(test, source, "commit", "--quiet", "--allow-empty", "-m", "newer")
	newer := runGit(test, source, "rev-parse", "HEAD")
	mirror := filepath.Join(test.TempDir(), "mirror.git")
	runGit(test, source, "clone", "--quiet", "--bare", "--depth", "1", "file://"+source, mirror)

	copied, copyError := copySource(mirror, filepath.Join(test.TempDir(), "mirror.git"), progress{writer: io.Discard})
	if copyError != nil {
		test.Fatal(copyError)
	}
	if existsError := commitExists(copied, newer); existsError != nil {
		test.Errorf("the shallow copy lacks the commit at its depth: %v", existsError)
	}
	existsError := commitExists(copied, older)
	if existsError == nil {
		test.Fatal("the shallow copy holds a commit beyond its depth, so the fixture is not shallow")
	}
	for _, words := range []string{"it is shallow", "git fetch --unshallow", shortCommit(older)} {
		if !strings.Contains(existsError.Error(), words) {
			test.Errorf("the refusal does not say %q: %v", words, existsError)
		}
	}
	whole := filepath.Join(test.TempDir(), "whole.git")
	runGit(test, source, "clone", "--quiet", "--bare", source, whole)
	if wholeError := commitExists(whole, "0123456789abcdef0123456789abcdef01234567"); wholeError == nil ||
		strings.Contains(wholeError.Error(), "shallow") {
		test.Errorf("a whole repository's refusal of a missing commit is %v, want one that never says shallow",
			wholeError)
	}
}
