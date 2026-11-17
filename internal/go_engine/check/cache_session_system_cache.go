package check

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"

	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// tableHit is what the host's session holds of one package it restored from the system cache's table, for its extra
// builds to restore from (keepTableHit): the table's entry decoded, its export data renamed to the run's `GOROOT`, and
// its persisted reads as the table holds them, nil for none.
type tableHit struct {
	entry      standardLibraryEntry
	exportData []byte
	reads      []byte
}

// restoreFromSystemCache keys every standard-library package among reachable (see cache.StandardLibraryKeys), reading
// the digest of each file hashed holds rather than hashing it again, reads the system cache's table of the build's
// context (readSystemCacheTable), and makes a hit of each package it holds an entry for that decodes whole, its
// Withheld and export data renamed from goRootMarker to the run's own `GOROOT`. The renames are done here, before the
// load, so a package whose export data does not rename is a miss, analyzed as any other, rather than a hit whose types
// the load checks from source. Export data is shallow, so each package's is read against the table's entries of the
// packages it imports, which are read first, one package at a time, from every package keyed that the table holds an
// entry of, held or not (renameTableExportData); the rest of each restore runs as many at once as there are cores. A
// package Analyze was given is never restored from the table, which holds no findings, and neither is one with a list
// error, nor one the run reloads to answer a question (see Reload). A table that is damaged or does not parse, or keys
// that cannot be computed, are a problem, and every standard-library package is then computed. So is an entry that
// does not restore, its package alone computed, and the run's write replaces it (refusedFromTable). The host's
// session of a run with extra builds holds what it restored of each, which it keeps for the builds as it keeps a
// package it checked (keepTableHit, #pcv6eff).
func (session *CacheSession) restoreFromSystemCache(
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
	session.standardLibraryByPath = make(map[string]*packages.Package, len(keys))
	for keyed := range keys {
		session.standardLibraryByPath[keyed.PkgPath] = keyed
	}
	session.standardLibraryContext = &shared
	table := session.readSystemCacheTable(shared)
	session.systemCacheTable = table
	if table == nil {
		return
	}
	held := map[*packages.Package]cache.StandardLibraryEntry{}
	for keyed, key := range keys {
		if session.given[keyed] || len(keyed.Errors) > 0 || session.reloading(keyed) {
			continue
		}
		if entry, found := table.Lookup(key); found {
			held[keyed] = entry
		}
	}
	if tableProblem := table.Problem(); tableProblem != nil {
		session.problem(fmt.Sprintf("the system cache's table of the standard library: %v", tableProblem))
		return
	}
	rename := renameFromGoRootMarker(shared.Goroot)
	renamed := renameTableExportData(keys, table, held, rename)
	var hitsLock sync.Mutex
	restorings := slices.Collect(maps.Keys(held))
	session.fanOut(len(restorings), func(index int) {
		restoring := restorings[index]
		restored, hit, restoreError := restoreTableEntry(session.known, restoring, held[restoring],
			renamed[restoring], rename)
		hitsLock.Lock()
		defer hitsLock.Unlock()
		// An entry the table holds that does not restore is damage the run says, computes again, and replaces at its
		// write (withoutDamage): left in the table unsaid, it would miss on every run that reads it.
		if restoreError != nil {
			session.refusedFromTable[keys[restoring]] = true
			session.problem(fmt.Sprintf("restoring %s from the system cache's table: %v", restoring.ID,
				restoreError))
			return
		}
		session.hits[restoring] = restored
		session.fromSystemCache[restoring] = true
		if session.keepsForBuilds {
			session.tableHits[restoring] = hit
		}
	})
}

// readSystemCacheTable is the system cache's table of the build's context, shared with its fact analyzers named,
// unparsed, and nil when there is none: a run with no system cache, or one that holds no table of this context, which
// is no problem, since every standard-library package is then computed. A table found damaged, cut short or written
// over, which the store reads as a miss and removes, is a problem the run says, and is computed again. A table found
// is marked used, so a trim keeps it (cache.TrimSystemCache).
func (session *CacheSession) readSystemCacheTable(shared cache.StandardLibraryContext) *cache.StandardLibraryTable {
	system := session.caching.StandardLibrary.System
	if system == nil {
		return nil
	}
	key := cache.StandardLibraryTableKey(shared)
	// Asked before the read, since a read that finds the file damaged removes it: a file there that does not read
	// whole is damage, where no file is a table never written. By the time Get misses, it has removed the damaged
	// file already, so the next run finds no table and computes the standard library as this one does. A table a trim
	// removed between the two reads as damaged once, which says more than it must and changes no finding.
	held := system.Holds(key)
	contents, found := system.Get(key)
	if !found {
		if held {
			session.problem(fmt.Sprintf("the system cache's table of the standard library under %s was damaged, so "+
				"the standard library is computed", key))
		}
		return nil
	}
	system.MarkUsed(key)
	return cache.NewStandardLibraryTable(contents)
}

