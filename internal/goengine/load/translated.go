package load

import (
	"fmt"
	"strings"

	"golang.org/x/tools/go/packages"
)

// translated loads the patterns through go/packages, with go list -compiled and -test, so the cgo tool translates
// every cgo file and the project's own cgo code is type-checked as it builds, never with package "C" faked. It is
// the load for a project with cgo files of its own, and needs a go that builds: a runner of prebuilt tests, which
// refuses -compiled, cannot run it. Nothing is withheld, since nothing is faked.
func translated(workspace string, patterns []string, environment []string) ([]*packages.Package, *Loaded, error) {
	fullEnvironment, environmentError := translatedEnvironment(workspace, environment)
	if environmentError != nil {
		return nil, nil, environmentError
	}
	roots, loadError := packages.Load(&packages.Config{
		// ForTest is what tells a test variant from the package it tests without reading the opaque ID.
		Mode:  packages.LoadAllSyntax | packages.NeedForTest,
		Tests: true,
		Dir:   workspace,
		Env:   fullEnvironment,
	}, patterns...)
	if loadError != nil {
		return nil, nil, fmt.Errorf("loading %s from %s: %w", strings.Join(patterns, " "), workspace, loadError)
	}
	return roots, &Loaded{
		Packages:                analyzed(roots),
		WithheldInDependencyCgo: []Withheld{},
		CgoTranslated:           true,
	}, nil
}
