package project

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Gitlink is one submodule of a worktree: its path and the commit its tree pins.
type Gitlink struct {
	Path string
	Pin  string
}

// GitlinksOf lists a worktree's submodules from its index, where a gitlink is an entry of mode 160000.
func GitlinksOf(worktree string) ([]Gitlink, error) {
	staged, gitError := gitOutput(worktree, "ls-files", "--stage")
	if gitError != nil {
		return nil, fmt.Errorf("listing %s's files: %w", worktree, gitError)
	}
	var links []Gitlink
	for _, line := range strings.Split(staged, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "160000" {
			continue
		}
		links = append(links, Gitlink{Path: strings.Join(fields[3:], " "), Pin: fields[1]})
	}
	return links, nil
}

// gitlinks is the paths of a worktree's submodules.
func gitlinks(worktree string) ([]string, error) {
	links, linksError := GitlinksOf(worktree)
	if linksError != nil {
		return nil, linksError
	}
	paths := make([]string, 0, len(links))
	for _, link := range links {
		paths = append(paths, link.Path)
	}
	return paths, nil
}

// gitOutput runs git in a directory and returns its trimmed output, carrying git's stderr into the error.
func gitOutput(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	command.Dir = directory
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), runError,
			strings.TrimSpace(standardError.String()))
	}
	return strings.TrimSpace(string(output)), nil
}
