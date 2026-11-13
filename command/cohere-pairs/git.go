package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// shortCommitLength is how many characters of a commit the command prints.
const shortCommitLength = 12

// gitOutput runs git in directory and returns what it printed, trimmed, carrying git's stderr into the error.
func gitOutput(directory string, arguments ...string) (string, error) {
	output, gitError := gitBytes(directory, arguments...)
	return strings.TrimSpace(string(output)), gitError
}

// gitBytes runs git in directory and returns what it printed byte for byte, as a file's contents or a -z listing
// need it, carrying git's stderr into the error.
func gitBytes(directory string, arguments ...string) ([]byte, error) {
	command := exec.Command("git", arguments...)
	command.Dir = directory
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("git %s in %s: %w: %s", strings.Join(arguments, " "), directory, runError,
			strings.TrimSpace(standardError.String()))
	}
	return output, nil
}

// commitExists reports whether repository holds commit, as `git cat-file -e` answers. A shallow repository holds only
// part of its history, and a copy of one is shallow the same way, so when it lacks the commit the refusal says so:
// the source named on the command line is the one to deepen, not the copy.
func commitExists(repository string, commit string) error {
	_, catFileError := gitOutput(repository, "cat-file", "-e", commit+"^{commit}")
	if catFileError == nil {
		return nil
	}
	// When git cannot say whether the repository is shallow, the plain refusal below is still true, so it is said.
	shallow, shallowError := gitOutput(repository, "rev-parse", "--is-shallow-repository")
	if shallowError == nil && shallow == "true" {
		return fmt.Errorf("%s does not hold commit %s, and it is shallow: the source it came from holds only part of "+
			"its history. Give a source with its whole history, or one fetched deep enough to reach %s (`git fetch "+
			"--unshallow` in it): %w", repository, commit, shortCommit(commit), catFileError)
	}
	return fmt.Errorf("%s does not hold commit %s: %w", repository, commit, catFileError)
}

// gitlinkOf is the commit a tree pins the submodule at path to, read with `git ls-tree`, which prints
// "160000 commit <sha>\t<path>" for a submodule. Anything else is refused: a corpus missing a pin it names checks a
// workspace that does not build.
func gitlinkOf(repository string, commit string, path string) (string, error) {
	listing, listTreeError := gitOutput(repository, "ls-tree", commit, path)
	if listTreeError != nil {
		return "", listTreeError
	}
	fields := strings.Fields(listing)
	if len(fields) < 3 || fields[0] != "160000" || fields[1] != "commit" {
		return "", fmt.Errorf("%s at %s pins no submodule at %s (ls-tree printed %q)", repository, commit, path,
			listing)
	}
	return fields[2], nil
}

// shortCommit is the first twelve characters of a commit, as the table and the release version name it.
func shortCommit(commit string) string {
	return prefix(commit, shortCommitLength)
}
