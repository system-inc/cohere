// Package load is the Go engine's one type-checked read of a workspace: every package and dependency parsed and
// type-checked from source, as checker.Analyze requires, test variants included, and returned as go/packages
// would have returned them. What the load could not list, parse or type-check is the engine's types phase, so
// every such error comes back as a diagnostic, and a file the build constraints left out, which no rule will see,
// comes back named rather than silently missed.
//
// The only go commands it runs are `go env` and `go list -deps -json -e`, which read and never build, so it runs
// where a runner of prebuilt tests answers only those, as Loom's does. go/packages cannot: it always asks go list
// for -compiled, -test, -export and -pgo, and compiling is what such a runner refuses. So the test variants go
// test would build are built here (graph.go), and no cgo tool runs: a dependency's cgo file is type-checked as
// written, with package "C" faked (withheld_errors.go and cgo_reach.go say what that costs). A project with cgo
// files of its own is checked as written, always: its load goes through go/packages, whose cgo tool translates
// them (translated.go), and so does not run where go may not build.
//
// It takes and returns plain values; the engine turns them into contract records.
package load

import (
	"bytes"
	"fmt"
	"go/build"
	"go/types"
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

	// Roots are what go/packages returns with Tests set: each package the patterns matched, and each test variant
	// go test builds of it, "p [p.test]" beside p and "p_test [p.test]", less go test's synthesized main. Packages
	// are chosen from them; a driver that analyzes every variant, as the copy of analysistest does, takes them all.
	Roots []*packages.Package

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

	// WithheldInDependencyCgo are the type errors no diagnostic reports because they are in a dependency the cgo
	// tool would translate and reach its cgo files (see Withheld), one per file, sorted by file, and never nil.
	WithheldInDependencyCgo []Withheld

	// WithheldDependencyCgoDeclarations counts the declarations of dependencies with cgo files the analyzers were
	// given without what reaches cgo: functions without their bodies, package-level variables without their initial
	// values (see withholdCgoDeclarations).
	WithheldDependencyCgoDeclarations int

	// Withheld is, by package, what the load withheld of it, its own errors and declarations, for a package the
	// findings cache saves; a package it restored has what it had when it was checked.
	Withheld map[*packages.Package]PackageWithheld

	// FilesParsed counts the files the load parsed, and PackagesFromExportData the packages whose types it read
	// from the findings cache's export data rather than checking them; both are 0 on the go/packages load, which
	// counts neither.
	FilesParsed            int
	PackagesFromExportData int

	// CgoTranslated is set when a package of the project has cgo files, so the load went through go/packages and
	// the cgo tool translated every cgo file, rather than from source (see translated).
	CgoTranslated bool

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
	return loadWorkspace(directory, patterns, environment, census, true, nil)
}

// PackagesRestoring is Packages with the findings cache: the load from source parses and type-checks none of the
// packages restorer holds (see Restorer).
func PackagesRestoring(
	directory string,
	patterns []string,
	environment []string,
	census []string,
	restorer Restorer,
) (*Loaded, error) {
	return loadWorkspace(directory, patterns, environment, census, true, restorer)
}

// loadWorkspace is PackagesRestoring. With translateOwnCgo unset it loads from source even a project with cgo files
// of its own, which only the tests ask for, to judge the load from source on such a package.
func loadWorkspace(
	directory string,
	patterns []string,
	environment []string,
	census []string,
	translateOwnCgo bool,
	restorer Restorer,
) (*Loaded, error) {
	started := time.Now()
	workspace, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		return nil, fmt.Errorf("resolving %s: %w", directory, absoluteError)
	}
	fullEnvironment, environmentError := sourceEnvironment(workspace, environment)
	if environmentError != nil {
		return nil, environmentError
	}
	buildContext, contextError := constraintContext(workspace, fullEnvironment)
	if contextError != nil {
		return nil, contextError
	}
	listed, listError := list(workspace, fullEnvironment, patterns)
	if listError != nil {
		return nil, fmt.Errorf("loading %s from %s: %w", strings.Join(patterns, " "), workspace, listError)
	}
	var roots []*packages.Package
	var loaded *Loaded
	var loadError error
	if translateOwnCgo && hasOwnCgo(listed, workspace) {
		roots, loaded, loadError = translated(workspace, patterns, environment, restorer)
	} else {
		roots, loaded, loadError = fromSource(workspace, listed, buildContext, restorer)
	}
	if loadError != nil {
		return nil, loadError
	}

	loaded.Roots = withoutTestMain(roots)
	loaded.PackagesInWorkspace = len(loaded.Packages)
	loaded.Diagnostics = diagnostics(workspace, roots, loaded.Packages)
	packages.Visit(roots, nil, func(*packages.Package) { loaded.PackagesLoaded++ })
	loaded.FilesTypeChecked = filesTypeChecked(workspace, loaded.Packages)
	excluded, unseen, excludedError := excludedAndUnseen(workspace, buildContext, roots, census)
	if excludedError != nil {
		return nil, excludedError
	}
	loaded.Excluded, loaded.Unseen = excluded, unseen
	loaded.Elapsed = time.Since(started)
	return loaded, nil
}

