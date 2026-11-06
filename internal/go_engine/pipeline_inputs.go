package go_engine

import (
	"maps"
	"os"
	"path/filepath"
	"slices"

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
// has no `.git`; that repository's info/exclude when its `.git` is a directory; and a `.gitignore` in each directory
// from the repository down to the root and in each directory the load names inside the repository.
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
		path = filepath.Clean(path)
		if _, statError := os.Lstat(path); statError == nil {
			files[path] = true
			return
		}
		absent[path] = true
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

	return contract.InputsRecord{
		Files:       slices.Sorted(maps.Keys(files)),
		Directories: slices.Clone(loaded.Inputs.Directories),
		Absent:      slices.Sorted(maps.Keys(absent)),
	}
}
