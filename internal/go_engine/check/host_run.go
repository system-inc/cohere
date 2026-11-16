package check

import (
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// HostRun is what the host's run keeps for the extra builds to restore packages from: each package's entry by ID, the
// renderings the verdicts make of the host's types, shared by every build, and the digests of the files its keys
// hashed, which an extra build's keys read rather than hash again. known is what the host's session knows of its
// analyzers, which each entry's shared decode reads (hostEntry.decoded), and which a build's own must decode alike for
// the build to share it (decodesAlike).
type HostRun struct {
	entries    map[string]*hostEntry
	renderings *ReadsBuild
	files      cache.FileDigests
	known      *cacheAnalyzers

	// keeping is the time the run spent keeping its packages for the extra builds over every load (Keeping).
	keeping time.Duration

	// kept is how many entries the run kept, which outlives Release (EntriesKept).
	kept int

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

// Release lets go of everything the host's run kept for the extra builds once the last of them has run, since no
// build can ask for an entry, or a verdict render a host type, after that: the entries, with the host's types their
// export data is written from, each with its decode the builds shared (hostEntry.decoded), and the renderings, keyed by
// the host's objects (#b2t9xmf). What the record counts of it stays.
func (host *HostRun) Release() {
	if host == nil {
		return
	}
	host.entries = nil
	host.renderings = nil
	host.known = nil
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