// StandardLibraryContext is what every standard-library key of the last Restore shared, its fact analyzers named, by
// which the system cache's table of the build's context is keyed (cache.StandardLibraryTableKey), and false before a
// Restore keyed the standard library or when it could not.
func (session *CacheSession) StandardLibraryContext() (cache.StandardLibraryContext, bool) {
	if session.standardLibraryContext == nil {
		return cache.StandardLibraryContext{}, false
	}
	return *session.standardLibraryContext, true
}

// renamedExportData is a package's export data from the system cache's table renamed (renameTableExportData), nil for
// unsafe, which has none, or why it did not rename.
type renamedExportData struct {
	exportData  []byte
	renameError error
}

// renameTableExportData is the export data of each of held, the packages restoreFromSystemCache restores from table,
// renamed by rename. Each is read shallow against the table's entries of every package it imports, read first, among
// every package keyed whose key the table holds an entry under, so a package held or not, as one Analyze was given,
// still lends its types to the packages that import it. One that the table does not hold, or whose export data does
// not read, is a rename error of every held package that reaches it.
func renameTableExportData(
	keys map[*packages.Package]cache.Key,
	table *cache.StandardLibraryTable,
	held map[*packages.Package]cache.StandardLibraryEntry,
	rename func(string) (string, error),
) map[*packages.Package]renamedExportData {
	exportData := newStandardLibraryExportData()
	for keyed, key := range keys {
		entry, found := table.Lookup(key)
		if !found {
			continue
		}
		imports := make([]string, 0, len(keyed.Imports))
		for _, imported := range keyed.Imports {
			imports = append(imports, imported.PkgPath)
		}
		// Sorted, so a package whose two imports both fail to read names the same one each run.
		slices.Sort(imports)
		exportData.add(keyed.PkgPath, exportDataSource{imports: imports, exportData: entry.ExportData})
	}
	renamed := make(map[*packages.Package]renamedExportData, len(held))
	for restoring, entry := range held {
		if entry.ExportData == nil {
			renamed[restoring] = renamedExportData{}
			continue
		}
		read, readFileSet, readError := exportData.readPackage(restoring.PkgPath)
		if readError != nil {
			renamed[restoring] = renamedExportData{renameError: readError}
			continue
		}
		written, renameError := renameExportDataFiles(read, readFileSet, rename)
		renamed[restoring] = renamedExportData{exportData: written, renameError: renameError}
	}
	return renamed
}

// restoreTableEntry is one standard-library package's entry from the system cache's table decoded whole, with its
// export data as renameTableExportData renamed it and every file its Withheld names renamed by rename, and what the
// host keeps of it for its extra builds, or an error, which is a miss, as is an entry of another package than restoring
// and one whose export data did not rename.
func restoreTableEntry(
	known *cacheAnalyzers,
	restoring *packages.Package,
	entry cache.StandardLibraryEntry,
	renamed renamedExportData,
	rename func(string) (string, error),
) (*restoredPackage, tableHit, error) {
	if renamed.renameError != nil {
		return nil, tableHit{}, renamed.renameError
	}
	tableEntry, decodeError := decodeStandardLibraryEntry(entry.Entry)
	if decodeError != nil {
		return nil, tableHit{}, decodeError
	}
	if tableEntry.PackagePath != restoring.PkgPath {
		return nil, tableHit{}, fmt.Errorf("the system cache's table holds %s under the key of %s",
			tableEntry.PackagePath, restoring.PkgPath)
	}
	readExportData := func() ([]byte, error) {
		if renamed.exportData == nil {
			return nil, fmt.Errorf("the system cache's table holds no export data for %s", restoring.ID)
		}
		return renamed.exportData, nil
	}
	restored, restoreError := restoredOf(known, restoring, cacheEntryOf(tableEntry), readExportData)
	if restoreError != nil {
		return nil, tableHit{}, restoreError
	}
	withheld, renameError := renameWithheldFiles(restored.types.Withheld, rename)
	if renameError != nil {
		return nil, tableHit{}, renameError
	}
	restored.types.Withheld = withheld
	tableEntry.Withheld = withheld
	return restored, tableHit{entry: tableEntry, exportData: renamed.exportData, reads: entry.Reads}, nil
}

