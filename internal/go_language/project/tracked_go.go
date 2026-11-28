package project

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/gitignore"
)

// TrackedGoFile is the census's second witness (#td4wp62): the first Go file, by module and then by path, that the
// index of a module's repository tracks where go would see it, read from each index in process (gitignore.TrackedPaths)
// and never through the census's walk, so a census that wrongly counted nothing is not trusted alone. "" is indexes
// that track no such file. A tracked file is never ignored, so every one go would see is a file of ours the census
// counts.
//
// It judges each tracked path by the rules the census's walk judges a directory by (roleOf) and a file by (goSees),
// from the path's own words and what the disk holds below the module, never from what the walk found: a `.go` file
// whose name starts with neither `_` nor `.`, on disk as anything but a directory, as go reads it, below no directory
// that is testdata, whose name starts with `.` or `_`, that is below a `vendor` directory, that the module's go.mod
// ignores, or that holds a go.mod of its own (a nested module, counted as itself when the workspace owns it) or a
// `.git` (a repository of its own, whose files are never ours). A tracked file deleted from the disk is one go never
// sees, so it is left out too.
//
// An error is a module whose index cannot be read, one in no repository among them: then there is no second witness.
func TrackedGoFile(workspace Workspace) (string, error) {
	// Each repository's tracked paths, by its root, read once however many modules it holds.
	trackedByRepository := map[string][]string{}
	for _, module := range workspace.Modules {
		moduleDirectory := filepath.Join(workspace.Root, filepath.FromSlash(module.Directory))
		// The module's real directory, as the census walks it, so a module a go.work uses through a link is judged by
		// the repository holding its files.
		walked, resolveError := filepath.EvalSymlinks(moduleDirectory)
		if resolveError != nil {
			return "", fmt.Errorf("resolving the module at %s: %w", module.Directory, resolveError)
		}
		repositoryRoot := gitignore.RepositoryOf(walked)
		if repositoryRoot == "" {
			return "", fmt.Errorf("the module at %s is in no repository, so no index says what it tracks",
				moduleDirectory)
		}
		tracked, read := trackedByRepository[repositoryRoot]
		if !read {
			var trackedError error
			tracked, trackedError = gitignore.TrackedPaths(repositoryRoot)
			if trackedError != nil {
				return "", fmt.Errorf("reading what the repository at %s tracks: %w", repositoryRoot, trackedError)
			}
			trackedByRepository[repositoryRoot] = tracked
		}
		directives, directivesError := readIgnoreDirectives(moduleDirectory)
		if directivesError != nil {
			return "", directivesError
		}
		moduleFromRepository, relativeError := repositoryRelative(repositoryRoot, walked)
		if relativeError != nil {
			return "", relativeError
		}
		// What every tracked path below the module starts with: nothing at the repository's own root.
		modulePrefix := ""
		if moduleFromRepository != "" {
			modulePrefix = moduleFromRepository + "/"
		}
		for _, trackedPath := range tracked {
			moduleRelative, inModule := strings.CutPrefix(trackedPath, modulePrefix)
			if inModule && goSeesTracked(walked, moduleRelative, directives) {
				return filepath.Join(moduleDirectory, filepath.FromSlash(moduleRelative)), nil
			}
		}
	}
	return "", nil
}

// goSeesTracked reports whether go would read a tracked file as Go source of the module whose real directory is walked,
// the file named by its slash-separated path relative to that directory (TrackedGoFile).
func goSeesTracked(walked string, moduleRelative string, directives []string) bool {
	if !goSees(path.Base(moduleRelative)) {
		return false
	}
	for directory := path.Dir(moduleRelative); directory != "."; directory = path.Dir(directory) {
		name := path.Base(directory)
		if name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
			path.Base(path.Dir(directory)) == "vendor" || ignoredByDirective(directory, directives) {
			return false
		}
		onDisk := filepath.Join(walked, filepath.FromSlash(directory))
		for _, marker := range []string{"go.mod", ".git"} {
			if _, statError := os.Lstat(filepath.Join(onDisk, marker)); statError == nil {
				return false
			}
		}
	}
	information, statError := os.Lstat(filepath.Join(walked, filepath.FromSlash(moduleRelative)))
	return statError == nil && !information.IsDir()
}
