package check

import (
	"maps"
	"reflect"
	"sync"
	"sync/atomic"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// hostEntry is one package the host's run kept: what a verdict judges it by, its portable digest, and its entry, its
// body and its record (entryRecord), and export data, read from the store when the run wrote them there, or, under
// `--no-cache`, which never stores them, held in memory: the entry encoded at the host's tail, and the export data,
// most of the cost, written from the package's types only when a load first reads it, after the package's late
// decision held, so a package no build restores never has it written (lazyExportData, #b2t9xmf). contents and the
// reader it returns may be called from several goroutines at once.
//
// bodyAt is the key the host's record of it is under, when the run wrote it to the store, holding its body, which an
// extra build writing the package through names rather than copy (#fq7dxvx); holdsBodyAt is whether there is one.
//
// decoded is the entry read and decoded whole, once a run however many builds consider it (decodeOnce), and the
// error every build then sees as the same miss: the gob of its body and every fact by reflection, which each build
// decoded again from the same bytes for the same analyzers, about 1.2 GB of gob and 0.5 GB of facts across the builds
// at cohere's root (#954dadc). decodes counts the decodes it ran, which a test reads.
//
// factTypes are restoring's ReadFactTypes, read once by the first of every build's verdicts to ask (readFactTypes).
type hostEntry struct {
	restoring   Restoring
	portable    cache.Key
	contents    func() (body []byte, record entryRecord, readExportData func() ([]byte, error), readError error)
	bodyAt      cache.Key
	holdsBodyAt bool
	decoded     func() (*restoredPackage, error)
	decodes     atomic.Int64

	factTypesOnce sync.Once
	factTypes     map[reflect.Type]bool
}

// readFactTypes is kept's ReadFactTypes, read once however many builds' verdicts ask at once (#nrk4zt0): restoring is
// never written once the entry is made, and FactsHold only reads the set, so every verdict shares one.
func (kept *hostEntry) readFactTypes() map[reflect.Type]bool {
	kept.factTypesOnce.Do(func() { kept.factTypes = ReadFactTypes(kept.restoring) })
	return kept.factTypes
}

// decodeOnce is the decode of kept, the entry of the package identified so, shared by every build (decoded), by known,
// the host's analyzers, read once kept's contents is set. It holds the identifier alone, as a package whose ID
// decodeEntry names in what it says: the host's package, with its syntax and type maps, would live as long as the
// entry.
func decodeOnce(kept *hostEntry, identifier string, known *cacheAnalyzers) func() (*restoredPackage, error) {
	named := &packages.Package{ID: identifier}
	return sync.OnceValues(func() (*restoredPackage, error) {
		kept.decodes.Add(1)
		body, record, readExportData, readError := kept.contents()
		if readError != nil {
			return nil, readError
		}
		return decodeEntry(known, named, body, record, readExportData)
	})
}

// restoredForBuild is shared, an entry's decode, as one build restores it: a package and types of its own, since the
// build's restore writes the types' Holds, Declined and Untouched, over everything else of shared, its analyses, the
// facts and findings in them, its answers, reads, files, slots and bodiless, which no build writes, and its export data
// reader, the host's.
func restoredForBuild(shared *restoredPackage) *restoredPackage {
	restored := *shared
	ownTypes := *shared.types
	restored.types = &ownTypes
	return &restored
}

// decodesAlike reports whether an entry decodes the same by left as by right: the same analyzers in the same slots, the
// same report analyzers, and the same fact types by name, which is all an entry's decode reads of them (restoredOf,
// decodeAnalysis, decodeFact). Every build of a run has its session's made from the same enabled rules, so the host's
// and each build's are alike, and a build whose differ decodes the host's entries itself.
func decodesAlike(left *cacheAnalyzers, right *cacheAnalyzers) bool {
	if left == nil || right == nil {
		return false
	}
	return maps.Equal(left.slots, right.slots) && maps.Equal(left.reports, right.reports) &&
		maps.EqualFunc(left.factTypes, right.factTypes, func(leftTypes, rightTypes map[string]reflect.Type) bool {
			return maps.Equal(leftTypes, rightTypes)
		})
}
