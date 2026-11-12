package check

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// binaryHit is what the host's session holds of one package it restored from the binary's table, for its extra builds
// to restore from (keepBinaryHit): the table's entry decoded, its export data renamed to the run's `GOROOT`, and its
// persisted reads as the table holds them, nil for none.
type binaryHit struct {
	entry      standardLibraryEntry
	exportData []byte
	reads      []byte
}

// restoreFromBinary keys every standard-library package among reachable (see cache.StandardLibraryKeys), reading the
// digest of each file hashed holds rather than hashing it again, and makes a hit of each one the binary's table holds
// an entry for that decodes whole, its Withheld and export data renamed from goRootMarker to the run's own `GOROOT`.
// The renames are done here, before the load, as many at once as there are cores, so a package whose export data does
// not rename is a miss, analyzed as any other, rather than a hit whose types the load checks from source. A package
// Analyze was given is never restored from the table, which holds no findings, and neither is one with a list error,
// nor one the run reloads to answer a question (see Reload). A
// table that does not parse, or keys that cannot be computed, are a problem, and every standard-library package is
// then computed. The host's session of a run with extra builds holds what it restored of each, which it keeps for the
// builds as it keeps a package it checked (keepBinaryHit, #pcv6eff).
func (session *CacheSession) restoreFromBinary(
	loader string,
	reachable []*packages.Package,
	hashed cache.FileDigests,
) {
	standardLibrary := session.caching.StandardLibrary
	if standardLibrary == nil {
		return
	}
	shared := standardLibrary.Context
	shared.Facts = factAnalyzerSet(session.known)
	shared.Loader = loader
	keys, keysError := cache.StandardLibraryKeys(reachable, shared, hashed)
	if keysError != nil {
		session.problem(fmt.Sprintf("computing the standard library's keys: %v", keysError))
		return
	}
	session.standardLibraryKeys = keys
	if standardLibrary.Table == nil {
		return
	}
	held := map[*packages.Package]cache.StandardLibraryEntry{}
	for keyed, key := range keys {
		if session.given[keyed] || len(keyed.Errors) > 0 || session.reloading(keyed) {
			continue
		}
		if entry, found := standardLibrary.Table.Lookup(key); found {
			held[keyed] = entry
		}
	}
	if tableProblem := standardLibrary.Table.Problem(); tableProblem != nil {
		session.problem(tableProblem.Error())
		return
	}
	rename := renameFromGoRootMarker(shared.Goroot)
	var group sync.WaitGroup
	var hitsLock sync.Mutex
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	for restoring, entry := range held {
		slots <- struct{}{}
		group.Go(func() {
			defer func() { <-slots }()
			restored, hit, restoreError := restoreBinaryEntry(session.known, restoring, entry, rename)
			if restoreError != nil {
				return
			}
			hitsLock.Lock()
			defer hitsLock.Unlock()
			session.hits[restoring] = restored
			session.fromBinary[restoring] = true
			if session.keepsForBuilds {
				session.binaryHits[restoring] = hit
			}
		})
	}
	group.Wait()
}

// restoreBinaryEntry is one standard-library package's entry from the binary's table decoded whole, with every file
// its Withheld and its export data name renamed by rename, and what the host keeps of it for its extra builds, or an
// error, which is a miss, as is an entry of another package than restoring.
func restoreBinaryEntry(
	known *cacheAnalyzers,
	restoring *packages.Package,
	entry cache.StandardLibraryEntry,
	rename func(string) (string, error),
) (*restoredPackage, binaryHit, error) {
	var exportData []byte
	if entry.ExportData != nil {
		renamed, renameError := renameExportDataFiles(entry.ExportData, restoring.PkgPath, rename)
		if renameError != nil {
			return nil, binaryHit{}, renameError
		}
		exportData = renamed
	}
	tableEntry, decodeError := decodeStandardLibraryEntry(entry.Entry)
	if decodeError != nil {
		return nil, binaryHit{}, decodeError
	}
	if tableEntry.PackagePath != restoring.PkgPath {
		return nil, binaryHit{}, fmt.Errorf("the binary's table holds %s under the key of %s", tableEntry.PackagePath,
			restoring.PkgPath)
	}
	readExportData := func() ([]byte, error) {
		if exportData == nil {
			return nil, fmt.Errorf("the binary's table holds no export data for %s", restoring.ID)
		}
		return exportData, nil
	}
	restored, restoreError := restoredOf(known, restoring, cacheEntryOf(tableEntry), readExportData)
	if restoreError != nil {
		return nil, binaryHit{}, restoreError
	}
	withheld, renameError := renameWithheldFiles(restored.types.Withheld, rename)
	if renameError != nil {
		return nil, binaryHit{}, renameError
	}
	restored.types.Withheld = withheld
	tableEntry.Withheld = withheld
	return restored, binaryHit{entry: tableEntry, exportData: exportData, reads: entry.Reads}, nil
}

