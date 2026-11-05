// Package load is the Go engine's one type-checked read of a workspace: go/packages with every package and
// dependency loaded from source, as checker.Analyze requires, test variants included. What the load could not
// list, parse or type-check is the engine's types phase, so every such error comes back as a diagnostic, and a
// file the build constraints left out, which no rule will see, comes back named rather than silently missed.
//
// It takes and returns plain values; the engine turns them into contract records.
package load

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"
)

// Loaded is one load of a workspace.
type Loaded struct {
	// Packages are the ones to analyze: each package with its in-package tests ("p [p.test]") in place of the
	// package alone, each external test package ("p_test [p.test]"), and never go test's synthesized main.
	// Their Imports reach the whole graph as go/packages loaded it.
	Packages []*packages.Package

	// Diagnostics are every list, parse and type error anywhere in the graph, and every test function go test
	// would refuse, each once, sorted by file and position.
	Diagnostics []Diagnostic

	// Excluded are the workspace files the build constraints left out, each once, sorted by file: those a
	// loaded package lists as ignored, and those of the census no package holds, as in a directory whose every
	// file is left out, which go list matches as no package at all.
	Excluded []Excluded

	// Unseen are the files of the census no package holds that the build constraints would build, sorted: a
	// gap, since nothing type-checked them.
	Unseen []string

	// PackagesLoaded counts every package in the graph, dependencies and test variants included.
	PackagesLoaded int

	// PackagesInWorkspace counts Packages.
	PackagesInWorkspace int

	// FilesTypeChecked counts the distinct workspace .go files Packages type-checked.
	FilesTypeChecked int

	// Elapsed is how long the whole load took.
	Elapsed time.Duration
}

// Packages loads the packages the patterns name from directory, with the current environment plus
// environment (such as `GOWORK=off`), and accounts for census, the project's own absolute list of its .go
// files: each comes back held by a package, in Excluded, or in Unseen. It returns an error only when
// nothing could be loaded at all; what the packages themselves get wrong is Loaded.Diagnostics.
func Packages(directory string, patterns []string, environment []string, census []string) (*Loaded, error) {
	started := time.Now()
	workspace, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		return nil, fmt.Errorf("resolving %s: %w", directory, absoluteError)
	}
	fullEnvironment, environmentError := goEnvironment(workspace, environment)
	if environmentError != nil {
		return nil, environmentError
	}
	roots, loadError := packages.Load(&packages.Config{
		// ForTest is what tells a test variant from the package it tests without reading the opaque ID.
		Mode:  packages.LoadAllSyntax | packages.NeedForTest,
		Tests: true,
		Dir:   workspace,
		Env:   fullEnvironment,
	}, patterns...)
	if loadError != nil {
		return nil, fmt.Errorf("loading %s from %s: %w", strings.Join(patterns, " "), workspace, loadError)
	}

	loaded := &Loaded{Packages: analyzed(roots)}
	loaded.PackagesInWorkspace = len(loaded.Packages)
	loaded.Diagnostics = diagnostics(workspace, roots, loaded.Packages)
	packages.Visit(roots, nil, func(*packages.Package) { loaded.PackagesLoaded++ })
	loaded.FilesTypeChecked = filesTypeChecked(workspace, loaded.Packages)
	excluded, unseen, excludedError := excludedAndUnseen(workspace, fullEnvironment, roots, census)
	if excludedError != nil {
		return nil, excludedError
	}
	loaded.Excluded, loaded.Unseen = excluded, unseen
	loaded.Elapsed = time.Since(started)
	return loaded, nil
}

// filesTypeChecked counts the distinct .go files inside workspace that the analyzed packages hold. It reads
// GoFiles, the files as written, since a cgo package's CompiledGoFiles are generated files in the build cache.
func filesTypeChecked(workspace string, analyzedPackages []*packages.Package) int {
	seen := map[string]bool{}
	for _, analyzedPackage := range analyzedPackages {
		for _, file := range analyzedPackage.GoFiles {
			if inside(workspace, file) {
				seen[file] = true
			}
		}
	}
	return len(seen)
}

// goEnvironment is the whole environment every go command here runs with in workspace: the current one, then
// environment, then `GOFLAGS` as go reads it there (from the environment, or the go env file when that is
// empty) with every `-trimpath` taken out and `-trimpath=false` put last. Analysis needs real source paths:
// under -trimpath go keys a cgo package's translation without its directory, so a load is served the
// translation cached for the same bytes in another directory, whose `//line` paths name that directory's files,
// gone once it is, and cgocall, which reads the raw file those paths name, fails. The value is never empty,
// since an empty `GOFLAGS` would hand go the go env file's flags, -trimpath among them.
func goEnvironment(workspace string, environment []string) ([]string, error) {
	given := append(os.Environ(), environment...)
	command := exec.Command("go", "env", "GOFLAGS")
	command.Dir = workspace
	command.Env = given
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("reading GOFLAGS with go env in %s: %w: %s",
			workspace, runError, strings.TrimSpace(standardError.String()))
	}
	kept := []string{}
	for _, flag := range strings.Fields(string(output)) {
		name, _, _ := strings.Cut(strings.TrimLeft(flag, "-"), "=")
		if name != "trimpath" {
			kept = append(kept, flag)
		}
	}
	kept = append(kept, "-trimpath=false")
	result := []string{}
	for _, variable := range given {
		if !strings.HasPrefix(variable, "GOFLAGS=") {
			result = append(result, variable)
		}
	}
	return append(result, "GOFLAGS="+strings.Join(kept, " ")), nil
}

// inside reports whether path is in workspace or below it.
func inside(workspace, path string) bool {
	return strings.HasPrefix(path, workspace+string(filepath.Separator))
}
