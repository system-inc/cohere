package check

import (
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// HostRun is what the host's run keeps for the extra builds to restore packages from: each package's entry by ID, the
// renderings the verdicts make of the host's types, shared by every build, and the digests of the files its keys
// hashed, which an extra build's keys read rather than hash again. known is what the host's session knows of its
// analyzers, which each entry's shared decode reads (hostEntry.decoded), and which a build's own must decode alike for
// the build to share it (decodesAlike). bodies is the run's memo of the bodies decoded from the store, by digest,
// which the host's session filled with its hits and each build adds the bodies it read from disk to (decodedBodies).
type HostRun struct {
	entries    map[string]*hostEntry
	renderings *ReadsBuild
	files      cache.FileDigests
	known      *cacheAnalyzers
	bodies     *decodedBodies

	// portables are the portable digests of every package the host keyed, by ID, those it kept and those it restored
	// alike, so a build tells a package it holds itself, whose inputs are the host's, from one whose inputs differ,
	// though the host kept no entry of it (restoreFromHost).
	portables map[string]cache.Key

	// keeping is the time the run spent keeping its packages for the extra builds over every load (Keeping).
	keeping time.Duration

	// kept is how many entries the run kept, which outlives Release (EntriesKept).
	kept int

	// computing are the sets of the kept packages the host computes as each package's walk ends (computeDependence),
	// and joining, nil outside tests, is told as a reader starts waiting for them (DependenceComputed).
	computing sync.WaitGroup
	joining   func()

	// holders is the IDs of the kept packages that compiled each file, by the file's name, sorted, made once a run by
	// the first build to ask (holdersOf).
	holdersOnce sync.Once
	holders     map[string][]string
}

// EntriesKept counts the packages the host's run kept an entry of for the extra builds.
func (host *HostRun) EntriesKept() int {
	if host == nil {
		return 0
	}
	return host.kept
}

// DependenceComputed returns once the host has computed the dependence set of every package it kept, each as its walk
// ended (computeDependence), so a build started after it finds every set it asks in the memo, rather than compute one
// beside the pass, the duplicate work the pass exists to remove (#ybjbqm2). A nil host, which kept nothing, has none.
func (host *HostRun) DependenceComputed() {
	if host == nil {
		return
	}
	if host.joining != nil {
		host.joining()
	}
	host.computing.Wait()
}

// Release lets go of everything the host's run kept for the extra builds once the last of them has run, since no
// build can ask for an entry, or a verdict render a host type, after that: the entries, with the host's types their
// export data is written from, each with its decode the builds shared (hostEntry.decoded), the bodies decoded from the
// store (decodedBodies), and the renderings, keyed by the host's objects (#b2t9xmf). What the record counts of it
// stays.
func (host *HostRun) Release() {
	if host == nil {
		return
	}
	// A set still being computed writes to the memo, so the memo goes only once none is.
	host.computing.Wait()
	host.entries = nil
	host.renderings = nil
	host.known = nil
	// Emptied, not only let go: the host's session holds the same memo (keepForBuilds).
	host.bodies.release()
	host.bodies = nil
	host.holders = nil
}

// Keeping is the time the host's run spent keeping its packages for the extra builds, over every load: the keys a run
// without a store computes only to keep them, and the entries, encoded without their export data, which is written when
// a load first reads it (lazyExportData), within that build's load. A run that kept nothing spent none.
func (host *HostRun) Keeping() time.Duration {
	if host == nil {
		return 0
	}
	return host.keeping
}

// restoringOf is kept, a package of the host's run, as a verdict judges it: its path, its imports' paths, its name, its
// reads, and whether it has assembly files, as asmdecl finds them among its other files.
func restoringOf(kept *packages.Package, reads *build_reads.Reads) Restoring {
	assembly := slices.ContainsFunc(kept.OtherFiles, func(file string) bool { return strings.HasSuffix(file, ".s") })
	return Restoring{Path: kept.PkgPath, Imports: importPaths(kept), Name: kept.Name, Reads: reads, Assembly: assembly}
}

// importPaths are the paths of the packages importer imports, sorted.
func importPaths(importer *packages.Package) []string {
	paths := make([]string, 0, len(importer.Imports))
	for _, each := range importer.Imports {
		paths = append(paths, each.PkgPath)
	}
	slices.Sort(paths)
	return paths
}

// computeDependence has host's memo compute the dependence set of every package the host kept, each once its own
// type-read walk on walks has ended (build_reads.Walks.Ended), on a slot of pool at the priority below every load's and
// check's work, as the session's fanOut takes them, or of as many slots as there are cores when pool is nil, so each
// extra build's walk (restoreFromHost) finds every set it asks in the memo (#ybjbqm2). Asked there instead, each set
// was computed by the first of seven builds racing to ask it, the others waiting or computing it again before the memo
// stored one: each build's walk took 223 ms at cohere's root, and 4 to 5 ms with the sets computed by the host, in a
// pass of 111 ms on about three cores, the run's wall 0.11 s less there and 0.07 s at deeafbf4, its user and system
// time level, with the same findings.
//
// A set reads its own package's reads and the host's types alone, so it waits for that package's walk and no other:
// asked while the walk still runs, its reads are never recorded and its set the whole closure (Dependence). The keep
// runs inside check.Run, before the walks are waited for, and waiting there for all of them put the pass on the run's
// serial path, about 110 ms at cohere's root. So each set waits with no slot held, as the walk it waits for may need
// one, and the keep returns at once. The run joins the pass once the walks are over, before any build starts
// (DependenceComputed), which waits for the last sets alone, and Release waits for it too, so no set is computed into
// a memo let go of. kept are the entries, owners their packages, index by index, and asking, nil outside tests, is
// told of each as its set is asked.
func (host *HostRun) computeDependence(
	walks *build_reads.Walks,
	pool *bounded.Pool,
	owners []*packages.Package,
	kept []*hostEntry,
	asking func(owner *packages.Package, restoring Restoring),
) {
	// Read once here, so a set computed late never reads the field Release clears.
	renderings := host.renderings
	var cores chan struct{}
	if pool == nil {
		cores = make(chan struct{}, runtime.GOMAXPROCS(0))
	}
	for index, owner := range owners {
		ended := walks.Ended(owner)
		host.computing.Go(func() {
			<-ended
			if pool != nil {
				pool.Acquire(0)
				defer pool.Release()
			} else {
				cores <- struct{}{}
				defer func() { <-cores }()
			}
			if asking != nil {
				asking(owner, kept[index].restoring)
			}
			renderings.Dependence(kept[index].restoring)
		})
	}
}

// Inheritance is an extra build's take of host for the rules enabled, nil when host kept nothing, as under `--timing`.
// Whether a rule inherits is its own to ask: a listener marked SyntaxOnly asks through its dispatcher, and a
// syntax-only walk through rule.SyntaxOnlyRun, and no other rule asks at all.
func (host *HostRun) Inheritance(enabled []rule.Rule) *HostInheritance {
	if host == nil {
		return nil
	}
	analyzerOf := make(map[string]*analysis.Analyzer, len(enabled))
	for _, each := range enabled {
		analyzerOf[each.Name] = each.Analyzer
	}
	return &HostInheritance{host: host, analyzerOf: analyzerOf, taken: map[inheritedKey]int{}}
}

// holdersOf is the IDs of the host's kept packages that compiled filename, sorted, read from an index of every kept
// entry's files made once a run, the first time any build asks.
func (host *HostRun) holdersOf(filename string) []string {
	host.holdersOnce.Do(func() {
		host.holders = map[string][]string{}
		for identifier, kept := range host.entries {
			for _, compiled := range kept.compiledFiles {
				host.holders[compiled] = append(host.holders[compiled], identifier)
			}
		}
		for _, identifiers := range host.holders {
			slices.Sort(identifiers)
		}
	})
	return host.holders[filename]
}
