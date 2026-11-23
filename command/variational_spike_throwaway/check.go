package main

import (
	"go/ast"
	"go/token"

	"github.com/system-inc/cohere/internal/variational_types/merged"
	"github.com/system-inc/cohere/internal/variational_types/types"
)

// checkedPackage is one of ours after the variational check: its answers by build and the check's counts.
type checkedPackage struct {
	our       *ourPackage
	variation *types.Variation
}

// checkCounts is what one run of checks over ours reported: how long its imports and its checks took, and its errors.
type checkCounts struct {
	imports     phaseTiming
	checks      phaseTiming
	errors      int
	firstErrors []string
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
// ours, so the imports are timed apart and no import runs inside a variational check, whose state is the fork's
// package state. Each checked package is given to importer for the packages of ours that import it.
func checkVariationally(
	fileSet *token.FileSet,
	ordered []*ourPackage,
	ours map[string]*ourPackage,
	importer *merged.Importer,
	all types.Mask,
	sizes types.Sizes,
) ([]*checkedPackage, checkCounts) {
	counts := checkCounts{}
	checked := make([]*checkedPackage, 0, len(ordered))
	for _, our := range ordered {
		counts.imports = counts.imports.add(measure(func() {
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
		counts.checks = counts.checks.add(measure(func() {
			// Ignored: every error went to Error above. The Info holds no Types map: Variation.Types is the answer, and a
			// plain map beside it would record every expression twice.
			checkedTypes, _ = configuration.Check(our.path, fileSet, our.files, &types.Info{})
		}))
		importer.Add(checkedTypes)
		checked = append(checked, &checkedPackage{our: our, variation: variation})
	}
	return checked, counts
}

// checkPlainly checks each of ordered that build compiles the plain way, with only build's files and build's own
// export data, unmerged: the baseline the variational check is priced against. Its answers go to a Types map, as a
// plain check's must to be recorded at all.
func checkPlainly(
	fileSet *token.FileSet,
	ordered []*ourPackage,
	listed *listedBuild,
	importer types.Importer,
	build int,
	sizes types.Sizes,
) checkCounts {
	counts := checkCounts{}
	for _, our := range ordered {
		files := our.filesIn(build)
		listedPackage := listed.packages[our.path]
		if len(files) == 0 || listedPackage == nil {
			continue
		}
		counts.imports = counts.imports.add(measure(func() {
			for _, imported := range listedPackage.Imports {
				if imported == "unsafe" {
					continue
				}
				if _, importError := importer.Import(imported); importError != nil {
					counts.noteError(our.path + ": importing " + imported + ": " + importError.Error())
				}
			}
		}))
		configuration := &types.Config{
			Importer:  resolvingImporter{importer: importer, importMap: listedPackage.ImportMap},
			Sizes:     sizes,
			GoVersion: goVersionOf(our),
			Error:     func(reported error) { counts.noteError(reported.Error()) },
		}
		counts.checks = counts.checks.add(measure(func() {
			// Ignored: every error went to Error above.
			_, _ = configuration.Check(our.path, fileSet, files, &types.Info{Types: map[ast.Expr]types.TypeAndValue{}})
		}))
	}
	return counts
}
