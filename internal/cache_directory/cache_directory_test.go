package cache_directory

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// gitStatus is `git status --porcelain --untracked-files=all` in repository: every path git would show a person.
func gitStatus(test *testing.T, repository string) string {
	test.Helper()
	command := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	command.Dir = repository
	output, runError := command.CombinedOutput()
	if runError != nil {
		test.Fatalf("git status in %s: %v\n%s", repository, runError, output)
	}
	return string(output)
}

// newRepository is a git repository with no ignore rules of its own and one committed file, and git's global and
// system ignore files read from nowhere, so nothing outside the test decides what is ignored.
func newRepository(test *testing.T) string {
	test.Helper()
	repository := test.TempDir()
	test.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	test.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, arguments := range [][]string{
		{"init", "--quiet"},
		{
			"-c", "user.name=cohere", "-c", "user.email=cohere@example.com",
			"commit", "--quiet", "--allow-empty", "-m", "start",
		},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = repository
		if output, runError := command.CombinedOutput(); runError != nil {
			test.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), runError, output)
		}
	}
	return repository
}

// TestACacheDirectoryItMadeIgnoresItself: in a repository whose rules say nothing of `.cache/`, a cache directory
// Make created, and files a run then writes in it, leave git status empty. The control is the same tree without the
// ignore file, where git shows the cache, so the empty status is the file's doing.
//
// Not parallel: newRepository sets git's configuration variables, which test.Setenv refuses beside parallel tests.
func TestACacheDirectoryItMadeIgnoresItself(test *testing.T) {
	repository := newRepository(test)
	directory := Of(repository)
	if makeError := Make(directory); makeError != nil {
		test.Fatal(makeError)
	}
	for _, name := range []string{"findings.gob", "go/ab/abcdef", "swift/build.db"} {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte("cached"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if status := gitStatus(test, repository); status != "" {
		test.Errorf("a cache directory Make created shows in git status:\n%s", status)
	}

	contents, readError := os.ReadFile(filepath.Join(directory, ignoreFileName))
	if readError != nil {
		test.Fatal(readError)
	}
	if string(contents) != IgnoreFileContents {
		test.Errorf("the ignore file holds %q, want %q", contents, IgnoreFileContents)
	}

	if removeError := os.Remove(filepath.Join(directory, ignoreFileName)); removeError != nil {
		test.Fatal(removeError)
	}
	if status := gitStatus(test, repository); !strings.Contains(status, ".cache/cohere/findings.gob") {
		test.Errorf("without the ignore file git does not show the cache, so the test proves nothing:\n%s", status)
	}
}

// TestADirectoryThatWasThereGetsNoIgnoreFile: Make writes the file only when it created the directory. One that was
// there already, as an older cohere made it, is left as it is, even with no file of its own.
func TestADirectoryThatWasThereGetsNoIgnoreFile(test *testing.T) {
	test.Parallel()
	directory := Of(test.TempDir())
	if createDirectoryError := os.MkdirAll(directory, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if makeError := Make(directory); makeError != nil {
		test.Fatal(makeError)
	}
	if _, statError := os.Lstat(filepath.Join(directory, ignoreFileName)); !os.IsNotExist(statError) {
		test.Errorf("Make wrote an ignore file in a directory it did not create (%v)", statError)
	}
}

// TestTheIgnoreFileIsNeverWrittenOverOneThatIsThere: the file is created exclusively, so one there already keeps its
// bytes.
func TestTheIgnoreFileIsNeverWrittenOverOneThatIsThere(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	path := filepath.Join(directory, ignoreFileName)
	if writeError := os.WriteFile(path, []byte("!keep\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if writeIgnoreFileError := writeIgnoreFile(directory); writeIgnoreFileError != nil {
		test.Fatal(writeIgnoreFileError)
	}
	if contents, readError := os.ReadFile(path); readError != nil || string(contents) != "!keep\n" {
		test.Errorf("the ignore file there was written over: %q (%v)", contents, readError)
	}
}

// TestAFileWhereTheDirectoryGoesIsAnError: a file at the cache directory's path is said, not taken for the directory.
func TestAFileWhereTheDirectoryGoesIsAnError(test *testing.T) {
	test.Parallel()
	directory := Of(test.TempDir())
	if createDirectoryError := os.MkdirAll(filepath.Dir(directory), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(directory, nil, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if makeError := Make(directory); makeError == nil {
		test.Error("Make took a file for the cache directory")
	}
}

// TestTheSwiftEngineWritesTheSameBytes: the Swift engine makes the directory with its own CacheDirectory, which must
// write exactly IgnoreFileContents, so whichever engine creates the directory leaves the same file.
func TestTheSwiftEngineWritesTheSameBytes(test *testing.T) {
	test.Parallel()
	source, readError := os.ReadFile(filepath.Join(
		"..", "..", "swift", "Sources", "CohereSwift", "Pipeline", "CacheDirectory.swift",
	))
	if readError != nil {
		test.Fatal(readError)
	}
	// Go's quoting of these bytes is Swift's too: printable ASCII, an apostrophe, and `\n`.
	want := "static let ignoreFileContents = " + strconv.Quote(IgnoreFileContents) + "\n"
	if !strings.Contains(string(source), want) {
		test.Errorf("CacheDirectory.swift does not hold %q", want)
	}
}
