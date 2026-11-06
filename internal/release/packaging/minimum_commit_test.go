package release

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs git in a test repository and returns its trimmed output.
func gitIn(test *testing.T, directory string, arguments ...string) string {
	test.Helper()

	output, gitRunError := gitRun(directory, arguments...)
	if gitRunError != nil {
		test.Fatal(gitRunError)
	}
	return output
}

// gitRun is gitIn for a fixture built outside any one test, which has no *testing.T to fail.
func gitRun(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", append([]string{
		"-C",
		directory,
		"-c",
		"user.name=test",
		"-c",
		"user.email=test@example.com",
		"-c",
		"commit.gpgsign=false",
	}, arguments...)...)
	output, outputError := command.CombinedOutput()
	if outputError != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(arguments, " "), outputError, output)
	}
	return strings.TrimSpace(string(output)), nil
}

// repositoryWithSideCommit returns a repository checked out at its first commit, the hash of that
// commit, and the hash of a second commit on a branch the checkout does not contain.
func repositoryWithSideCommit(test *testing.T) (directory string, checkedOut string, notContained string) {
	test.Helper()

	directory = test.TempDir()
	gitIn(test, directory, "init", "--quiet")

	writeFile(test, filepath.Join(directory, "first.txt"), "first\n")
	gitIn(test, directory, "add", "first.txt")
	gitIn(test, directory, "commit", "--quiet", "-m", "first")
	checkedOut = gitIn(test, directory, "rev-parse", "HEAD")

	gitIn(test, directory, "checkout", "--quiet", "-b", "side")
	writeFile(test, filepath.Join(directory, "second.txt"), "second\n")
	gitIn(test, directory, "add", "second.txt")
	gitIn(test, directory, "commit", "--quiet", "-m", "second")
	notContained = gitIn(test, directory, "rev-parse", "HEAD")

	gitIn(test, directory, "checkout", "--quiet", checkedOut)
	return directory, checkedOut, notContained
}

// TestReleaseProceedsFromACommitContainingTheMinimum is the positive half. Without it, a check that
// refused everything would pass the controls below.
func TestReleaseProceedsFromACommitContainingTheMinimum(test *testing.T) {
	test.Parallel()

	directory, checkedOut, _ := repositoryWithSideCommit(test)

	if requireAncestorError := requireAncestor(directory, checkedOut, "the reason"); requireAncestorError != nil {
		test.Fatalf("a checkout containing the minimum was refused: %v", requireAncestorError)
	}
}

// TestReleaseRefusesACommitMissingTheMinimum is the known-dirty control: the minimum exists in the
// repository but not in the history being released, and the refusal says why.
func TestReleaseRefusesACommitMissingTheMinimum(test *testing.T) {
	test.Parallel()

	directory, _, notContained := repositoryWithSideCommit(test)

	requireAncestorError := requireAncestor(directory, notContained, "the reason it matters")
	if requireAncestorError == nil {
		test.Fatal("a checkout missing the minimum was allowed to release")
	}
	if !strings.Contains(requireAncestorError.Error(), "the reason it matters") {
		test.Fatalf("the refusal does not carry the reason: %v", requireAncestorError)
	}
}

// TestReleaseRefusesWhenGitCannotTell holds the third exit code. A minimum git has never seen,
// which is what a shallow clone looks like, must not read as a pass.
func TestReleaseRefusesWhenGitCannotTell(test *testing.T) {
	test.Parallel()

	directory, _, _ := repositoryWithSideCommit(test)

	requireAncestorError := requireAncestor(directory, strings.Repeat("0", 40), "the reason")
	if requireAncestorError == nil {
		test.Fatal("a minimum git could not find was treated as present")
	}
	if !strings.Contains(requireAncestorError.Error(), "cannot tell") {
		test.Fatalf("an undecidable check was reported as a plain refusal: %v", requireAncestorError)
	}
}

// TestBuildRefusesBeforeTheMinimum proves Build calls the check, which the tests above cannot: they
// drive requireAncestor directly and would pass with the call deleted from Build. A repository that
// never contained MinimumReleaseCommit must be refused before anything is compiled. It pins a compiler
// correctly, because Build reads the pin first and would otherwise refuse on that instead.
func TestBuildRefusesBeforeTheMinimum(test *testing.T) {
	test.Parallel()

	fixture := moduleWithCompiler(test)

	_, buildError := Build(Options{Version: "1.0.0", ModuleDirectory: fixture.Module, OutputDirectory: test.TempDir()})
	if buildError == nil {
		test.Fatal("Build released from a repository that never contained the minimum commit")
	}
	// Refused for the minimum, by name. Any earlier refusal also returns an error, and passed this test
	// unnoticed when the copyright check first stopped the fixture on a missing license.
	if !strings.Contains(buildError.Error(), ShortCommit(MinimumReleaseCommit)) {
		test.Fatalf("Build refused, but not for the minimum commit: %v", buildError)
	}
	if !strings.Contains(buildError.Error(), ShortCommit(MinimumReleaseCommit)) {
		test.Fatalf("Build failed, but not on the minimum commit: %v", buildError)
	}
}
