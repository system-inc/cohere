// Package written_repository makes a directory a git repository without running git: the `.git` `git init` would
// write, as far as git and cohere read it, and the index `git add` and `git update-index` would leave. A test's git
// child forks the test process, and on darwin each fork makes every page of the heap fault again on its next write,
// so each fixture's `git init` cost a package's process kernel time it spent on nothing (#2htryg8), and a test that
// starts git reads the machine's git besides (#vb50z0q). TestAWrittenRepositoryReadsAsTheOneGitMakes holds the two
// alike.
package written_repository

import (
	"os"
	"path/filepath"

	"github.com/system-inc/cohere/internal/git_tree"
)

// writtenFiles are the files Write writes below the git directory, by path, with their bytes: what `git init` writes
// there that git needs to read the repository, `HEAD` and the config, and the info/exclude git's template gives every
// repository, which cohere-go names among a run's inputs. The config is the one `git init` writes under no config of
// the person's but for the lines it probes the file system for, and cohere reads only its object format, SHA-1's when
// it names none.
var writtenFiles = map[string]string{
	"HEAD":   "ref: refs/heads/main\n",
	"config": "[core]\n\trepositoryformatversion = 0\n\tfilemode = true\n\tbare = false\n\tlogallrefupdates = true\n",
	filepath.Join("info", "exclude"): "# git ls-files --others --exclude-from=.git/info/exclude\n" +
		"# Lines that start with '#' are comments.\n" +
		"# For a project mostly in C, the following would be a good set of\n" +
		"# exclude patterns (uncomment them if you want to use them):\n" +
		"# *.[oa]\n" +
		"# *~\n",
}

// writtenDirectories are the directories Write makes below the git directory, empty: git reads a directory as a
// repository only when it holds `HEAD`, objects and refs.
var writtenDirectories = []string{"objects", filepath.Join("refs", "heads"), filepath.Join("refs", "tags")}

// Write makes directory a git repository of its own, its `.git` written without running git, with tracked in its
// index (WriteGitDirectory). With nothing tracked it writes no index, as `git init` writes none, and cohere-go names
// the index absent.
func Write(directory string, tracked ...git_tree.IndexEntry) error {
	return WriteGitDirectory(filepath.Join(directory, ".git"), directory, tracked...)
}

// WriteGitDirectory writes gitDirectory as `git init` would, and an index of tracked when there is any, each path
// relative to worktree: a submodule's git directory lives inside its parent's `.git` (`modules/<name>`) and its
// worktree elsewhere. An entry naming no object is the file at its path in worktree, recorded as `git add` records it
// (git_tree.HashFile); one naming its object, a gitlink's commit or a side of a conflict, is recorded as given, as
// `git update-index --cacheinfo` records it.
func WriteGitDirectory(gitDirectory string, worktree string, tracked ...git_tree.IndexEntry) error {
	for _, relative := range writtenDirectories {
		if makeError := os.MkdirAll(filepath.Join(gitDirectory, relative), 0o755); makeError != nil {
			return makeError
		}
	}
	for relative, contents := range writtenFiles {
		path := filepath.Join(gitDirectory, relative)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			return makeError
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			return writeError
		}
	}
	if len(tracked) == 0 {
		return nil
	}
	return writeIndex(gitDirectory, worktree, tracked)
}
