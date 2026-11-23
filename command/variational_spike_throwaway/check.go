package main

import (
	"go/ast"
	"go/token"
	"maps"
	"sync"

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
func (counts *checkCounts) noteError(reported string) {
	counts.errors++
	if len(counts.firstErrors) < firstErrorsKept {
		counts.firstErrors = append(counts.firstErrors, reported)
	}
}

// absorb sums one package's counts into counts. A job of a parallel schedule counts into its own checkCounts, which
// no other job writes, and its schedule absorbs them under a lock as the job ends.
func (counts *checkCounts) absorb(job checkCounts) {
	counts.imports = counts.imports.add(job.imports)
	counts.checks = counts.checks.add(job.checks)
	maps.Copy(counts.checkSeconds, job.checkSeconds)
	counts.errors += job.errors
	for _, reported := range job.firstErrors {
		if len(counts.firstErrors) < firstErrorsKept {
			counts.firstErrors = append(counts.firstErrors, reported)
		}
	}
}

// parallelRun is one check of ours on the engine's schedule: its wall, which is the real critical path its workers
// gave, the process's CPU across the whole of it, and each package's job seconds (its imports and its check) and check
// seconds, each by wall alone.
type parallelRun struct {
	WallSeconds  float64            `json:"wallSeconds"`
	Process      phaseTiming        `json:"process"`
	JobSeconds   map[string]float64 `json:"jobSeconds"`
	CheckSeconds map[string]float64 `json:"checkSeconds"`
}

// onSchedule runs job for each of ordered on runSchedule's workers, as the engine schedules its checks, and measures
// the whole: the schedule's wall and each job's from runSchedule, the process's CPU across it from measure. Each job
// counts into a checkCounts of its own, absorbed into the one returned as the job ends.
func onSchedule(
	ordered []*ourPackage,
	ours map[string]*ourPackage,
	workers int,
	job func(*ourPackage) checkCounts,
) (checkCounts, parallelRun) {
	counts := checkCounts{checkSeconds: map[string]float64{}}
	var countsLock sync.Mutex
	var run scheduleRun
	process := measure(func() {
		run = runSchedule(ordered, ours, workers, func(our *ourPackage) int {
			jobCounts := job(our)
			countsLock.Lock()
			defer countsLock.Unlock()
			counts.absorb(jobCounts)
			// No lookup is replayed, so none is missed: the check's errors are in counts.
			return 0
		})
	})
	return counts, parallelRun{
		WallSeconds:  run.WallSeconds,
		Process:      process,
		JobSeconds:   run.JobSeconds,
		CheckSeconds: counts.checkSeconds,
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
		each, eachCounts := checkOneVariationally(fileSet, our, ours, importer, all, sizes, timed)
		counts.absorb(eachCounts)
		checked = append(checked, each)
	}
	return checked, counts
}

// checkVariationallyInParallel checks ordered as checkVariationally does, but on workers goroutines along ours' import
// graph, one types.Checker a package, as the engine schedules its checks: each of ours starts once every one of ours
// it imports is checked and added to importer, which is safe for many goroutines at once, and imports what it imports
// that isn't ours inside its own job. Each job's phases are timed by wall alone, since the process's CPU over one job
// is every running job's; the run's wall and the process's CPU across it are its price. checked is in ordered's order,
// whatever order the jobs ran in, so the proof reads it as it reads the serial check's.
func checkVariationallyInParallel(
	fileSet *token.FileSet,
	ordered []*ourPackage,
	ours map[string]*ourPackage,
	importer *merged.Importer,
	all types.Mask,
	sizes types.Sizes,
	workers int,
) ([]*checkedPackage, checkCounts, parallelRun) {
	positions := make(map[string]int, len(ordered))
	for index, our := range ordered {
		positions[our.path] = index
	}
	// Each job writes its own element, and runSchedule's wait is over before checked is read.
	checked := make([]*checkedPackage, len(ordered))
	counts, run := onSchedule(ordered, ours, workers, func(our *ourPackage) checkCounts {
		each, eachCounts := checkOneVariationally(fileSet, our, ours, importer, all, sizes, wallOnly)
		checked[positions[our.path]] = each
		return eachCounts
	})
	return checked, counts, run
}

// checkOneVariationally is our checked once for every build in all, after importing what it imports that isn't ours,
// and given to importer for the packages of ours that import it, with its own counts.
func checkOneVariationally(
	fileSet *token.FileSet,
	our *ourPackage,
	ours map[string]*ourPackage,
	importer *merged.Importer,
	all types.Mask,
	sizes types.Sizes,
	timed func(func()) phaseTiming,
) (*checkedPackage, checkCounts) {
	counts := checkCounts{checkSeconds: map[string]float64{}}
	counts.imports = timed(func() {
		for _, imported := range our.imports {
			if _, isOurs := ours[imported]; isOurs || imported == "unsafe" {
				continue
			}
			if _, importError := importer.Import(imported); importError != nil {
				counts.noteError(our.path + ": importing " + imported + ": " + importError.Error())
			}
		}
	})
	variation := &types.Variation{Builds: all, Files: our.masks}
	configuration := &types.Config{
		Variation: variation,
		Importer:  resolvingImporter{importer: importer, importMap: our.importMap},
		Sizes:     sizes,
		GoVersion: goVersionOf(our),
		Error:     func(reported error) { counts.noteError(reported.Error()) },
	}
	var checkedTypes *types.Package
	counts.checks = timed(func() {
		// Ignored: every error went to Error above. The Info holds no Types map: Variation.Types is the answer, and a
		// plain map beside it would record every expression twice.
		checkedTypes, _ = configuration.Check(our.path, fileSet, our.files, &types.Info{})
	})
	counts.checkSeconds[our.path] = counts.checks.WallSeconds
	importer.Add(checkedTypes)
	return &checkedPackage{our: our, variation: variation}, counts
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
	checked := newOursChecked()
	for _, our := range ordered {
		counts.absorb(checkOnePlainly(fileSet, our, listed, importer, checked, build, sizes, timed))
	}
	return counts
}

// checkPlainlyInParallel checks ordered as checkPlainly does, on workers goroutines along ours' import graph as
// checkVariationallyInParallel schedules them, each job's phases timed by wall alone. importer must be safe under
// checks that run at once, which merged.Importer.PlainWhole is and Plain isn't.
func checkPlainlyInParallel(
	fileSet *token.FileSet,
	ordered []*ourPackage,
	ours map[string]*ourPackage,
	listed *listedBuild,
	importer types.Importer,
	build int,
	sizes types.Sizes,
	workers int,
) (checkCounts, parallelRun) {
	checked := newOursChecked()
	return onSchedule(ordered, ours, workers, func(our *ourPackage) checkCounts {
		return checkOnePlainly(fileSet, our, listed, importer, checked, build, sizes, wallOnly)
	})
}

// checkOnePlainly is our checked as build compiles it, with its own counts, when build compiles it: its imports
// first, then the check, and the checked package given to checked for the packages of ours that import it, unless a
// package not ours imports it, which reads it from export data.
func checkOnePlainly(
	fileSet *token.FileSet,
	our *ourPackage,
	listed *listedBuild,
	importer types.Importer,
	checked *oursChecked,
	build int,
	sizes types.Sizes,
	timed func(func()) phaseTiming,
) checkCounts {
	counts := checkCounts{checkSeconds: map[string]float64{}}
	files := our.filesIn(build)
	listedPackage := listed.packages[our.path]
	if len(files) == 0 || listedPackage == nil {
		return counts
	}
	resolving := resolvingImporter{importer: importer, importMap: listedPackage.ImportMap, checked: checked}
	counts.imports = timed(func() {
		for _, imported := range listedPackage.Imports {
			if _, importError := resolving.Import(imported); importError != nil {
				counts.noteError(our.path + ": importing " + imported + ": " + importError.Error())
			}
		}
	})
	configuration := &types.Config{
		Importer:  resolving,
		Sizes:     sizes,
		GoVersion: goVersionOf(our),
		Error:     func(reported error) { counts.noteError(reported.Error()) },
	}
	var checkedTypes *types.Package
	counts.checks = timed(func() {
		// Ignored: every error went to Error above.
		checkedTypes, _ = configuration.Check(our.path, fileSet, files,
			&types.Info{Types: map[ast.Expr]types.TypeAndValue{}})
	})
	counts.checkSeconds[our.path] = counts.checks.WallSeconds
	if !listed.oursFromExport[our.path] {
		checked.put(our.path, checkedTypes)
	}
	return counts
}
