package project

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/system-inc/cohere/git_tree"
)

// Gitlink is one submodule of a worktree: its path and the commit its tree pins.
type Gitlink struct {
	Path string
	Pin  string
}

// GitlinksOf lists a worktree's submodules from its index, where a gitlink is an entry of mode 160000.
// It reads git's -z listing, so a path holding a tab, a newline or two spaces in a row is the path. A
// worktree mid-merge still reads: a path a merge left in conflict is a gitlink when any side lists it as
// one, listed once, pinned at the side its stage ranks first by stagePreference.
//
// A check reads only the paths (gitlinks below). The run cache keys the index on gitignore.TrackedIgnoredDigest,
// which digests each gitlink by path and mode and not by its pinned commit (#m1daxxk), so a check that comes to read
// Pin needs the digest to take the object too, or a replay serves a verdict over another pin.
func GitlinksOf(worktree string) ([]Gitlink, error) {
	listing, gitError := gitOutput(worktree, "ls-files", "--stage", "-z")
	if gitError != nil {
		return nil, fmt.Errorf("listing %s's files: %w", worktree, gitError)
	}
	entries, parseError := git_tree.ParseIndexListing(listing)
	if parseError != nil {
		return nil, fmt.Errorf("reading %s's index: %w", worktree, parseError)
	}
	var links []Gitlink
	// pinned is where each path already listed sits in links and the stage its pin was read at, so a
	// later stage of the same path that stagePreference ranks first takes its pin's place.
	type pinnedLink struct {
		position int
		stage    int
	}
	pinned := map[string]pinnedLink{}
	for _, entry := range entries {
		if entry.Mode != git_tree.ModeGitlink {
			continue
		}
		link, listed := pinned[entry.Path]
		if !listed {
			pinned[entry.Path] = pinnedLink{position: len(links), stage: entry.Stage}
			links = append(links, Gitlink{Path: entry.Path, Pin: entry.Object})
			continue
		}
		if stagePreference(entry.Stage) < stagePreference(link.stage) {
			pinned[entry.Path] = pinnedLink{position: link.position, stage: entry.Stage}
			links[link.position].Pin = entry.Object
		}
	}
	return links, nil
}

// stagePreference ranks the stage a conflicted gitlink's pin is read from, lowest first: a merged entry
// (0), then ours (2), what `HEAD` pins and the checkout holds while the merge is open, then theirs (3),
// which git leaves in the tree when ours removed the path, and the merge's base (1) last.
func stagePreference(stage int) int {
	switch stage {
	case 0:
		return 0
	case 2:
		return 1
	case 3:
		return 2
	}
	return 3
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

// gitOutput runs git in a directory and returns its output as git wrote it, untrimmed: a -z listing is
// records each ended by a zero byte, which its parser reads whole. It carries git's stderr into the error.
func gitOutput(directory string, arguments ...string) ([]byte, error) {
	command := exec.Command("git", arguments...)
	command.Dir = directory
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), runError,
			strings.TrimSpace(standardError.String()))
	}
	return output, nil
}
