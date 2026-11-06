package go_engine

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/gitignore"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/settings"
)

// inputsRecord is the run's inputs record (contract.InputsRecord says what it is for and the rule it holds): what
// the load read, which load.Inputs names, and what describing the project read before it to decide which files are
// ours and which rules run. That is the root's go.work and go.mod, the settings file, each file of ours that could not
// be read (named as a file, so a replay that cannot read it either runs cold), and what the census reads git's
// ignore rules from: the nearest repository at or above the root, so each directory above the root and below it
// has no `.git`; that repository's `.git` when it is a file, a worktree's or a submodule's pointer to its git
// directory; its info/exclude when its `.git` is a directory; and a `.gitignore` in each directory from the
// repository down to the root and in each directory the load names inside the repository. git's global excludes file
// is not one: the census never reads it (see package gitignore). At a go.work root the census also reads the
// repository's index for its gitlinks, so the index is named, which costs a miss whenever git rewrites it.
//
// The rules read more: staticcheck's config analyzer reads a staticcheck.conf in each analyzed package's directory
// and every directory above it, to the filesystem's root, so each is named, there or absent.
func (engine *pipeline) inputsRecord(loaded *load.Loaded) contract.InputsRecord {
	files := map[string]bool{}
	absent := map[string]bool{}
	for _, file := range loaded.Inputs.Files {
		files[file] = true
	}
	for _, path := range loaded.Inputs.Absent {
		absent[path] = true
	}
	fileOrAbsent := func(path string) {
		cleaned := filepath.Clean(path)
		if _, statError := os.Lstat(cleaned); statError == nil {
			files[cleaned] = true
			return
		}
		absent[cleaned] = true
	}

	root := engine.described.workspace.Root
	fileOrAbsent(filepath.Join(root, "go.work"))
	fileOrAbsent(filepath.Join(root, "go.mod"))
	fileOrAbsent(settings.Path(engine.request.root, engine.request.lintConfiguration))
	for _, unreadable := range engine.described.unreadable {
		files[filepath.Clean(unreadable.File)] = true
	}

	// The directories the census's ignore rules are read down through, from the root up to the repository's.
	var above []string
	repository := ""
	for directory := filepath.Clean(root); ; directory = filepath.Dir(directory) {
		above = append(above, directory)
		marker := filepath.Join(directory, ".git")
		information, statError := os.Lstat(marker)
		if statError == nil {
			repository = directory
			if information.IsDir() {
				fileOrAbsent(filepath.Join(directory, filepath.FromSlash(gitignore.ExcludeFile)))
			} else {
				files[marker] = true
			}
			if engine.described.workspace.FromWorkspace {
				fileOrAbsent(gitIndex(marker, information.IsDir()))
			}
			break
		}
		absent[marker] = true
		if filepath.Dir(directory) == directory {
			break
		}
	}
	if repository != "" {
		for _, directory := range above {
			fileOrAbsent(filepath.Join(directory, ".gitignore"))
		}
		for _, directory := range loaded.Inputs.Directories {
			// A `.gitignore` appearing here changes the directory's listing, which is named already, so only one
			// that is there is named, as a file.
			if !insideScope(directory, []string{repository}) {
				continue
			}
			ignoreFile := filepath.Join(directory, ".gitignore")
			if _, statError := os.Lstat(ignoreFile); statError == nil {
				files[ignoreFile] = true
			}
		}
	}

	for _, analyzed := range loaded.Packages {
		if analyzed.Dir == "" {
			continue
		}
		for directory := filepath.Clean(analyzed.Dir); ; directory = filepath.Dir(directory) {
			fileOrAbsent(filepath.Join(directory, "staticcheck.conf"))
			if filepath.Dir(directory) == directory {
				break
			}
		}
	}

	return contract.InputsRecord{
		Files:       slices.Sorted(maps.Keys(files)),
		Directories: slices.Clone(loaded.Inputs.Directories),
		Absent:      slices.Sorted(maps.Keys(absent)),
	}
}

// gitIndex is the index git reads for the repository whose `.git` is marker: `.git/index` when it is a directory, and
// the index in the git directory a `.git` file points to, a worktree's or a submodule's, when it is a file. A pointer
// that cannot be read gives `.git/index` below the file, which is never there; git cannot read such a repository
// either, so the census refuses the run and no record is written.
func gitIndex(marker string, isDirectory bool) string {
	if isDirectory {
		return filepath.Join(marker, "index")
	}
	contents, readError := os.ReadFile(marker)
	if readError != nil {
		return filepath.Join(marker, "index")
	}
	gitDirectory, found := strings.CutPrefix(strings.TrimSpace(string(contents)), "gitdir:")
	if !found {
		return filepath.Join(marker, "index")
	}
	gitDirectory = strings.TrimSpace(gitDirectory)
	if !filepath.IsAbs(gitDirectory) {
		gitDirectory = filepath.Join(filepath.Dir(marker), gitDirectory)
	}
	return filepath.Join(filepath.Clean(gitDirectory), "index")
}
