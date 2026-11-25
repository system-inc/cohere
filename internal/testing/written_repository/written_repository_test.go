package written_repository

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/git_tree"
)

// sealedGitEnvironment is what TestMain sets so that the oracle's git reads no configuration but a repository's own:
// no global file, and no system one.
var sealedGitEnvironment = map[string]string{"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1"}

// TestMain seals every git child of the process from the machine's git configuration, as internal/go_language's
// does: the person's global config sets hooks, the default branch and what is ignored, which the oracle must never
// read.
func TestMain(suite *testing.M) {
	for name, value := range sealedGitEnvironment {
		if setError := os.Setenv(name, value); setError != nil {
			// Ignored: the process exits failed whether or not the reason reaches standard error.
			_, _ = fmt.Fprintln(os.Stderr, setError)
			os.Exit(1)
		}
	}
	os.Exit(suite.Run())
}

// oracleFiles are the files each repository of the oracle holds, by slash-separated path.
var oracleFiles = map[string]string{
	"go.mod":           "module example.com/written\n\ngo 1.25\n",
	"written.go":       "package written\n",
	"inner/inner.go":   "package inner\n",
	"tab\tname.go":     "package written\n",
	"two  spaces.go":   "package written\n",
	"conflicted/merge": "<<<<<<< ours\n",
}

// oracleRecorded are the entries both repositories record as given rather than from a file: a gitlink, a gitlink at a
// path a plain listing would quote, and a path left in conflict, at each of its three stages.
var oracleRecorded = []git_tree.IndexEntry{
	{Entry: git_tree.Entry{Mode: git_tree.ModeGitlink, Object: strings.Repeat("1", 40), Path: "third_party/lib"}},
	{Entry: git_tree.Entry{Mode: git_tree.ModeGitlink, Object: strings.Repeat("2", 40), Path: "linked\there"}},
	{Entry: git_tree.Entry{Mode: git_tree.ModeRegular, Object: strings.Repeat("a", 40), Path: "conflicted/merge"},
		Stage: 1},
	{Entry: git_tree.Entry{Mode: git_tree.ModeRegular, Object: strings.Repeat("b", 40), Path: "conflicted/merge"},
		Stage: 2},
	{Entry: git_tree.Entry{Mode: git_tree.ModeRegular, Object: strings.Repeat("c", 40), Path: "conflicted/merge"},
		Stage: 3},
}

// A repository written without git (Write) reads as the one git makes of the same files: `git init`, `git add` and
// `git update-index --index-info` for what is recorded as given. Git finds each, lists the same index entry for entry
// with `git ls-files --stage -z`, its stages and pins included, tells the same status, and reads the same `HEAD` and
// info/exclude, the files cohere-go names among a run's inputs. With
// TestAWrittenGitlinksIndexIsTheBytesGitWrites it is the package's one live-git proof, the oracle every test that
// writes a repository with this package rests on (#vb50z0q).
func TestAWrittenRepositoryReadsAsTheOneGitMakes(test *testing.T) {
	test.Parallel()
	written, made := oracleWorktree(test), oracleWorktree(test)
	tracked := []git_tree.IndexEntry{}
	for path := range oracleFiles {
		if path != "conflicted/merge" {
			tracked = append(tracked, git_tree.IndexEntry{Entry: git_tree.Entry{Path: path}})
		}
	}
	if writeError := Write(written, append(tracked, oracleRecorded...)...); writeError != nil {
		test.Fatal(writeError)
	}
	runOracleGit(test, made, nil, "init", "-q", "-b", "main")
	for _, entry := range tracked {
		runOracleGit(test, made, nil, "add", "--", entry.Path)
	}
	var indexInformation bytes.Buffer
	for _, entry := range oracleRecorded {
		indexInformation.WriteString(entry.Mode.String() + " " + entry.Object + " " + strconv.Itoa(entry.Stage) + "\t" +
			entry.Path + "\x00")
	}
	runOracleGit(test, made, &indexInformation, "update-index", "-z", "--index-info")

	for _, arguments := range [][]string{
		{"rev-parse", "--git-dir"}, {"ls-files", "--stage", "-z"}, {"status", "--porcelain", "-z"},
	} {
		writtenOutput := runOracleGit(test, written, nil, arguments...)
		madeOutput := runOracleGit(test, made, nil, arguments...)
		if writtenOutput != madeOutput {
			test.Errorf("git %s over the written repository read\n%q\nand over the one git made\n%q",
				strings.Join(arguments, " "), writtenOutput, madeOutput)
		}
	}
	for _, relative := range []string{"HEAD", filepath.Join("info", "exclude")} {
		writtenContents, writtenError := os.ReadFile(filepath.Join(written, ".git", relative))
		madeContents, madeError := os.ReadFile(filepath.Join(made, ".git", relative))
		if writtenError != nil || madeError != nil {
			test.Fatalf("reading %s: %v, %v", relative, writtenError, madeError)
		}
		if !bytes.Equal(writtenContents, madeContents) {
			test.Errorf("the written %s is\n%q\nand git's\n%q", relative, writtenContents, madeContents)
		}
	}
}

// A gitlinks index written without git is the bytes git writes for the same gitlinks with `git update-index --add
// --cacheinfo`, as copyFixture's workspace fixture records them: git leaves a gitlink's stat data zero and adds no
// extension, so the two files agree byte for byte, and a recording that drifts from git's format in any field fails
// here rather than in a reader that forgives it.
func TestAWrittenGitlinksIndexIsTheBytesGitWrites(test *testing.T) {
	test.Parallel()
	gitlinks := oracleRecorded[:2]
	written, made := test.TempDir(), test.TempDir()
	if writeError := Write(written, gitlinks...); writeError != nil {
		test.Fatal(writeError)
	}
	runOracleGit(test, made, nil, "init", "-q", "-b", "main")
	for _, entry := range gitlinks {
		runOracleGit(test, made, nil, "update-index", "--add", "--cacheinfo",
			entry.Mode.String()+","+entry.Object+","+entry.Path)
	}
	writtenIndex, writtenError := os.ReadFile(filepath.Join(written, ".git", "index"))
	madeIndex, madeError := os.ReadFile(filepath.Join(made, ".git", "index"))
	if writtenError != nil || madeError != nil {
		test.Fatalf("reading the indexes: %v, %v", writtenError, madeError)
	}
	if !bytes.Equal(writtenIndex, madeIndex) {
		test.Errorf("the written index is\n%x\nand git's\n%x", writtenIndex, madeIndex)
	}
}

// oracleWorktree is a temporary directory holding oracleFiles.
func oracleWorktree(test *testing.T) string {
	test.Helper()
	directory := test.TempDir()
	for name, contents := range oracleFiles {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return directory
}

// runOracleGit runs git in directory with input on its standard input, and returns what it wrote there, failing test
// when it fails. It inherits TestMain's seal, so it reads no git configuration but the repository's own.
func runOracleGit(test *testing.T, directory string, input *bytes.Buffer, arguments ...string) string {
	test.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	if input != nil {
		command.Stdin = input
	}
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		test.Fatalf("git %s in %s: %v: %s", strings.Join(arguments, " "), directory, runError, standardError.String())
	}
	return string(output)
}
