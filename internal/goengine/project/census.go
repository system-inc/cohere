package project

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/gitignore"
)

// IgnoredByGit is the reason a file git ignores is not ours.
const IgnoredByGit = "ignored by git"

// Census is every `.go` file a workspace's modules hold, as absolute paths in order, and which are ours.
type Census struct {
	// Files are every file go would see in an owned module's packages.
	Files []string

	// Ours are the Files nothing excludes.
	Ours []string

	// Excluded are the Files that are not ours, each with its reason.
	Excluded []Exclusion
}

// Exclusion is a file left out of what is ours, and why.
type Exclusion struct {
	File   string
	Reason string
}

// TakeCensus lists every `.go` file the workspace's modules hold, the way go itself sees packages: under each
// owned module's directory, never entering testdata, a directory whose name starts with `.` or `_`, a
// directory with a go.mod of its own (an owned one is walked as itself, and an unowned one is another
// project), or a repository of its own, a submodule's checkout included. A file git ignores is excluded, by the ignore
// files of the nearest repository at or above the root; a root in no repository ignores nothing. Generated
// files are counted as ours here: the format and lint phases decide about those.
func TakeCensus(workspace Workspace) (Census, error) {
	var census Census
	repositoryRoot := nearestRepository(workspace.Root)
	var rootMatcher *gitignore.Matcher
	if repositoryRoot != "" {
		matcher, matcherError := gitignore.New(repositoryRoot)
		if matcherError != nil {
			return Census{}, fmt.Errorf("reading %s's ignore files: %w", repositoryRoot, matcherError)
		}
		rootMatcher = matcher
	}
	// The matcher for each directory entered, by its path relative to the repository root, so a file is
	// asked about through the matcher holding every ignore file from the root down to its directory.
	scopes := map[string]*gitignore.Matcher{}

	for _, module := range workspace.Modules {
		moduleDirectory := filepath.Join(workspace.Root, filepath.FromSlash(module.Directory))
		walkError := filepath.WalkDir(moduleDirectory, func(path string, entry fs.DirEntry, walkError error) error {
			if walkError != nil {
				return walkError
			}
			if entry.IsDir() {
				if path != moduleDirectory && !partOfModule(path, entry.Name()) {
					return filepath.SkipDir
				}
				if rootMatcher == nil {
					return nil
				}
				return enterScope(rootMatcher, scopes, repositoryRoot, path)
			}
			if !strings.HasSuffix(entry.Name(), ".go") {
				return nil
			}
			census.Files = append(census.Files, path)
			if rootMatcher != nil {
				relative, relativeError := repositoryRelative(repositoryRoot, path)
				if relativeError != nil {
					return relativeError
				}
				scope, entered := scopes[parentOf(relative)]
				if !entered {
					// Invariant: WalkDir visits a directory before its files, and every visit enters it.
					panic(fmt.Sprintf("project: %s was reached before its directory was entered", path))
				}
				if ignored, _ := scope.Ignored(relative, false); ignored {
					census.Excluded = append(census.Excluded, Exclusion{File: path, Reason: IgnoredByGit})
					return nil
				}
			}
			census.Ours = append(census.Ours, path)
			return nil
		})
		if walkError != nil {
			return Census{}, fmt.Errorf("walking the module at %s: %w", moduleDirectory, walkError)
		}
	}
	slices.Sort(census.Files)
	slices.Sort(census.Ours)
	slices.SortFunc(census.Excluded, func(left Exclusion, right Exclusion) int {
		return cmp.Compare(left.File, right.File)
	})
	return census, nil
}

// partOfModule reports whether a directory below a module's own is part of that module, as go sees it. A
// directory with a `.git` of its own is another repository, found by that rather than by the index's
// gitlinks, which only a go.work root reads: a checked-out submodule, or a clone that no ignore file of ours
// governs. A submodule never checked out is an empty directory, with nothing to count.
func partOfModule(directory string, name string) bool {
	if name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return false
	}
	for _, marker := range []string{"go.mod", ".git"} {
		if _, statError := os.Lstat(filepath.Join(directory, marker)); statError == nil {
			return false
		}
	}
	return true
}

// enterScope records the matcher for a directory the walk enters, from its parent's when the walk came
// through it, and from the repository root's for a module's own directory.
func enterScope(
	rootMatcher *gitignore.Matcher, scopes map[string]*gitignore.Matcher, repositoryRoot string, directory string,
) error {
	relative, relativeError := repositoryRelative(repositoryRoot, directory)
	if relativeError != nil {
		return relativeError
	}
	from, parentEntered := scopes[parentOf(relative)]
	if !parentEntered || relative == "" {
		from = rootMatcher
	}
	entered, enterError := from.Enter(relative)
	if enterError != nil {
		return fmt.Errorf("reading the ignore files above %s: %w", directory, enterError)
	}
	scopes[relative] = entered
	return nil
}

// repositoryRelative is a path relative to the repository root, slash separated, "" for the root itself, as
// the matcher keys on.
func repositoryRelative(repositoryRoot string, path string) (string, error) {
	relative, relativeError := filepath.Rel(repositoryRoot, path)
	if relativeError != nil {
		return "", fmt.Errorf("placing %s in the repository at %s: %w", path, repositoryRoot, relativeError)
	}
	if relative == "." {
		return "", nil
	}
	return filepath.ToSlash(relative), nil
}

// parentOf is the directory holding a repository-relative path, "" at the root.
func parentOf(relative string) string {
	index := strings.LastIndexByte(relative, '/')
	if index < 0 {
		return ""
	}
	return relative[:index]
}

// nearestRepository is the nearest directory at or above start holding a `.git`, a clone's directory or a
// worktree's or submodule's file, whose ignore files are the ones that govern start, or "" when there is
// none.
func nearestRepository(start string) string {
	directory := filepath.Clean(start)
	for {
		if _, statError := os.Lstat(filepath.Join(directory, ".git")); statError == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}
