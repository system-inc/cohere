package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeRepository makes a repository at directory whose one commit, on main and checked out, holds files and a
// gitlink at each path of gitlinks to the commit it names, and returns the commit. It is made with plumbing, so no
// hook a machine's git configuration names runs, and with a fixed identity, so it needs none.
func makeRepository(test *testing.T, directory string, files map[string]string, gitlinks map[string]string) string {
	test.Helper()
	if createDirectoryError := os.MkdirAll(directory, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	runGit(test, directory, "init", "--quiet", directory)
	for name, contents := range files {
		target := filepath.Join(directory, filepath.FromSlash(name))
		if createParentError := os.MkdirAll(filepath.Dir(target), 0o755); createParentError != nil {
			test.Fatal(createParentError)
		}
		if writeError := os.WriteFile(target, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		runGit(test, directory, "add", "--", name)
	}
	for name, commit := range gitlinks {
		runGit(test, directory, "update-index", "--add", "--cacheinfo", "160000,"+commit+","+name)
	}
	tree := runGit(test, directory, "write-tree")
	commit := runGit(test, directory, "commit-tree", tree, "-m", "fixture")
	runGit(test, directory, "update-ref", "refs/heads/main", commit)
	runGit(test, directory, "symbolic-ref", "HEAD", "refs/heads/main")
	return commit
}

// runGit runs git in directory for a test, failing it on an error, and returns what git printed, trimmed.
func runGit(test *testing.T, directory string, arguments ...string) string {
	test.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com")
	output, runError := command.CombinedOutput()
	if runError != nil {
		test.Fatalf("git %s in %s: %v: %s", strings.Join(arguments, " "), directory, runError, output)
	}
	return strings.TrimSpace(string(output))
}
