package main

import (
	"fmt"
	"path/filepath"
)

// projectLocation is where a run is anchored, and which directory each kind of path resolves against.
//
// Two directories, because there are two kinds of path and they mean different things. A path the
// project supplies by default (its tsconfig, its lint config) belongs to the project and resolves
// against its root. A path the caller typed (a file to check, a config named by flag) belongs to the
// shell it was typed in and resolves against the working directory. Collapsing the two is what made
// `cohere` usable only from the root: the defaults were resolved against wherever the caller stood.
type projectLocation struct {
	// Root is the project's directory: the program's current directory, where git is asked what
	// changed, and where the format walk starts.
	Root string

	// Engine is which engine checks the project at Root, decided by the marker that made it the root.
	Engine projectEngine

	// ConfigFileName is the absolute path of the tsconfig that defines the program, and empty for a
	// Swift package, which has none.
	ConfigFileName string

	// LintConfigFileName is the absolute path of the lint config.
	LintConfigFileName string

	// LintConfigFileNameGiven is whether `--lint-config` named it. A named file that is missing is an
	// error; the default one missing is zero config, the house stack (#bfxz13m).
	LintConfigFileNameGiven bool

	// ArgumentBase is the directory a path typed on the command line resolves against.
	ArgumentBase string

	// WorkingDirectory is where the process was started, kept so the report can say when the root
	// it checked is somewhere else.
	WorkingDirectory string
}

// rootNote is the sentence the report carries when the root is not where the run started, and empty
// when it is.
//
// Silent in the ordinary case, because a line on every run is one people learn to skip. When the two
// differ it is the most important context in the output: a green run from `modules/tasks` that
// checked the whole of ahra and a green run that checked only `modules/tasks` would otherwise print
// the same verdict, and only one of them is what the caller probably assumed.
func (location projectLocation) rootNote() string {
	if filepath.Clean(location.Root) == filepath.Clean(location.WorkingDirectory) {
		return ""
	}
	return fmt.Sprintf("checked the project at %s (the run started in %s)", location.Root, location.WorkingDirectory)
}
