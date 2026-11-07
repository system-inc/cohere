package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
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

// swiftProjectMarker is the file whose directory is a Swift package's root, and the run there belongs
// to the Swift engine rather than to this binary's own phases.
//
// Discovery looks for both markers at each directory on the way up, and the nearest directory holding
// either wins, for the reason projectMarker gives: the nearer project is the one the caller is
// standing in. From inside `projects/ahraos-macos` that is its Package.swift, never ahra's tsconfig
// above it, which would check a program that excludes `projects/` and print a green about Swift code
// nothing looked at.
//
// For one engine's run, a directory holding both is TypeScript's. A run from the root checks both: discovery
// finds each marker as a project of its own, and each project's run names its engine (see discovery.go).
const swiftProjectMarker = "Package.swift"

// goWorkspaceMarker is the file whose directory is a Go workspace's root, and the run there belongs to the
// Go engine, over every module the workspace uses.
//
// It shares the walk with the other markers and comes after them in one directory, so a tsconfig.json or a
// Package.swift beside it keeps the run it had. Ahead of goModuleMarker, because a directory holding both is
// one project, the workspace: go itself builds the workspace there, its own module among the uses.
const goWorkspaceMarker = "go.work"

// goModuleMarker is the file whose directory is a Go module's root, last of the four markers.
//
// A module is the nearest marker but not always the root: go builds a module inside the workspace whose
// go.work is nearest above it, when that workspace uses it, so the walk up keeps looking for that go.work
// and stands where go would (see goModuleRoot).
const goModuleMarker = "go.mod"

// projectEngine is which engine checks the project at a root.
type projectEngine string

const (
	// engineTypeScript is this binary's own pipeline, over the program a tsconfig defines.
	engineTypeScript projectEngine = "TypeScript"

	// engineSwift is cohere-swift, which this binary runs and whose records it renders.
	engineSwift projectEngine = "Swift"

	// engineGo is the Go engine, over a workspace a go.work defines or a module a go.mod does.
	engineGo projectEngine = "Go"

	// engineFormat is this binary's format phase alone, at a repository root no TypeScript project holds: the
	// JSON, Markdown and the rest the house formatter reads there, which no other engine formats. It has no
	// marker, since discovery adds it (see addRootFormatProject).
	engineFormat projectEngine = "Format"
)

// locationRequest is what the command line said, before anything was resolved.
type locationRequest struct {
	// WorkingDirectory is the process's own, absolute.
	WorkingDirectory string

	// Directory is `--directory`, empty when not given.
	Directory string

	// Engine, when set, is the engine that checks Directory, overriding what its markers say: the run of a
	// discovered project names it, since a directory holding both markers is two projects (#f9nftxz).
	Engine projectEngine

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
//     tsconfig.json, a Package.swift, a go.work or a go.mod, except that a Go module a workspace above
//     it uses is that workspace's. See projectMarker for why the tsconfig and not the lint config,
//     swiftProjectMarker for how the markers share the walk, and goModuleMarker for the workspace.
//
// The engine follows the marker. `--tsconfig` names a TypeScript program, so it is always TypeScript.
// `--directory` is a `cd`, so the directory it names is read for markers the way discovery reads each
// directory it passes.
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
		// A directory with neither marker stays TypeScript, so the graph builder fails naming the
		// tsconfig it could not find, as it did before Swift existed.
		location.Engine = engineTypeScript
		if engine, found := engineAt(base); found {
			location.Engine = engine
		}
		if request.Engine != "" {
			location.Engine = request.Engine
		}

	case request.ConfigFileNameGiven:
		location.ConfigFileName = absoluteFrom(base, request.ConfigFileName)
		location.Root = filepath.Dir(location.ConfigFileName)
		location.Engine = engineTypeScript

	default:
		root, engine, findProjectRootError := findProjectRoot(base)
		if findProjectRootError != nil {
			return projectLocation{}, findProjectRootError
		}
		location.Root = root
		location.Engine = engine
		if engine == engineTypeScript {
			location.ConfigFileName = filepath.Join(root, request.ConfigFileName)
		}
	}

	location.LintConfigFileNameGiven = request.LintConfigFileNameGiven
	if request.LintConfigFileNameGiven {
		location.LintConfigFileName = absoluteFrom(base, request.LintConfigFileName)
	} else {
		location.LintConfigFileName = absoluteFrom(location.Root, request.LintConfigFileName)
	}
	return location, nil
}

