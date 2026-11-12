package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The run cache's half every engine that declares its inputs shares, Go's (go_run_cache.go) and Swift's
// (swift_run_cache.go): the engine names what its verdict was read from in its inputs record, and the front door
// declares that, refuses a record that could never replay, and declines an exit that is no verdict.

// declareEngineRunCacheInputs declares a finished engine run's inputs to the run cache, when its exit is a verdict a
// later run may replay, and otherwise leaves it unrecorded, reporting whether it declared. A run with no session (not
// admitted, `--no-cache`, a listing) has nothing to declare.
//
// A run whose engine wrote no inputs record is never declared, so never recorded, the way a TypeScript run that never
// built its graph is not: with no inputs there is nothing to prove a later tree unchanged against. What is declared is
// the engine's files, the directories it listed as extra directories, its absent paths as absent, and the settings
// file this front door handed it, present or absent. Where a file's holding directory is recorded is the caller's to
// say (runCacheSession.recordedWithin), since the engines name their listings differently.
//
// unnameable is why this engine's run read what its record cannot name, empty when it did not: a Go load that
// translated cgo. Such a run is declined, and said, since it would otherwise never replay with nothing to say why.
func declareEngineRunCacheInputs(run *engineRun, exitCode int, settingsFile string, unnameable string) bool {
	session := activeRunCache
	if session == nil || run.inputs == nil {
		return false
	}
	if reason := engineRunUnrecordable(run, exitCode); reason != "" {
		declineRunCache(reason)
		return false
	}
	if unnameable != "" {
		declineRunCache(unnameable)
		session.note("the run cache did not record this run: " + unnameable)
		return false
	}
	if reason := engineInputsUnreplayable(
		run.language.theEngine(),
		run.inputs,
		session.projectRoots,
		run.language.NamedScratch,
	); reason != "" {
		// Said, and kept for the next run, since a record like this would never replay and nothing else would say
		// why: it is the engine's record that is wrong, and only a cold run every time shows it.
		declineRunCache(reason)
		session.note("the run cache did not record this run: " + reason)
		return false
	}
	declareRunCacheInputs(run.inputs.Files...)
	session.extraDirectories = append(session.extraDirectories, run.inputs.Directories...)
	session.extraAbsent = append(session.extraAbsent, run.inputs.Absent...)
	for _, named := range run.inputs.TrackedIgnored {
		session.trackedIgnored = append(session.trackedIgnored,
			trackedIgnoredIndex{index: named.Index, root: named.Root, scope: named.Scope})
	}
	declareFileOrAbsent(settingsFile)
	return true
}

// declareFileOrAbsent adds path to this run's inputs as a file when something is there, and as absent otherwise.
func declareFileOrAbsent(path string) {
	session := activeRunCache
	if session == nil {
		return
	}
	if _, statError := os.Stat(path); statError == nil {
		session.extraFiles = append(session.extraFiles, path)
	} else {
		session.extraAbsent = append(session.extraAbsent, path)
	}
}

// engineInputsUnreplayable is why an inputs record could never be replayed, empty when it can: a path inside a root's
// cache directory, which this run writes after its record is taken (the table, and any cache of the engine's kept
// beside it), or a listed directory above a root, whose listing moves with everything beside the project. Each would
// make every later run a miss, so the record is refused rather than kept, and the engine's record is what needs
// mending. engine names it in the reason, "the Go engine".
//
// scratch, when not empty, is the directory in the cache directory where the engine keeps a scratch of its own whose
// files its record may name (engineLanguage.NamedScratch): the front door writes nothing there, and the engine writes
// such a file only while a run of it reads it anew, a run the clock then keeps from being recorded.
func engineInputsUnreplayable(engine string, inputs *engineInputsRecord, roots []string, scratch string) string {
	for _, paths := range [][]string{inputs.Files, inputs.Directories, inputs.Absent} {
		for _, path := range paths {
			for _, root := range roots {
				named := scratch != "" && !slices.Contains(inputs.Directories, path) &&
					pathWithin(path, filepath.Join(cacheDirectory(root), scratch))
				if pathWithin(path, cacheDirectory(root)) && !named {
					return fmt.Sprintf("%s named %s, inside the cache directory the run writes, so "+
						"no later run could replay it", engine, path)
				}
			}
		}
	}
	for _, directory := range inputs.Directories {
		for _, root := range roots {
			if directory != root && pathWithin(root, directory) {
				return fmt.Sprintf("%s listed %s, a directory above the root %s whose listing moves "+
					"with everything beside the project, so no later run could replay it", engine, directory, root)
			}
		}
	}
	return ""
}

// pathWithin reports whether path is directory or below it.
func pathWithin(path string, directory string) bool {
	relative, relativeError := filepath.Rel(directory, path)
	return relativeError == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// engineRunUnrecordable is why a finished engine run's exit is no verdict to replay, empty when it is one.
//
// A verdict is exit 0 or 1 from a run that called itself complete. acceptSummary refuses a complete summary whose
// own records show a requested phase that did not run, a crash, a file with no compiler record or a file it could
// not read, so complete is every phase ran and nothing went unchecked, held to the records rather than taken on
// the engine's word. And a run whose fix phase rewrote a file is declined, as a TypeScript run that rewrote one
// is: its verdict describes the tree it read, and the record would sign the tree it wrote. So is a run whose go list
// ran beside a go.work.sum with no side copy (#34r3v1y): go may have rewritten that file after the run read it, and a
// replay would print the gap again over a run in which no go command ran.
func engineRunUnrecordable(run *engineRun, exitCode int) string {
	switch {
	case exitCode != 0 && exitCode != 1:
		return fmt.Sprintf("the run exited %d, which is no verdict about the tree", exitCode)
	case run.summary == nil || run.summary.Complete == nil:
		return "the run ended without a summary"
	case !*run.summary.Complete:
		return "the run did not check everything, and a run that fell short is checked again rather than replayed"
	case run.filesRewritten > 0:
		return fmt.Sprintf("the fix phase rewrote %s, so the tree it read is not the tree on disk",
			counted(run.filesRewritten, "file", "files"))
	case len(run.summary.UnguardedWorkspaceSums) > 0:
		return fmt.Sprintf("go list may have rewritten %s of the tree, so the next run checks again",
			counted(len(run.summary.UnguardedWorkspaceSums), "go.work.sum", "go.work.sum files"))
	}
	return ""
}

// toolOnPath is the tool PATH finds by name, absolute, and where its links lead, or a word saying there is none. A
// tool the engine cannot find fails its run, and a run that failed is never recorded, so the word only has to differ
// from every path.
func toolOnPath(name string, lookPath func(string) (string, error)) string {
	found, lookPathError := lookPath(name)
	if lookPathError != nil {
		return "not found"
	}
	absolute, absoluteError := filepath.Abs(found)
	if absoluteError != nil {
		return "not absolute: " + found
	}
	resolved, resolveError := filepath.EvalSymlinks(absolute)
	if resolveError != nil {
		return absolute + " (links unresolved)"
	}
	return absolute + " resolved " + resolved
}
