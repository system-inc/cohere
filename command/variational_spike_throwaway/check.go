package main

import (
	"go/ast"
	"go/token"

	"github.com/system-inc/cohere/internal/variational_types/merged"
	"github.com/system-inc/go-types/types"
)

// checkedPackage is one of ours after the variational check: its answers by build and the check's counts.
type checkedPackage struct {
	our       *ourPackage
	variation *types.Variation
}

// checkCounts is what one run of checks over ours reported: how long its imports and its checks took, and its errors.
type checkCounts struct {
	imports phaseTiming
	checks  phaseTiming
	// checkSeconds is each package's check wall, for the schedule's model.
	checkSeconds map[string]float64
	errors       int
	firstErrors  []string
}

// noteError counts reported, spelling the first few.
//
// Mutates: counts gets the error.
func (counts *checkCounts) noteError(reported string) {
	counts.errors++
	if len(counts.firstErrors) < firstErrorsKept {
		counts.firstErrors = append(counts.firstErrors, reported)
	}
}

// goVersionOf is the go version our's module asks for, as types.Config spells it, or empty for none.
func goVersionOf(our *ourPackage) string {
	if our.goVersion == "" {
		return ""
	}
	return "go" + our.goVersion
}

// checkVariationally checks each of ordered once for every build in all, after importing what it imports that isn't
// ours. The importer is lazy, so an import only lays out a package's names: each object is read and merged the first
// time the check looks it up, inside the check's time and outside every unit (types.Outside), so the import row holds
// less than all of the importing. timed measures each package's imports and check, or only runs them. Each checked
// package is given to importer for the packages of ours that import it, and the imports stay interleaved with the
// checks: a package not ours can import one of ours, and imported before that one is added it would read ours' export
// data, a second identity for every type of it.
func checkVariationally(
	fileSet *token.FileSet,
	ordered []*ourPackage,
	ours map[string]*ourPackage,
	importer *merged.Importer,
	all types.Mask,
	sizes types.Sizes,
	timed func(func()) phaseTiming,
) ([]*checkedPackage, checkCounts) {
	counts := checkCounts{checkSeconds: map[string]float64{}}
	checked := make([]*checkedPackage, 0, len(ordered))
	for _, our := range ordered {
		counts.imports = counts.imports.add(timed(func() {
			for _, imported := range our.imports {
				if _, isOurs := ours[imported]; isOurs || imported == "unsafe" {
					continue
				}
				if _, importError := importer.Import(imported); importError != nil {
					counts.noteError(our.path + ": importing " + imported + ": " + importError.Error())
				}
			}
		}))
		variation := &types.Variation{Builds: all, Files: our.masks}
		configuration := &types.Config{
			Variation: variation,
			Importer:  resolvingImporter{importer: importer, importMap: our.importMap},
			Sizes:     sizes,
			GoVersion: goVersionOf(our),
			Error:     func(reported error) { counts.noteError(reported.Error()) },
		}
		var checkedTypes *types.Package
		checkTiming := timed(func() {
			// Ignored: every error went to Error above. The Info holds no Types map: Variation.Types is the answer, and a
			// plain map beside it would record every expression twice.
			checkedTypes, _ = configuration.Check(our.path, fileSet, our.files, &types.Info{})
		})
		counts.checks = counts.checks.add(checkTiming)
		counts.checkSeconds[our.path] = checkTiming.WallSeconds
		importer.Add(checkedTypes)
		checked = append(checked, &checkedPackage{our: our, variation: variation})
	}
	return checked, counts
}

// checkPlainly checks each of ordered that build compiles the plain way, with only build's files: the baseline the
// variational check is priced against, each phase of each package run through timed. Ours come from this run's own
// checks, in dependency order, but those a package not ours imports, which come with everything else from build's own
// export data, unmerged, as that package's export data reads them. Its answers go to a Types map, as a plain check's
// must to be recorded at all.
func checkPlainly(
	fileSet *token.FileSet,
	ordered []*ourPackage,
	listed *listedBuild,
	importer types.Importer,
	build int,
	sizes types.Sizes,
	timed func(func()) phaseTiming,
) checkCounts {
	counts := checkCounts{checkSeconds: map[string]float64{}}
	checked := map[string]*types.Package{}
	for _, our := range ordered {
		files := our.filesIn(build)
		listedPackage := listed.packages[our.path]
		if len(files) == 0 || listedPackage == nil {
			continue
		}
		resolving := resolvingImporter{importer: importer, importMap: listedPackage.ImportMap, checked: checked}
		counts.imports = counts.imports.add(timed(func() {
			for _, imported := range listedPackage.Imports {
				if _, importError := resolving.Import(imported); importError != nil {
					counts.noteError(our.path + ": importing " + imported + ": " + importError.Error())
				}
			}
		}))
		configuration := &types.Config{
			Importer:  resolving,
			Sizes:     sizes,
			GoVersion: goVersionOf(our),
			Error:     func(reported error) { counts.noteError(reported.Error()) },
		}
		var checkedTypes *types.Package
		checkTiming := timed(func() {
			// Ignored: every error went to Error above.
			checkedTypes, _ = configuration.Check(our.path, fileSet, files,
				&types.Info{Types: map[ast.Expr]types.TypeAndValue{}})
		})
		counts.checks = counts.checks.add(checkTiming)
		counts.checkSeconds[our.path] = checkTiming.WallSeconds
		if !listed.oursFromExport[our.path] {
			checked[our.path] = checkedTypes
		}
	}
	return counts
}