// hasOwnCgo reports whether a listed package inside workspace has cgo files, which go list lists only when cgo is
// on for the target.
func hasOwnCgo(listed *listing, workspace string) bool {
	for _, listedPackage := range listed.packages {
		if len(listedPackage.CgoFiles) > 0 && inside(workspace, listedPackage.Directory+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// fromSource parses and type-checks the listing's graph from source, and returns its roots and what the load
// knows of them before the diagnostics and the counts. The declarations of dependencies that reach their cgo files
// lose what reaches it only once every package is type-checked.
func fromSource(
	workspace string,
	listed *listing,
	buildContext *build.Context,
	restorer Restorer,
) ([]*packages.Package, *Loaded, error) {
	sizes := types.SizesFor("gc", buildContext.GOARCH)
	if sizes == nil {
		return nil, nil, fmt.Errorf("loading %s: go/types knows no sizes for GOARCH %s", workspace,
			buildContext.GOARCH)
	}
	built := newGraph(listed, workspace)
	withheld := newWithheldErrors()
	typeChecker := newLoader(built, sizes, withheld)
	reachable := typeChecker.connect()
	analyzedPackages := analyzedOfGraph(built)
	if restorer != nil {
		typeChecker.restored = restorer.Restore(LoaderSource, loadedOf(reachable), analyzedPackages)
	}
	typeChecker.load(reachable)

	roots := make([]*packages.Package, 0, len(built.roots))
	for _, root := range built.roots {
		roots = append(roots, root.loaded)
	}
	declarations := withholdCgoDeclarations(typeChecker.fileSet, reachable)
	byPackage := map[*packages.Package]PackageWithheld{}
	distinct := map[string]bool{}
	for _, current := range reachable {
		owned := PackageWithheld{Errors: withheld.ownedBy(current.loaded), Declarations: declarations[current.loaded]}
		if restored, held := typeChecker.restored[current.loaded]; held {
			owned.Declarations = restored.Withheld.Declarations
		}
		for _, position := range owned.Declarations {
			distinct[position] = true
		}
		if len(owned.Errors)+len(owned.Declarations) > 0 {
			byPackage[current.loaded] = owned
		}
	}
	return roots, &Loaded{
		Packages:                          analyzedPackages,
		WithheldInDependencyCgo:           withheld.sorted(),
		WithheldDependencyCgoDeclarations: len(distinct),
		Withheld:                          byPackage,
		FilesParsed:                       int(typeChecker.filesParsed.Load()),
		PackagesFromExportData:            int(typeChecker.fromExportData.Load()),
	}, nil
}

// analyzedOfGraph is the roots of the load from source to analyze, as analyzed chooses them from go/packages'
// roots, in the graph's order: each package with tests as "p [p.test]", which holds every file of p, in place of p,
// so no file of p is analyzed and reported twice; each external test package, "p_test [p.test]"; and each package
// with no in-package tests as itself. A package go test builds again for another's tests, "q [p.test]", is no
// root: it is q, analyzed already.
func analyzedOfGraph(built *graph) []*packages.Package {
	var chosen []*packages.Package
	for _, root := range built.roots {
		withTests := built.nodes[root.loaded.ID+" ["+root.loaded.ID+".test]"]
		if root.loaded.ForTest == "" && withTests != nil {
			continue
		}
		chosen = append(chosen, root.loaded)
	}
	return chosen
}

// filesTypeChecked counts the distinct .go files inside workspace that the analyzed packages hold. It reads
// GoFiles, the files as written, since a translated cgo package's CompiledGoFiles are generated files in the build
// cache.
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

// sourceEnvironment is the whole environment every go command of the load from source runs with in workspace:
// `GOFLAGS` as go reads it there (see withGoFlags), every flag kept, with -pgo=off last, as go/packages lists: a
// command with a default.pgo profile would otherwise have go list print a second variant of every package the
// command builds. -trimpath stays as given, since no cgo tool runs and go list prints nothing differently for it.
func sourceEnvironment(workspace string, environment []string) ([]string, error) {
	return withGoFlags(workspace, environment, func(flags []string) []string { return append(flags, "-pgo=off") })
}

// translatedEnvironment is the whole environment go/packages runs with in workspace when it translates the
// project's cgo files: `GOFLAGS` as go reads it there (see withGoFlags) with every `-trimpath` taken out and
// `-trimpath=false` put last. Analysis needs real source paths: under -trimpath go keys a cgo package's
// translation without its directory, so a load is served the translation cached for the same bytes in another
// directory, whose `//line` paths name that directory's files, gone once it is, and cgocall, which reads the raw
// file those paths name, fails. The value is never empty, since an empty `GOFLAGS` would hand go the go env file's
// flags, -trimpath among them.
func translatedEnvironment(workspace string, environment []string) ([]string, error) {
	return withGoFlags(workspace, environment, func(flags []string) []string {
		kept := []string{}
		for _, flag := range flags {
			name, _, _ := strings.Cut(strings.TrimLeft(flag, "-"), "=")
			if name != "trimpath" {
				kept = append(kept, flag)
			}
		}
		return append(kept, "-trimpath=false")
	})
}

// withGoFlags is the current environment, then environment, then `GOFLAGS` as go reads it in workspace (from the
// environment, or the go env file when that is empty), its flags rewritten by rewrite.
func withGoFlags(workspace string, environment []string, rewrite func([]string) []string) ([]string, error) {
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
	result := []string{}
	for _, variable := range given {
		if !strings.HasPrefix(variable, "GOFLAGS=") {
			result = append(result, variable)
		}
	}
	return append(result, "GOFLAGS="+strings.Join(rewrite(strings.Fields(string(output))), " ")), nil
}

// exists reports whether file can be read about.
func exists(file string) bool {
	_, statError := os.Stat(file)
	return statError == nil
}

// inside reports whether path is in workspace or below it.
func inside(workspace, path string) bool {
	return strings.HasPrefix(path, workspace+string(filepath.Separator))
}
