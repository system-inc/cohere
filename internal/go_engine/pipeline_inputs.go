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
// has no `.git`; that repository's `.git` when it is a file, a worktree's or a submodule's pointer to its git
// directory; its info/exclude when its `.git` is a directory; and a `.gitignore` in each directory from the
// repository down to the root and in each directory the load names inside the repository. git's global excludes file
// is not one: the census never reads it (see package gitignore). The census also reads the repository's index, since
// a path git tracks is never ignored, and at a go.work root for its gitlinks too, so the index is named, there or
// absent, and when there, named again under TrackedIgnored with the repository and the root below it, so the front
// door keys it on what it decides for the census rather than on bytes git rewrites on every `git status` (#m1daxxk).
//
// The rules read more: staticcheck's config analyzer reads a staticcheck.conf in each analyzed package's directory
// and every directory above it, to the filesystem's root, so each is named, there or absent.
//
// Under `--other-builds` the record is every build's (#e4b08k7): each build's load is named, and the directories of
// the files each build compiled are read for staticcheck.conf, so a replay misses when anything any build read moves.
func (engine *pipeline) inputsRecord(loaded *load.Loaded) contract.InputsRecord {
	files := map[string]bool{}
	absent := map[string]bool{}
	directories := map[string]bool{}
	// Under `--other-builds` every build's load is named beside the host's (#e4b08k7): another build reads files the
	// host's never does, the standard library's and a module's that only its platform builds among them, and its
	// verdict is the run's as much as the host's is, so a replay keyed on the host's load alone would serve a stale
	// verdict after an edit to one of them.
	for _, inputs := range append([]load.Inputs{loaded.Inputs}, engine.buildInputs()...) {
		for _, file := range inputs.Files {
			files[file] = true
		}
		for _, path := range inputs.Absent {
			absent[path] = true
		}
		for _, directory := range inputs.Directories {
			directories[directory] = true
		}
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
	var trackedIgnored []contract.TrackedIgnoredScope
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
			index := filepath.Clean(gitignore.IndexFile(marker, information.IsDir()))
			fileOrAbsent(index)
			// The census asks the index only which of the files below the root the ignore rules exclude are tracked,
			// and a go.work root which of its paths are gitlinks, so the index is keyed on that, below the root.
			scope, relativeError := filepath.Rel(directory, filepath.Clean(root))
			if files[index] && relativeError == nil {
				if scope == "." {
					scope = ""
				}
				trackedIgnored = append(trackedIgnored,
					contract.TrackedIgnoredScope{Index: index, Root: directory, Scope: filepath.ToSlash(scope)})
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
		for _, directory := range slices.Sorted(maps.Keys(directories)) {
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

	analyzedDirectories := map[string]bool{}
	for _, analyzed := range loaded.Packages {
		if analyzed.Dir != "" {
			analyzedDirectories[filepath.Clean(analyzed.Dir)] = true
		}
	}
	// Another build analyzes a package of ours the host's never loads when every file of it is one only that build
	// compiles, so the directory of each file it compiled is one the rules read staticcheck.conf from too.
	for _, run := range engine.buildRuns {
		for file := range run.compiled {
			analyzedDirectories[filepath.Dir(filepath.Clean(file))] = true
		}
	}
	for start := range analyzedDirectories {
		for directory := start; ; directory = filepath.Dir(directory) {
			fileOrAbsent(filepath.Join(directory, "staticcheck.conf"))
			if filepath.Dir(directory) == directory {
				break
			}
		}
	}

	return contract.InputsRecord{
		Files:          slices.Sorted(maps.Keys(files)),
		Directories:    slices.Sorted(maps.Keys(directories)),
		Absent:         slices.Sorted(maps.Keys(absent)),
		TrackedIgnored: trackedIgnored,
	}
}

// buildInputs is what every build beside the host's loaded, in the order the merge was handed them, and nil for a run
// of one build. The host's run is first in engine.buildRuns and carries the host's load's inputs, which the record
// names already.
func (engine *pipeline) buildInputs() []load.Inputs {
	if len(engine.buildRuns) < 2 {
		return nil
	}
	inputs := make([]load.Inputs, 0, len(engine.buildRuns)-1)
	for _, run := range engine.buildRuns[1:] {
		inputs = append(inputs, run.inputs)
	}
	return inputs
}