// keepTableHit is what the host's run keeps for its extra builds of kept, a package it restored from the system cache's
// table as hit, whose portable digest is portable: the table's entry as the store would hold it, its export data, and
// its reads as the table persisted them, read by the analyzers' fact types (heldReadsOf), so a build judges it as it
// judges a package the host checked (#pcv6eff). A package the table persisted no reads of is kept with none, and a
// build declines it as unpersisted.
func (session *CacheSession) keepTableHit(kept *packages.Package, hit tableHit, portable cache.Key) (*hostEntry,
	error,
) {
	whole := cacheEntryOf(hit.entry)
	body, encodeError := encodeBody(whole)
	if encodeError != nil {
		return nil, fmt.Errorf("keeping %s, restored from the system cache's table: %w", kept.ID, encodeError)
	}
	// The table names no export data by digest: a build writing the package through names what it read.
	record := entryRecord{Version: whole.Version, Body: contentDigest(body), Reads: whole.Reads,
		ReadsIncomplete: whole.ReadsIncomplete}
	restoring := restoringOf(kept, nil)
	if hit.reads != nil {
		restoring.persisted = heldReadsOf(hit.reads, learnedFactTypes(session.known))
	}
	return &hostEntry{restoring: restoring, portable: portable,
		contents: func() ([]byte, entryRecord, func() ([]byte, error), error) {
			return body, record, func() ([]byte, error) {
				if hit.exportData == nil {
					return nil, fmt.Errorf("the system cache's table holds no export data for %s", kept.ID)
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
// other packages, reads, persisted by a memo measured by the run's sizes, its target's, as a table carries them beside
// each entry (#pcv6eff). The caller hands the reads once every type-read walk has ended, nil for a run that records
// none, as an extra build's. Each package whose reads the run recorded is judged (ReadsJudged), which a table asks of
// each entry it takes: persisted when they can be, and kept with none when they cannot, as when marked never, which a
// build declines as unpersisted. One whose reads the run never recorded stays unjudged, so no table takes it.
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
		recorded := reads[keyed]
		if recorded == nil {
			continue
		}
		collected.ReadsJudged = true
		if record, persisted := persistReads(restoringOf(keyed, recorded), memo,
			ReadsRendererVersion); persisted {
			encoded, encodeError := encodePersistedReads(record)
			if encodeError != nil {
				session.problem(fmt.Sprintf("persisting the reads of %s: %v", keyed.ID, encodeError))
				continue
			}
			collected.Reads = encoded
		}
		standardLibrary.Collected[key] = collected
	}
}

// waitingEntry is a standard-library package's entry as the run encoded it, which waits for the system cache's table
// before the store takes it under storeKey, the package's key in the store, if the table refuses it
// (WriteSystemCacheTable), with record, its dependence record under `--build-dependence`, nil for none.
//
// Each waits with its body and export data until the build's table is written, beside Collected's copy of both, the
// host's until every extra build has ended: the price of deciding at the table's write rather than at the save.
type waitingEntry struct {
	storeKey cache.Key
	entry    encodedEntry
	record   []byte
}

// collect adds a standard-library package the run just encoded, with demanded, the answers and reads its entry holds,
// to the entries the run collects, when it collects any (see StandardLibraryCaching's Collected). It records what
// standardLibraryEntryWhole later judges the entry by, which no table takes before then: the slots of the run's fact
// analyzers, each of which a whole entry holds an analysis of, since every action on a package leaves its analysis in
// the entry, facts or none, so the entry says which ran and the session says which had to; and whether its answers
// were computed whole. An entry whose write to the store waits for the table (KeptBySystemCache) is kept as encoded
// until then. The caller holds the lock the save counts under.
func (session *CacheSession) collect(saved *packages.Package, entry encodedEntry, demanded entryDemand) {
	standardLibrary := session.caching.StandardLibrary
	if standardLibrary == nil || standardLibrary.Collected == nil {
		return
	}
	key, standard := session.standardLibraryKeys[saved]
	if !standard {
		return
	}
	// The table is written from whole entries, reads and body together.
	whole, wholeError := wholeEntry(entry)
	if wholeError != nil {
		session.problem(fmt.Sprintf("collecting %s for the standard library's table: %v", saved.ID, wholeError))
		return
	}
	factAnalyzers := make([]string, 0, len(session.known.facts))
	for _, analyzer := range session.known.facts {
		factAnalyzers = append(factAnalyzers, session.known.slots[analyzer])
	}
	// The resolver learns a package as ctrlflow's action on it ends, and answers a question only of a package it
	// learned (fact_resolver's Owner), so a rule that asks a fact on demand runs ctrlflow. In a run with no ctrlflow
	// nothing is ever asked, and neither is anything in a run that reads this table, since its key holds the fact
	// analyzers, ctrlflow among them whenever a rule asks: the answers it holds, none, are all there will ever be.
	asksOnDemand := slices.Contains(session.known.facts, ctrlflow.Analyzer)
	standardLibrary.Collected[key] = StandardLibraryRunEntry{
		PackagePath:   saved.PkgPath,
		Entry:         whole,
		ExportData:    entry.exportData,
		FactAnalyzers: factAnalyzers,
		AnswersWhole:  demanded.answersWhole || !asksOnDemand,
	}
	if session.caching.Store != nil && session.KeptBySystemCache(saved) {
		if session.waitingForTable == nil {
			session.waitingForTable = map[cache.Key]waitingEntry{}
		}
		session.waitingForTable[key] = waitingEntry{storeKey: session.keys[saved], entry: entry}
	}
}

// waitWithRecord gives saved's entry, waiting for the system cache's table, its dependence record, which the store
// takes beside it if the table refuses it (storeWhatTheTableRefused), so a run under `--build-dependence` finds the
// record a store hit needs rather than backfill it every run. The caller holds the lock the save counts under.
func (session *CacheSession) waitWithRecord(saved *packages.Package, record []byte) {
	key, standard := session.standardLibraryKeys[saved]
	if !standard {
		return
	}
	if waited, waiting := session.waitingForTable[key]; waiting {
		waited.record = record
		session.waitingForTable[key] = waited
	}
}

// collectWithoutStore collects, in a session with no store, as under `--no-cache`, each standard-library package the
// last load computed: one the run keyed apart and did not restore, with no load error, every action of graph on it
// finished clean and nothing unsettled below it, as saveMisses saves a miss. Such a session writes nothing to a store,
// so saveMisses never runs in it, and the system cache is the one place its work on the standard library is kept.
func (session *CacheSession) collectWithoutStore(
	graph *checker.Graph,
	withheld map[*packages.Package]load.PackageWithheld,
) {
	standardLibrary := session.caching.StandardLibrary
	if standardLibrary == nil || standardLibrary.Collected == nil || session.known == nil {
		return
	}
	actionsOf := map[*packages.Package][]*checker.Action{}
	if graph != nil {
		for action := range graph.All() {
			actionsOf[action.Package] = append(actionsOf[action.Package], action)
		}
	}
	unsettled := session.unsettledBelow()
	var saving []*packages.Package
	for keyed, key := range session.standardLibraryKeys {
		if _, collected := standardLibrary.Collected[key]; collected || session.restored(keyed) || unsettled[keyed] ||
			!session.savable("collectWithoutStore", keyed, actionsOf[keyed]) {
			continue
		}
		saving = append(saving, keyed)
	}
	var collectLock sync.Mutex
	session.fanOutWriting(len(saving), func(index int) func() {
		saved := saving[index]
		demanded := session.withBodiless(saved, session.demandOf(saved, session.previous[saved]))
		entry, encodeError := encodeEntry(session.known, session.ruleOf, saved, actionsOf[saved], withheld[saved],
			demanded, nil, nil, nil, true)
		return func() {
			collectLock.Lock()
			defer collectLock.Unlock()
			switch {
			case errors.Is(encodeError, errorUnstorable):
				session.counts.Unstorable++
			case encodeError != nil:
				session.problem(encodeError.Error())
			default:
				session.collect(saved, entry, demanded)
			}
		}
	})
}

// HasCollected reports whether the session collected any standard-library entry, whose reads the run then persists
// (PersistCollectedReads) and whose table it writes (WriteSystemCacheTable), which a run that collected none, as a
// warm one, spares itself: the host's reads are whole only once its type-read walks end, which nothing else waits for.
func (session *CacheSession) HasCollected() bool {
	standardLibrary := session.caching.StandardLibrary
	return standardLibrary != nil && len(standardLibrary.Collected) > 0
}

// WriteSystemCacheTable writes back to the system cache the table of the build's context, once the run's reads are
// whole (PersistCollectedReads): the table as the run read it, every entry kept as it was, and beside it each entry
// the run collected whole (standardLibraryEntryWhole) that the table did not hold, all under the table's key in one
// Put, which renames a whole file into place. It returns how many entries it added, and writes nothing when that is
// none, as on a warm run, so a run writes its table only when it has something to add.
//
// Every entry that waited and that the table does not hold, short or left out for an import it could not read, and
// every one when the table could not be written or this returned before writing, is put in the project's store then,
// under its package's key, as saveMisses puts any miss, with its dependence record when it has one (#syscache): the
// next run hits it there, and dropShortHits judges a short one as it judges any store hit, rather than check it from
// source every run. A warm extra build hits what it checked only for the types of a package above it restored from the
// host's run, where no fact analyzer's action reached it, as it always has. One the table holds already, whose good
// held entry the run computed again, stays out of the store: the table answers for it.
//
// Two runs writing the same table each write what they read and their own, and the later rename wins: the earlier
// one's entries are lost and computed again by the next run, a miss and never a wrong read, since each entry stands
// under its package's full key. A table that did not parse, or was damaged, is written anew from this run's entries.
// An error is the run's reason it could not keep the system cache, never a finding's.
func (session *CacheSession) WriteSystemCacheTable() (added int, returned error) {
	// The keys the system cache's table holds once this returns: the table as read until a write replaces it. Whatever
	// waited and is not among them is in the store by then, the first write that failed joined to what this returns.
	inTable := map[cache.Key]bool{}
	defer func() { returned = errors.Join(returned, session.storeWhatTheTableRefused(inTable)) }()
	standardLibrary := session.caching.StandardLibrary
	if standardLibrary == nil || standardLibrary.System == nil || len(standardLibrary.Collected) == 0 ||
		session.standardLibraryContext == nil {
		return 0, nil
	}
	shared := *session.standardLibraryContext
	held := map[cache.Key]cache.StandardLibraryEntry{}
	if read := session.systemCacheTable; read != nil && read.Problem() == nil {
		for _, key := range read.Keys() {
			// Ignored: the key is one of the table's own.
			held[key], _ = read.Lookup(key)
			inTable[key] = true
		}
	}
	readAgainst := make(map[cache.Key]bool, len(session.standardLibraryKeys))
	for _, key := range session.standardLibraryKeys {
		readAgainst[key] = true
	}
	table, holds, added, tableError := standardLibraryTableWith(standardLibrary.Collected, held,
		session.refusedFromTable, readAgainst, shared.Goroot, nil)
	if tableError != nil {
		return 0, fmt.Errorf("writing the system cache's table of the standard library: %w", tableError)
	}
	if added == 0 {
		return 0, nil
	}
	if putError := standardLibrary.System.Put(cache.StandardLibraryTableKey(shared), table); putError != nil {
		return 0, fmt.Errorf("writing the system cache's table of the standard library: %w", putError)
	}
	inTable = holds
	return added, nil
}

// storeWhatTheTableRefused puts in the project's store each entry that waited for the system cache's table and is
// not among inTable, the keys the table holds now (WriteSystemCacheTable), each with its dependence record when it
// waited with one, forgets every one that waited, and returns the first write that failed. The run's counts were
// taken at its save, so a failed write is not among the store's counted problems or its writes: the run says it as the
// reason it could not keep the system cache as it should, since the write is the system cache's handing back. Nor does
// such an entry get an alias under its second key, which the run gives only what saveMisses wrote; the next run
// reaches it by its first key.
func (session *CacheSession) storeWhatTheTableRefused(inTable map[cache.Key]bool) error {
	waiting := session.waitingForTable
	session.waitingForTable = nil
	var refused []waitingEntry
	for key, waited := range waiting {
		if !inTable[key] {
			refused = append(refused, waited)
		}
	}
	putErrors := make([]error, len(refused))
	session.fanOutWriting(len(refused), func(index int) func() {
		return func() {
			waited := refused[index]
			putErrors[index] = session.putEntry(waited.storeKey, waited.entry)
			// A dependence record follows its entry, and only an entry whose write held (#6a3kjvz).
			if putErrors[index] == nil && waited.record != nil {
				putErrors[index] = session.caching.Store.Put(DependenceRecordKey(waited.storeKey), waited.record)
			}
		}
	})
	for _, putError := range putErrors {
		if putError != nil {
			return fmt.Errorf("keeping in the project's store a standard-library entry the table refused: %w",
				putError)
		}
	}
	return nil
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