// findProjectRoot walks up from a directory to the nearest one holding a tsconfig.json, a Package.swift,
// a go.work or a go.mod, and says which engine that marker belongs to. A go.mod alone hands the walk on
// to goModuleRoot, which finds the workspace go would build it in.
//
// Finding nothing is an error that names where the walk began, never a fallback to the starting
// directory. A fallback would hand the graph builder a tsconfig path that does not exist, and the
// error that came back would name a file in the wrong directory, which sends a reader looking for a
// typo rather than for the fact that they are standing outside any project.
func findProjectRoot(start string) (string, projectEngine, error) {
	directory := filepath.Clean(start)
	for {
		if engine, found := engineAt(directory); found {
			if engine == engineGo && !isRegularFile(filepath.Join(directory, goWorkspaceMarker)) {
				root, goModuleRootError := goModuleRoot(directory)
				return root, engineGo, goModuleRootError
			}
			return directory, engine, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", "", fmt.Errorf(
				"no %s, %s, %s or %s in %s or any directory above it, so there is no project here to check: "+
					"run from inside one, or name it with -tsconfig or -directory",
				projectMarker, swiftProjectMarker, goWorkspaceMarker, goModuleMarker, start,
			)
		}
		directory = parent
	}
}

// engineAt says which engine owns a directory by the marker it holds: TypeScript, then Swift, then Go by
// either of its markers. See swiftProjectMarker for why a directory holding more than one is the first
// one's, and goWorkspaceMarker for why the Go markers come last.
func engineAt(directory string) (projectEngine, bool) {
	if isRegularFile(filepath.Join(directory, projectMarker)) {
		return engineTypeScript, true
	}
	if isRegularFile(filepath.Join(directory, swiftProjectMarker)) {
		return engineSwift, true
	}
	if isRegularFile(filepath.Join(directory, goWorkspaceMarker)) ||
		isRegularFile(filepath.Join(directory, goModuleMarker)) {
		return engineGo, true
	}
	return "", false
}

// goModuleRoot is the root a Go module's run stands on: the directory of the nearest go.work above it, when
// that workspace uses the module, since that is the build go itself runs there, and the module's own
// directory otherwise. Only the nearest go.work counts, as with go, and the search stops at the enclosing
// repository's root, since a go.work above it belongs to some other tree.
func goModuleRoot(module string) (string, error) {
	boundary := enclosingRepository(module)
	for directory := module; directory != boundary; {
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
		if !isRegularFile(filepath.Join(directory, goWorkspaceMarker)) {
			continue
		}
		uses, workspaceUsesError := workspaceUses(directory)
		if workspaceUsesError != nil {
			return "", workspaceUsesError
		}
		for _, use := range uses {
			if filepath.Join(directory, filepath.FromSlash(use)) == module {
				return directory, nil
			}
		}
		return module, nil
	}
	return module, nil
}

// workspaceUses reads the use directives of the go.work in a directory, as cleaned slash paths relative to
// it, with the parser `go work edit` uses rather than by running go. A go.work that does not parse is an
// error naming it, never read as absent, since a workspace read as empty would run its modules apart. A use
// that cannot be put relative to the directory, an absolute path on another volume, is left out: nothing
// under the directory is there.
func workspaceUses(directory string) ([]string, error) {
	workspaceFile := filepath.Join(directory, goWorkspaceMarker)
	contents, readError := os.ReadFile(workspaceFile)
	if readError != nil {
		return nil, fmt.Errorf("reading the Go workspace %s: %w", workspaceFile, readError)
	}
	work, parseWorkError := modfile.ParseWork(goWorkspaceMarker, contents, nil)
	if parseWorkError != nil {
		return nil, fmt.Errorf("the Go workspace %s does not parse: %w", workspaceFile, parseWorkError)
	}
	uses := make([]string, 0, len(work.Use))
	for _, use := range work.Use {
		usePath := filepath.FromSlash(use.Path)
		if filepath.IsAbs(usePath) {
			relative, relativeError := filepath.Rel(directory, usePath)
			if relativeError != nil {
				continue
			}
			usePath = relative
		}
		uses = append(uses, filepath.ToSlash(filepath.Clean(usePath)))
	}
	return uses, nil
}

// isRegularFile reports whether a path names something that is not a directory. A directory named
// `tsconfig.json` is not a project, and treating it as one would build a graph from nothing.
func isRegularFile(path string) bool {
	information, statError := os.Stat(path)
	return statError == nil && !information.IsDir()
}

// absoluteFrom resolves a path against a directory unless it is already absolute.
func absoluteFrom(directory string, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(directory, path)
}
