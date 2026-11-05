// Package project reads which modules a Go root owns and which `.go` files are ours, before anything is
// loaded. cohere-dev's landing gate and the Go engine both read it, so the two can never disagree about which
// modules a repository builds.
//
// A root is the directory of a go.work or of a go.mod. A go.work's modules are its use directives, read with
// the parser `go work edit` uses rather than by running go, so the land fixture's stand-in go doesn't answer
// for it. A module inside a submodule (a gitlink in the index, by rule, never by name) is not owned: its
// tests and its findings are its upstream's.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// Module is one module a workspace owns: where it is, relative to the root and slash separated, and its
// module path. The root module's path is not read, since its pattern is ./... and needs none.
type Module struct {
	Directory string
	Path      string
}

// Workspace is what a root owns: its modules, the uses it leaves to a submodule's upstream, and the
// submodules themselves.
type Workspace struct {
	// Root is the absolute directory of the go.work or the go.mod.
	Root string

	Modules     []Module
	InSubmodule []string

	// Submodules are the gitlinks of the index, relative to Root. Only a go.work root reads them.
	Submodules []string

	// FromWorkspace is whether Root holds a go.work. Without one Root is a single module.
	FromWorkspace bool
}

// ReadWorkspace reads the modules a root owns. Without a go.work it is the root module alone. A module
// the go.work does not use is not refused here: the engine reads it as a project of its own, and a gate
// that refuses one calls RefuseUnlistedModules too.
func ReadWorkspace(root string) (Workspace, error) {
	absoluteRoot, absoluteError := filepath.Abs(root)
	if absoluteError != nil {
		return Workspace{}, fmt.Errorf("resolving %s: %w", root, absoluteError)
	}
	contents, readError := os.ReadFile(filepath.Join(absoluteRoot, "go.work"))
	if errors.Is(readError, fs.ErrNotExist) {
		return Workspace{Root: absoluteRoot, Modules: []Module{{Directory: "."}}}, nil
	}
	if readError != nil {
		return Workspace{}, readError
	}
	work, parseError := modfile.ParseWork("go.work", contents, nil)
	if parseError != nil {
		return Workspace{}, parseError
	}
	links, linksError := gitlinks(absoluteRoot)
	if linksError != nil {
		return Workspace{}, linksError
	}

	covered := Workspace{Root: absoluteRoot, Submodules: links, FromWorkspace: true}
	for _, use := range work.Use {
		directory := filepath.ToSlash(filepath.Clean(use.Path))
		if insideAny(directory, links) {
			covered.InSubmodule = append(covered.InSubmodule, directory)
			continue
		}
		module := Module{Directory: directory}
		if directory != "." {
			goMod, goModError := os.ReadFile(filepath.Join(absoluteRoot, directory, "go.mod"))
			if goModError != nil {
				return Workspace{}, fmt.Errorf("go.work uses %s: %w", directory, goModError)
			}
			module.Path = modfile.ModulePath(goMod)
			if module.Path == "" {
				return Workspace{}, fmt.Errorf("go.work uses %s, whose go.mod names no module", directory)
			}
		}
		covered.Modules = append(covered.Modules, module)
	}
	return covered, nil
}

// RefuseUnlistedModules walks the tree for go.mod files and refuses any that go.work doesn't use and that
// isn't inside a submodule, naming each with the use line that would list it. A testdata directory holds
// fixtures, not modules, and a dot directory or .cache holds nothing the repository builds, so neither is
// walked; nor is a submodule, whose modules are its upstream's. A root without a go.work lists nothing, so
// it refuses nothing.
func (workspace Workspace) RefuseUnlistedModules() error {
	if !workspace.FromWorkspace {
		return nil
	}
	used := map[string]bool{}
	for _, module := range workspace.Modules {
		used[module.Directory] = true
	}
	for _, directory := range workspace.InSubmodule {
		used[directory] = true
	}
	var unlisted []string
	walkError := filepath.WalkDir(workspace.Root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		relative, relativeError := filepath.Rel(workspace.Root, path)
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			name := entry.Name()
			if relative != "." &&
				(name == "testdata" || strings.HasPrefix(name, ".") || insideAny(relative, workspace.Submodules)) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "go.mod" {
			return nil
		}
		directory := filepath.ToSlash(filepath.Dir(relative))
		if !used[directory] {
			unlisted = append(unlisted, directory)
		}
		return nil
	})
	if walkError != nil {
		return walkError
	}
	if len(unlisted) == 0 {
		return nil
	}
	var message strings.Builder
	message.WriteString("go.work does not use every module in the tree, so a gate would leave these untested:")
	for _, directory := range unlisted {
		fmt.Fprintf(&message, "\n  %s/go.mod: add `./%s` to go.work's use", directory, directory)
	}
	return errors.New(message.String())
}

// Patterns are the package patterns one go invocation from the root takes to cover every owned module: the
// root's ./..., and each other module's path with /..., which reaches its packages through the workspace.
func (workspace Workspace) Patterns() []string {
	patterns := make([]string, 0, len(workspace.Modules))
	for _, module := range workspace.Modules {
		if module.Directory == "." {
			patterns = append(patterns, "./...")
			continue
		}
		patterns = append(patterns, module.Path+"/...")
	}
	return patterns
}

// Environment is what a go command run on the root adds to its environment. A module root runs with
// `GOWORK=off`: a go.work above it that does not use it would make go refuse the module, and one that does
// use it is never handed over as a root of its own. A workspace root needs nothing.
func (workspace Workspace) Environment() []string {
	if workspace.FromWorkspace {
		return nil
	}
	return []string{"GOWORK=off"}
}

// insideAny reports whether directory is one of paths or inside one.
func insideAny(directory string, paths []string) bool {
	for _, path := range paths {
		if directory == path || strings.HasPrefix(directory, path+"/") {
			return true
		}
	}
	return false
}