// keepBinaryHit is what the host's run keeps for its extra builds of kept, a package it restored from the binary's
// table as hit, whose portable digest is portable: the table's entry as the store would hold it, its export data, and
// its reads as the table persisted them, read by the analyzers' fact types (heldReadsOf), so a build judges it as it
// judges a package the host checked (#pcv6eff). A package the table persisted no reads of is kept with none, and a
// build declines it as unpersisted.
func (session *CacheSession) keepBinaryHit(kept *packages.Package, hit binaryHit, portable cache.Key) (*hostEntry,
	error,
) {
	var encoded bytes.Buffer
	if encodeError := gob.NewEncoder(&encoded).Encode(cacheEntryOf(hit.entry)); encodeError != nil {
		return nil, fmt.Errorf("keeping %s, restored from the binary's table: %w", kept.ID, encodeError)
	}
	restoring := restoringOf(kept, nil)
	if hit.reads != nil {
		restoring.persisted = heldReadsOf(hit.reads, learnedFactTypes(session.known))
	}
	entry := encoded.Bytes()
	return &hostEntry{restoring: restoring, portable: portable,
		contents: func() ([]byte, func() ([]byte, error), error) {
			return entry, func() ([]byte, error) {
				if hit.exportData == nil {
					return nil, fmt.Errorf("the binary's table holds no export data for %s", kept.ID)
				}
				return hit.exportData, nil
			}, nil
		}}, nil
}

// learnedFactTypes are the fact types of every analyzer known learns facts of, by factTypeName, as persisted reads
// name them.
func learnedFactTypes(known *cacheAnalyzers) map[string]reflect.Type {
	learned := map[string]reflect.Type{}
	for _, byName := range known.factTypes {
		for name, factType := range byName {
			learned[name] = factType
		}
	}
	return learned
}

// PersistCollectedReads gives each standard-library package the run collected an entry of (collect) its reads of
// other packages, reads, persisted by a memo measured by the run's sizes, its target's, as the release's table carries
// them beside each entry (#pcv6eff). The caller hands the reads once every type-read walk has ended. A package whose
// reads cannot be persisted, never recorded or marked never, keeps none, and a build declines it as unpersisted.
func (session *CacheSession) PersistCollectedReads(reads map[*packages.Package]*build_reads.Reads) {
	standardLibrary := session.caching.StandardLibrary
	if standardLibrary == nil || standardLibrary.Collected == nil {
		return
	}
	memo := NewReadsBuild()
	// Every package of one load has the load's sizes, the target's.
	for keyed := range session.standardLibraryKeys {
		if keyed.TypesSizes != nil {
			memo.measureBy(keyed.TypesSizes)
			break
		}
	}
	for keyed, key := range session.standardLibraryKeys {
		collected, held := standardLibrary.Collected[key]
		if !held {
			continue
		}
		record, persisted := persistReads(restoringOf(keyed, reads[keyed]), memo, ReadsRendererVersion)
		if !persisted {
			continue
		}
		encoded, encodeError := encodePersistedReads(record)
		if encodeError != nil {
			session.problem(fmt.Sprintf("persisting the reads of %s: %v", keyed.ID, encodeError))
			continue
		}
		collected.Reads = encoded
		standardLibrary.Collected[key] = collected
	}
}

// collect adds a standard-library package the run just encoded to the entries the run collects, when it collects
// any (see StandardLibraryCaching's Collected). The caller holds the lock the save counts under.
func (session *CacheSession) collect(saved *packages.Package, entry encodedEntry) {
	standardLibrary := session.caching.StandardLibrary
	if standardLibrary == nil || standardLibrary.Collected == nil {
		return
	}
	key, standard := session.standardLibraryKeys[saved]
	if !standard {
		return
	}
	standardLibrary.Collected[key] = StandardLibraryRunEntry{
		PackagePath: saved.PkgPath,
		Entry:       entry.entry,
		ExportData:  entry.exportData,
	}
}

// factAnalyzerSet is each analyzer that learns facts as its slot, then its flags with their values as one JSON object,
// sorted, which every standard-library key holds: the table's entries hold only what these analyzers learned, under
// these slots, so a run whose fact analyzers, or their options, are not the release's keys every package apart. The
// flags are read, as cacheRuleSet reads a rule's, since a flag's default is what an option left out resolves to.
func factAnalyzerSet(known *cacheAnalyzers) []string {
	set := make([]string, 0, len(known.facts))
	for _, analyzer := range known.facts {
		options := map[string]string{}
		analyzer.Flags.VisitAll(func(option *flag.Flag) {
			options[option.Name] = option.Value.String()
		})
		// Ignored: a map of strings always encodes, and its keys come out sorted.
		encoded, _ := json.Marshal(options)
		set = append(set, known.slots[analyzer]+" "+string(encoded))
	}
	slices.Sort(set)
	return set
}
