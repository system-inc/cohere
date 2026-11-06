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
		// ForTest is what tells a test variant from the package it tests without reading the opaque ID. Module is
		// what gives a pass its module's path, so a rule that judges a package's path below its module root, as
		// consistency_no_utils_folder does, never judges the module's own name, which nobody inside it can rename.
		// EmbedFiles no rule reads; they are loaded so the inputs record names them (see inputs.go).
		Mode:  packages.LoadAllSyntax | packages.NeedForTest | packages.NeedModule | packages.NeedEmbedFiles,
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
