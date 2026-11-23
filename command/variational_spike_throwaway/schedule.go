package main

import (
	"go/ast"
	"go/token"
	"sort"
	"sync"
	"time"

	"github.com/system-inc/cohere/internal/variational_types/merged"
	"github.com/system-inc/go-types/types"
)

// The importer on the engine's schedule (#hmepe1g). The engine checks our packages in parallel along the import graph,
// each one starting once every one of ours it imports is done. The fork's checker still runs one package at a time, so
// the schedule is replayed with the importer alone: each of ours, on a pool of workers in the engine's order, looks up
// exactly the imported objects its check reads, through a fresh importer, which reads and merges them as the check's
// lookups would. That measures the importer's wall on a parallel schedule, its contention included. Then each package's
// warm check time and its measured lookup time are laid on the import graph, and its longest path is the critical
// path that model gives: a model, since the checks themselves didn't run in parallel.

// importRead is one imported object a check reads: a package-level object of another package, or a member (a field
// or method) of one of its named types.
type importRead struct {
	path   string
	name   string
	member string
}

// recordReads is, for each of ordered, the imported objects its host build's plain check reads, found from the
// check's Uses and Selections. It runs untimed, through hostImporter, ours from its own checks.
func recordReads(
	fileSet *token.FileSet,
	ordered []*ourPackage,
	ours map[string]*ourPackage,
	host *listedBuild,
	hostImporter types.Importer,
	sizes types.Sizes,
) map[string][]importRead {
	reads := map[string][]importRead{}
	checked := map[string]*types.Package{}
	for _, our := range ordered {
		files := our.filesIn(0)
		listedPackage := host.packages[our.path]
		if len(files) == 0 || listedPackage == nil {
			continue
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
		configuration := &types.Config{
			Importer:  resolvingImporter{importer: hostImporter, importMap: listedPackage.ImportMap, checked: checked},
			Sizes:     sizes,
			GoVersion: goVersionOf(our),
			Error:     func(error) {},
		}
		// Ignored: the timed plain check reports this package's errors; this pass only gathers what it reads.
		checkedTypes, _ := configuration.Check(our.path, fileSet, files, info)
		if !host.oursFromExport[our.path] {
			checked[our.path] = checkedTypes
		}
		seen := map[importRead]bool{}
		note := func(read importRead) {
			if _, isOurs := ours[read.path]; isOurs || seen[read] {
				return
			}
			seen[read] = true
			reads[our.path] = append(reads[our.path], read)
		}
		for _, object := range info.Uses {
			if object.Pkg() != nil && object.Parent() == object.Pkg().Scope() {
				note(importRead{path: object.Pkg().Path(), name: object.Name()})
			}
		}
		for _, selection := range info.Selections {
			named := namedOf(selection.Recv())
			if named == nil || named.Obj().Pkg() == nil {
				continue
			}
			note(importRead{path: named.Obj().Pkg().Path(), name: named.Obj().Name(), member: selection.Obj().Name()})
		}
		sort.Slice(reads[our.path], func(left int, right int) bool {
			a, b := reads[our.path][left], reads[our.path][right]
			if a.path != b.path {
				return a.path < b.path
			}
			if a.name != b.name {
				return a.name < b.name
			}
			return a.member < b.member
		})
	}
	return reads
}

// namedOf is the named type a selection's receiver is, through one pointer, or nil.
func namedOf(receiver types.Type) *types.Named {
	if pointer, isPointer := types.Unalias(receiver).(*types.Pointer); isPointer {
		receiver = pointer.Elem()
	}
	named, _ := types.Unalias(receiver).(*types.Named)
	if named == nil {
		return nil
	}
	return named.Origin()
}

// replayReads looks up each of reads through importer, as a check would: the package, then the object in its scope,
// then the member of a named type by LookupFieldOrMethod. It returns how many reads found nothing, which a read of
// another build's own object counts.
func replayReads(importer types.Importer, reads []importRead) int {
	missed := 0
	for _, read := range reads {
		imported, importError := importer.Import(read.path)
		if importError != nil || imported == nil {
			missed++
			continue
		}
		object := imported.Scope().Lookup(read.name)
		if object == nil {
			missed++
			continue
		}
		if read.member == "" {
			_ = object.Type()
			continue
		}
		found, _, _ := types.LookupFieldOrMethod(object.Type(), true, imported, read.member)
		if found == nil {
			missed++
			continue
		}
		_ = found.Type()
	}
	return missed
}

// scheduleRun is one replay of the schedule: its wall, each package's own job time, and the reads that found nothing.
type scheduleRun struct {
	WallSeconds float64            `json:"wallSeconds"`
	JobSeconds  map[string]float64 `json:"-"`
	Missed      int                `json:"missed"`
}

// runSchedule runs job for each of ordered on workers goroutines, each starting once every one of ours it imports is
// done, as the engine schedules its checks, and measures the whole and each job.
func runSchedule(ordered []*ourPackage, ours map[string]*ourPackage, workers int, job func(*ourPackage) int) scheduleRun {
	waiting := map[string]int{}
	dependents := map[string][]string{}
	for _, our := range ordered {
		for _, imported := range our.imports {
			if _, isOurs := ours[imported]; isOurs && imported != our.path {
				waiting[our.path]++
				dependents[imported] = append(dependents[imported], our.path)
			}
		}
	}
	run := scheduleRun{JobSeconds: map[string]float64{}}
	var lock sync.Mutex
	ready := make(chan string, len(ordered))
	for _, our := range ordered {
		if waiting[our.path] == 0 {
			ready <- our.path
		}
	}
	var group sync.WaitGroup
	remaining := len(ordered)
	done := make(chan struct{})
	started := time.Now()
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for {
				select {
				case path := <-ready:
					jobStarted := time.Now()
					missed := job(ours[path])
					jobSeconds := time.Since(jobStarted).Seconds()
					lock.Lock()
					run.JobSeconds[path] = jobSeconds
					run.Missed += missed
					remaining--
					for _, dependent := range dependents[path] {
						waiting[dependent]--
						if waiting[dependent] == 0 {
							ready <- dependent
						}
					}
					if remaining == 0 {
						close(done)
					}
					lock.Unlock()
				case <-done:
					return
				}
			}
		}()
	}
	if len(ordered) == 0 {
		close(done)
	}
	group.Wait()
	run.WallSeconds = time.Since(started).Seconds()
	return run
}

