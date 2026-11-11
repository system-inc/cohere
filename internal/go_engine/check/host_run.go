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
}

// EntriesKept counts the packages the host's run kept an entry of for the extra builds, each encoded only when a build
// asks for it, so one gob turns out to refuse is still counted here and counted as no entry by the build that asked.
func (host *HostRun) EntriesKept() int {
	if host == nil {
		return 0
	}
	return len(host.entries)
}

// Keeping is the time the host's run spent keeping its packages for the extra builds, over every load: the keys a run
// without a store computes only to keep them, and what each entry holds of the run's answers; the entries themselves
// are encoded when a build first asks for them (lazyEntry), within that build's load. A run that kept nothing spent
// none.
func (host *HostRun) Keeping() time.Duration {
	if host == nil {
		return 0
	}
	return host.keeping
}

// hostEntry is one package the host's run kept: what a verdict judges it by, its portable digest, and its entry and
// export data, read from the store when the run wrote them there, or, under `--no-cache`, which never stores them,
// encoded from the host's analysis the first time a build asks for them and held in memory from then on, so a package
// no build asks about is never encoded (#b2t9xmf). contents may be called from several goroutines at once.
type hostEntry struct {
	restoring Restoring
	portable  cache.Key
	contents  func() (entry []byte, exportData []byte, readError error)
}

// restoringOf is kept, a package of the host's run, as a verdict judges it: its path, its imports' paths, its reads,
// and whether it has assembly files, as asmdecl finds them among its other files.
func restoringOf(kept *packages.Package, reads *build_reads.Reads) Restoring {
	imports := make([]string, 0, len(kept.Imports))
	for _, imported := range kept.Imports {
		imports = append(imports, imported.PkgPath)
	}
	slices.Sort(imports)
	assembly := slices.ContainsFunc(kept.OtherFiles, func(file string) bool { return strings.HasSuffix(file, ".s") })
	return Restoring{Path: kept.PkgPath, Imports: imports, Reads: reads, Assembly: assembly}
}
