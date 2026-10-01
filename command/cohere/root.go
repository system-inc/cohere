package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// projectMarker is the file whose directory is a project's root.
//
// The tsconfig alone, and not CohereSettings.json beside it, for two reasons. It is what every phase
// actually stands on: the type graph cannot be built without it, while the lint config is only
// loaded by the phases that lint, so `--types` is a complete run in a directory that has a tsconfig
// and no lint config. And it is the rule tsc itself uses when given no project, so `cohere` and
// `tsc` started in the same directory check the same program.
//
// Letting the lint config anchor the walk would be worse rather than merely different. On the ahra
// tree `projects/www-ahras-world` holds its own tsconfig and no lint config, and the root tsconfig
// excludes `projects/` as separate programs. Walking past the nearer tsconfig to find a lint config
// would check ahra's program from inside a directory that program excludes, and print a confident
// green about code the caller was not standing in. Anchoring on the tsconfig instead fails loudly
// there, naming the lint config it could not find, which is the answer a reader can act on.
const projectMarker = "tsconfig.json"

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

	// ConfigFileName is the absolute path of the tsconfig that defines the program.
	ConfigFileName string

	// LintConfigFileName is the absolute path of the lint config.
	LintConfigFileName string

	// ArgumentBase is the directory a path typed on the command line resolves against.
	ArgumentBase string

	// WorkingDirectory is where the process was started, kept so the report can say when the root
	// it checked is somewhere else.
	WorkingDirectory string
}

// locationRequest is what the command line said, before anything was resolved.
type locationRequest struct {
	// WorkingDirectory is the process's own, absolute.
	WorkingDirectory string

	// Directory is `--directory`, empty when not given.
	Directory string

	ConfigFileName      string
	ConfigFileNameGiven bool

	LintConfigFileName      string
	LintConfigFileNameGiven bool
}

// locateProject decides the project root and resolves every path the run uses against it.
//
// An explicit flag always wins over discovery, in this order:
//
//   - `--directory` is a `cd`: it is the root, and everything relative resolves against it, typed or
//     not, which is what the flag has always meant.
//   - `--tsconfig` names the project, so the root is the directory that holds it. The name itself was
//     typed, so it resolves against the working directory.
//   - Otherwise the root is the nearest directory at or above the working directory that holds a
//     tsconfig.json. See projectMarker for why that file alone.
//
// Discovery does not stop at a repository boundary, and that is deliberate: a submodule such as
// `libraries/structure` holds no tsconfig of its own and is part of the program above it, so
// stopping at its `.git` would refuse the most common place someone stands inside a project.
func locateProject(request locationRequest) (projectLocation, error) {
	base := request.WorkingDirectory
	if request.Directory != "" {
		base = absoluteFrom(request.WorkingDirectory, request.Directory)
	}
	location := projectLocation{ArgumentBase: base, WorkingDirectory: request.WorkingDirectory}

	switch {
	case request.Directory != "":
		location.Root = base
		location.ConfigFileName = absoluteFrom(base, request.ConfigFileName)

	case request.ConfigFileNameGiven:
		location.ConfigFileName = absoluteFrom(base, request.ConfigFileName)
		location.Root = filepath.Dir(location.ConfigFileName)

	default:
		root, err := findProjectRoot(base)
		if err != nil {
			return projectLocation{}, err
		}
		location.Root = root
		location.ConfigFileName = filepath.Join(root, request.ConfigFileName)
	}

	if request.LintConfigFileNameGiven {
		location.LintConfigFileName = absoluteFrom(base, request.LintConfigFileName)
	} else {
		location.LintConfigFileName = absoluteFrom(location.Root, request.LintConfigFileName)
	}
	return location, nil
}

// findProjectRoot walks up from a directory to the nearest one holding a tsconfig.json.
//
// Finding nothing is an error that names where the walk began, never a fallback to the starting
// directory. A fallback would hand the graph builder a tsconfig path that does not exist, and the
// error that came back would name a file in the wrong directory, which sends a reader looking for a
// typo rather than for the fact that they are standing outside any project.
func findProjectRoot(start string) (string, error) {
	directory := filepath.Clean(start)
	for {
		information, err := os.Stat(filepath.Join(directory, projectMarker))
		if err == nil && !information.IsDir() {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf(
				"no %s in %s or any directory above it, so there is no project here to check: "+
					"run from inside one, or name it with -tsconfig or -directory",
				projectMarker, start,
			)
		}
		directory = parent
	}
}

// absoluteFrom resolves a path against a directory unless it is already absolute.
func absoluteFrom(directory string, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(directory, path)
}

// rootNote is the sentence the report carries when the root is not where the run started, and empty
// when it is.
//
// Silent in the ordinary case, because a line on every run is one people learn to skip. When the two
// differ it is the most important context in the output: a green run from `modules/tasks` that
// checked the whole of ahra and a green run that checked only `modules/tasks` would otherwise print
// the same verdict, and only one of them is what the caller probably assumed.
func (l projectLocation) rootNote() string {
	if filepath.Clean(l.Root) == filepath.Clean(l.WorkingDirectory) {
		return ""
	}
	return fmt.Sprintf("checked the project at %s (the run started in %s)", l.Root, l.WorkingDirectory)
}
