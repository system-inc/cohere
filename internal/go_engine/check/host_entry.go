package check

import (
	"reflect"
	"sync"

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
// factTypes are restoring's ReadFactTypes, read once by the first of every build's verdicts to ask (readFactTypes).
type hostEntry struct {
	restoring   Restoring
	portable    cache.Key
	contents    func() (body []byte, record entryRecord, readExportData func() ([]byte, error), readError error)
	bodyAt      cache.Key
	holdsBodyAt bool

	factTypesOnce sync.Once
	factTypes     map[reflect.Type]bool
}

// readFactTypes is kept's ReadFactTypes, read once however many builds' verdicts ask at once (#nrk4zt0): restoring is
// never written once the entry is made, and FactsHold only reads the set, so every verdict shares one.
func (kept *hostEntry) readFactTypes() map[reflect.Type]bool {
	kept.factTypesOnce.Do(func() { kept.factTypes = ReadFactTypes(kept.restoring) })
	return kept.factTypes
}
