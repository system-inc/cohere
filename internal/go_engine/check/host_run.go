package check

import (
	"slices"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// HostRun is what the host's run keeps for the extra builds to restore packages from: each package's entry by ID, the
// renderings the verdicts make of the host's types, shared by every build, and the digests of the files its keys
// hashed, which an extra build's keys read rather than hash again.
type HostRun struct {
	entries    map[string]*hostEntry
	renderings *ReadsBuild
	files      cache.FileDigests

	// keeping is the time the run spent keeping its packages for the extra builds over every load (Keeping).
	keeping time.Duration

	// kept is how many entries the run kept, which outlives Release (EntriesKept).
	kept int
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
// export data is written from, and the renderings, keyed by the host's objects (#b2t9xmf). What the record counts of
// it stays.
func (host *HostRun) Release() {
	if host == nil {
		return
	}
	host.entries = nil
	host.renderings = nil
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

// hostEntry is one package the host's run kept: what a verdict judges it by, its portable digest, and its entry, its
// body and its record (entryRecord), and export data, read from the store when the run wrote them there, or, under
// `--no-cache`, which never stores them, held in memory: the entry encoded at the host's tail, and the export data,
// most of the cost, written from the package's types only when a load first reads it, after the package's late
// decision held, so a package no build restores never has it written (lazyExportData, #b2t9xmf). contents and the
// reader it returns may be called from several goroutines at once.
//
// bodyAt is the key the host's record of it is under, when the run wrote it to the store, holding its body, which an
// extra build writing the package through names rather than copy (#fq7dxvx); holdsBodyAt is whether there is one.
type hostEntry struct {
	restoring   Restoring
	portable    cache.Key
	contents    func() (body []byte, record entryRecord, readExportData func() ([]byte, error), readError error)
	bodyAt      cache.Key
	holdsBodyAt bool
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