// criticalPath is the longest path through ours' import graph, each package weighing weight's seconds: the wall a
// schedule with enough workers needs, by the model.
func criticalPath(ordered []*ourPackage, ours map[string]*ourPackage, weight map[string]float64) float64 {
	finish := map[string]float64{}
	longest := 0.0
	for _, our := range ordered {
		start := 0.0
		for _, imported := range our.imports {
			if _, isOurs := ours[imported]; isOurs && imported != our.path {
				start = max(start, finish[imported])
			}
		}
		finish[our.path] = start + weight[our.path]
		longest = max(longest, finish[our.path])
	}
	return longest
}

// scheduleReport is the importer on the engine's schedule, and the critical path the model gives.
type scheduleReport struct {
	Workers int `json:"workers"`
	// Serialized is whether the variational importer's lookups were held to one at a time, as an importer that isn't
	// safe under parallel lookups must be.
	Serialized         bool        `json:"serialized"`
	Reads              int         `json:"reads"`
	VariationalImport  scheduleRun `json:"variationalImport"`
	HostPlainImport    scheduleRun `json:"hostPlainImport"`
	EveryBuildImport   scheduleRun `json:"everyBuildImport"`
	VariationalPath    float64     `json:"variationalCriticalPathSeconds"`
	HostPlainPath      float64     `json:"hostPlainCriticalPathSeconds"`
	VariationalCheckOn float64     `json:"variationalChecksAloneCriticalPathSeconds"`
	HostPlainCheckOn   float64     `json:"hostPlainChecksAloneCriticalPathSeconds"`
}

// scheduleImporter replays the schedule for the variational importer, the host's plain one and every build's plain
// ones, each fresh from newImporter, and lays the measured times on the import graph with each package's warm check.
func scheduleImporter(
	ordered []*ourPackage,
	ours map[string]*ourPackage,
	reads map[string][]importRead,
	builds int,
	newImporter func() *merged.Importer,
	variationalChecks map[string]float64,
	hostChecks map[string]float64,
	workers int,
	serialized bool,
) *scheduleReport {
	report := &scheduleReport{Workers: workers, Serialized: serialized}
	for _, each := range reads {
		report.Reads += len(each)
	}
	var serial sync.Mutex
	variational := newImporter()
	report.VariationalImport = runSchedule(ordered, ours, workers, func(our *ourPackage) int {
		if serialized {
			serial.Lock()
			defer serial.Unlock()
		}
		return replayReads(variational, reads[our.path])
	})
	host := newImporter()
	hostPlain := host.Plain(0)
	report.HostPlainImport = runSchedule(ordered, ours, workers, func(our *ourPackage) int {
		if serialized {
			serial.Lock()
			defer serial.Unlock()
		}
		return replayReads(hostPlain, reads[our.path])
	})
	// Today each build reads its own, the builds side by side: one schedule a build, all of them at once, each
	// replaying the host's reads through its own build's export data. Its wall is the slowest build's, its job time a
	// package's slowest build's.
	every := newImporter()
	runs := make([]scheduleRun, builds)
	var builders sync.WaitGroup
	for build := range builds {
		plain := every.Plain(build)
		builders.Add(1)
		go func() {
			defer builders.Done()
			runs[build] = runSchedule(ordered, ours, workers, func(our *ourPackage) int {
				if serialized {
					serial.Lock()
					defer serial.Unlock()
				}
				return replayReads(plain, reads[our.path])
			})
		}()
	}
	builders.Wait()
	report.EveryBuildImport = scheduleRun{JobSeconds: map[string]float64{}}
	for _, run := range runs {
		report.EveryBuildImport.WallSeconds = max(report.EveryBuildImport.WallSeconds, run.WallSeconds)
		report.EveryBuildImport.Missed += run.Missed
		for path, seconds := range run.JobSeconds {
			report.EveryBuildImport.JobSeconds[path] = max(report.EveryBuildImport.JobSeconds[path], seconds)
		}
	}
	weigh := func(checks map[string]float64, imports map[string]float64) map[string]float64 {
		weight := map[string]float64{}
		for _, our := range ordered {
			weight[our.path] = checks[our.path] + imports[our.path]
		}
		return weight
	}
	report.VariationalPath = criticalPath(ordered, ours, weigh(variationalChecks, report.VariationalImport.JobSeconds))
	report.HostPlainPath = criticalPath(ordered, ours, weigh(hostChecks, report.HostPlainImport.JobSeconds))
	report.VariationalCheckOn = criticalPath(ordered, ours, weigh(variationalChecks, nil))
	report.HostPlainCheckOn = criticalPath(ordered, ours, weigh(hostChecks, nil))
	return report
}
