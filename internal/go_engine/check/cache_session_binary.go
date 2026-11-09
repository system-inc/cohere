package check

import (
	"encoding/json"
	"flag"
	"fmt"
	"runtime"
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// restoreFromBinary keys every standard-library package among reachable (see cache.StandardLibraryKeys), reading the
// digest of each file hashed holds rather than hashing it again, and makes a hit of each one the binary's table holds
// an entry for that decodes whole, its Withheld and export data renamed from goRootMarker to the run's own `GOROOT`.
// The renames are done here, before the load, as many at once as there are cores, so a package whose export data does
// not rename is a miss, analyzed as any other, rather than a hit whose types the load checks from source. A package
// Analyze was given is never restored from the table, which holds no findings, and neither is one with a list error,
// nor one the run reloads to answer a question (see Reload). A
// table that does not parse, or keys that cannot be computed, are a problem, and every standard-library package is
// then computed.
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
			restored, restoreError := restoreBinaryEntry(session.known, restoring, entry, rename)
			if restoreError != nil {
				return
			}
			hitsLock.Lock()
			defer hitsLock.Unlock()
			session.hits[restoring] = restored
			session.fromBinary[restoring] = true
		})
	}
	group.Wait()
}

// restoreBinaryEntry is one standard-library package's entry from the binary's table decoded whole, with every file
// its Withheld and its export data name renamed by rename, or an error, which is a miss, as is an entry of another
// package than restoring.
func restoreBinaryEntry(
	known *cacheAnalyzers,
	restoring *packages.Package,
	entry cache.StandardLibraryEntry,
	rename func(string) (string, error),
) (*restoredPackage, error) {
	var exportData []byte
	if entry.ExportData != nil {
		renamed, renameError := renameExportDataFiles(entry.ExportData, restoring.PkgPath, rename)
		if renameError != nil {
			return nil, renameError
		}
		exportData = renamed
	}
	tableEntry, decodeError := decodeStandardLibraryEntry(entry.Entry)
	if decodeError != nil {
		return nil, decodeError
	}
	if tableEntry.PackagePath != restoring.PkgPath {
		return nil, fmt.Errorf("the binary's table holds %s under the key of %s", tableEntry.PackagePath,
			restoring.PkgPath)
	}
	restored, restoreError := restoredOf(known, restoring, cacheEntryOf(tableEntry), func() ([]byte, error) {
		if exportData == nil {
			return nil, fmt.Errorf("the binary's table holds no export data for %s", restoring.ID)
		}
		return exportData, nil
	})
	if restoreError != nil {
		return nil, restoreError
	}
	withheld, renameError := renameWithheldFiles(restored.types.Withheld, rename)
	if renameError != nil {
		return nil, renameError
	}
	restored.types.Withheld = withheld
	return restored, nil
}

// collects reports whether the run collects saved's entry for the binary's table (see collect).
func (session *CacheSession) collects(saved *packages.Package) bool {
	if session.caching.StandardLibrary == nil || session.caching.StandardLibrary.Collected == nil {
		return false
	}
	_, standard := session.standardLibraryKeys[saved]
	return standard
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
