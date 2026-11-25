package go_language

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/cgo_testing"
	"github.com/system-inc/cohere/internal/go_language/check"
)

// Whether a cgo package's imports carry what its translation adds, `unsafe`, `syscall` and `runtime/cgo`, decides
// whether the reach's Imports comparison sees a package cgo is on for in one build alone (#bed0mnk, the Square's
// should 4 on the three-piece head): the loader drops "C" (load's resolvedImports) and adds the translation's three
// where go list says the package depends on them (load's cgoImports), and unusedwrite stands down in a package whose
// types import `unsafe`. In body-reach-cgo, gate's part_cgo.go imports "C" and part_nocgo.go, `!cgo`,
// declares the same name, beside a shared Target that unusedwrite reports unless gate imports `unsafe`. The host
// translates cgo and windows, cross-compiled, does not.
//
// What holds: on the host, gate's imports by path are the translation's three, as its types' imports are, and in
// windows's build, read by go/packages under the build's own environment, both are empty, so the imports differ by
// the translation's imports alone and Target's finding flips, the host silent and windows reporting it. The reach
// never asks: the host records no reads of a package with cgo files, which cgocall type-checks again, so the census
// calls gate Unrecorded and reaches its shared body whole before any reason of its own is read, and the differential
// compares none of gate. Were a cgo package's uses recorded, it would be reached by Imports, through the translation's
// imports and nothing of its own.
func TestACgoPackagesImportsCarryTheTranslationsAndTheReachTakesItUnrecorded(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)

	const gate = "example.test/gate"
	translated := []string{"runtime/cgo", "syscall", "unsafe"}
	root := copyFixture(test, filepath.Join("testdata", "body-reach-cgo"))
	on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
		engine.bodyDifferential = true
	}, "--no-cache", "--no-fix", "--build-dependence")
	if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
	}

	hosted := on.bodyReachHost.packages[gate]
	if hosted == nil || hosted.Types == nil {
		test.Fatal("the host holds no checked gate")
	}
	if got := importedPaths(hosted); !slices.Equal(got, translated) {
		test.Errorf("the host's gate imports %v by path, want the translation's %v", got, translated)
	}
	if got := typedImports(hosted); !slices.Equal(got, translated) {
		test.Errorf("the host's gate's types import %v, want the translation's %v", got, translated)
	}
	if reads := on.bodyReachHost.reads[hosted]; reads == nil || !strings.Contains(reads.Never(), "cgo") {
		test.Error("want the host's reads of gate never recorded, for its cgo files")
	}

	// The build's own load is not kept past its run, so gate is read as it would load there, under the process's
	// environment beneath the run's and the build's, a later variable taking the place of an earlier one.
	environment := append(append(os.Environ(), on.described.workspace.Environment()...),
		on.buildRuns[1].build.Environment()...)
	roots, loadError := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedImports |
		packages.NeedTypes, Dir: root, Env: environment}, gate)
	if loadError != nil {
		test.Fatal(loadError)
	}
	if len(roots) != 1 || roots[0].Types == nil {
		test.Fatalf("want windows's gate loaded with its types, got %d packages", len(roots))
	}
	if got, typed := importedPaths(roots[0]), typedImports(roots[0]); len(got) > 0 || len(typed) > 0 {
		test.Errorf("windows's gate imports %v by path and its types %v, want nothing", got, typed)
	}

	census, differential := on.buildRuns[1].bodyReach, on.buildRuns[1].bodyDifferential
	if census == nil || differential == nil {
		test.Fatal("windows's run has no census or no body differential")
	}
	// gate's one body in a file both builds compile is Target, and quiet's is unreached.
	if census.byReason[reachedUnrecorded] != 1 || census.byReason[reachedImports] != 0 {
		test.Errorf("windows's census reaches %v, want gate's shared body Unrecorded and nothing by Imports",
			census.byReason)
	}
	if reason := judgedBody(census, gate, "Target"); reason != "unjudged" {
		test.Errorf("windows's census judges Target %s, want it unjudged, its package unrecorded", reason)
	}
	if len(differential.failures) > 0 {
		test.Errorf("want the body differential to read no failure, got\n%s",
			strings.Join(differential.failures, "\n"))
	}
	// The case has a finding to lose: Target's flips with the translation's `unsafe`.
	reports := func(run buildRun) bool {
		return slices.ContainsFunc(run.result.Findings, func(finding check.Finding) bool {
			return finding.Rule == "cohere-go/unusedwrite" && filepath.Base(finding.File) == "shared.go"
		})
	}
	if reports(on.buildRuns[0]) || !reports(on.buildRuns[1]) {
		test.Errorf("want unusedwrite on Target in windows's build alone, got the host's %t and windows's %t",
			reports(on.buildRuns[0]), reports(on.buildRuns[1]))
	}
}

// importedPaths are loaded's imports by path, sorted.
func importedPaths(loaded *packages.Package) []string {
	paths := make([]string, 0, len(loaded.Imports))
	for path := range loaded.Imports {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

// typedImports are the paths of the packages loaded's types import, sorted.
func typedImports(loaded *packages.Package) []string {
	paths := make([]string, 0, len(loaded.Types.Imports()))
	for _, imported := range loaded.Types.Imports() {
		paths = append(paths, imported.Path())
	}
	slices.Sort(paths)
	return paths
}
