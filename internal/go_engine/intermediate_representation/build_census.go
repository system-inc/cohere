package intermediate_representation

import (
	"go/types"
	"slices"
	"sync"
	"sync/atomic"
)

// BuildCensus counts, while it is open, the programs each package's source functions are built in, so a test can
// hold a run to building each package once (#wt5vaft): a root built for its readers and again in the fact resolver's
// own program counts two. A function counts where its body builds from syntax, so a types-only stub of the package,
// in the program of one of its importers, never counts.
//
// It is a test's: an open census sees every build in the process, its own test's and any running beside it, so a
// test reads only its own packages, each a load of its own and so a *types.Package no other test holds.
type BuildCensus struct {
	lock     sync.Mutex
	programs map[*types.Package]map[*Program]bool
}

// openCensuses is every census open now, under openCensusesLock, and openCensusCount how many, which a build reads
// alone, without the lock, while none is open.
var (
	openCensusesLock sync.Mutex
	openCensuses     []*BuildCensus
	openCensusCount  atomic.Int32
)

// OpenBuildCensus is a new census, counting every build from now until Close.
func OpenBuildCensus() *BuildCensus {
	census := &BuildCensus{programs: map[*types.Package]map[*Program]bool{}}
	openCensusesLock.Lock()
	defer openCensusesLock.Unlock()
	openCensuses = append(openCensuses, census)
	openCensusCount.Add(1)
	return census
}

// Close stops census counting. What it counted stays readable.
func (census *BuildCensus) Close() {
	openCensusesLock.Lock()
	defer openCensusesLock.Unlock()
	if index := slices.Index(openCensuses, census); index >= 0 {
		openCensuses = slices.Delete(openCensuses, index, index+1)
		openCensusCount.Add(-1)
	}
}

// Programs is how many programs census saw a source function of the package whose types are packageTypes built in.
func (census *BuildCensus) Programs(packageTypes *types.Package) int {
	census.lock.Lock()
	defer census.lock.Unlock()
	return len(census.programs[packageTypes])
}

// Packages is how many programs census saw each package's source functions built in, by the package's types.
func (census *BuildCensus) Packages() map[*types.Package]int {
	census.lock.Lock()
	defer census.lock.Unlock()
	counted := make(map[*types.Package]int, len(census.programs))
	for packageTypes, programs := range census.programs {
		counted[packageTypes] = len(programs)
	}
	return counted
}

// countBuild adds function's program to every open census, for a source function of a package, built from syntax.
// While no census is open it costs one atomic load.
func countBuild(function *Function) {
	if openCensusCount.Load() == 0 || function.Package == nil || function.syntax == nil {
		return
	}
	openCensusesLock.Lock()
	defer openCensusesLock.Unlock()
	for _, census := range openCensuses {
		census.lock.Lock()
		if census.programs[function.Package.Types] == nil {
			census.programs[function.Package.Types] = map[*Program]bool{}
		}
		census.programs[function.Package.Types][function.Program] = true
		census.lock.Unlock()
	}
}
