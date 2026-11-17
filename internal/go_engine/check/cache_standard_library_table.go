package check

import (
	"bytes"
	"cmp"
	"encoding/gob"
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"reflect"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// standardLibraryEntry is one standard-library package as the system cache's table holds it: what a cacheEntry holds
// that a restore reads, with its import path, and its analyses as a list in the order of their slots rather than a map.
// gob writes a map in Go's iteration order, which is random, so a map would give the same entry other bytes in each
// process, and a table written twice from the same entries would differ; a list in a fixed order gives one encoding
// (see CanonicalStandardLibraryTable). It holds no findings and no walk of files, which StandardLibraryTableOf leaves
// out and refuses.
type standardLibraryEntry struct {
	PackagePath string
	Version     string
	Analyzers   []slotAnalysis
	Imports     []string
	Withheld    load.PackageWithheld
	Output      []byte

	// Answers are the package's answers of every fact kind computed on demand, each computed whole (see
	// cacheEntry's), sorted by kind and objectpath.
	Answers []kindAnswers

	// Reads and ReadsIncomplete are the cacheEntry's, the answers the package's analysis read of other packages, each
	// without its owner's key, which names the generating run's store: a read is judged by its owner's answer
	// (readsHold), which an extra build restoring the package from the host asks, so a table hit kept for the builds is
	// judged as a package the host checked is (#pcv6eff).
	Reads           []storedRead
	ReadsIncomplete bool
}

// slotAnalysis is one analyzer's analysis in a standardLibraryEntry, under its slot.
type slotAnalysis struct {
	Slot     string
	Analysis storedAnalysis
}

// encodeStandardLibraryEntry is entry as the table holds it.
func encodeStandardLibraryEntry(entry standardLibraryEntry) ([]byte, error) {
	var encoded bytes.Buffer
	if encodeError := gob.NewEncoder(&encoded).Encode(entry); encodeError != nil {
		return nil, fmt.Errorf("encoding the table's entry of %s: %w", entry.PackagePath, encodeError)
	}
	return encoded.Bytes(), nil
}

// decodeStandardLibraryEntry is an entry of the table decoded, refused when it is in another format or its analyses
// are not in the order of their slots, each once.
func decodeStandardLibraryEntry(contents []byte) (standardLibraryEntry, error) {
	var entry standardLibraryEntry
	if decodeError := gob.NewDecoder(bytes.NewReader(contents)).Decode(&entry); decodeError != nil {
		return standardLibraryEntry{}, fmt.Errorf("decoding the table's entry: %w", decodeError)
	}
	if entry.Version != CacheFormatVersion {
		return standardLibraryEntry{}, fmt.Errorf("reading the table's entry of %s: it is in format %q, not %q",
			entry.PackagePath, entry.Version, CacheFormatVersion)
	}
	for index := 1; index < len(entry.Analyzers); index++ {
		if entry.Analyzers[index-1].Slot >= entry.Analyzers[index].Slot {
			return standardLibraryEntry{}, fmt.Errorf("reading the table's entry of %s: its analyses are not in "+
				"the order of their slots, each once", entry.PackagePath)
		}
	}
	return entry, nil
}

// cacheEntryOf is the cacheEntry a table's entry stands for, which a restore reads as it reads the store's.
func cacheEntryOf(entry standardLibraryEntry) cacheEntry {
	analyzers := make(map[string]storedAnalysis, len(entry.Analyzers))
	for _, analysis := range entry.Analyzers {
		analyzers[analysis.Slot] = analysis.Analysis
	}
	return cacheEntry{
		Version:         entry.Version,
		Analyzers:       analyzers,
		Imports:         entry.Imports,
		Withheld:        entry.Withheld,
		Output:          entry.Output,
		Answers:         entry.Answers,
		Reads:           entry.Reads,
		ReadsIncomplete: entry.ReadsIncomplete,
	}
}

// StandardLibraryTableOf is the table that holds entries, a finished run's standard-library packages by their
// standard-library keys (see StandardLibraryCaching's Collected), and says it holds variants, written for any machine
// (standardLibraryTableWith), the release's: it holds nothing it did not compute. An entry that is not whole
// (standardLibraryEntryWhole) is left out, and so is one that imports a package the table does not hold, whose export
// data could not be read; the release's check that every package its probe imports was collected says so.
func StandardLibraryTableOf(
	entries map[cache.Key]StandardLibraryRunEntry,
	goRoot string,
	variants []cache.StandardLibraryVariant,
) ([]byte, error) {
	table, _, tableError := standardLibraryTableWith(entries, nil, nil, goRoot, variants)
	return table, tableError
}

// standardLibraryTableWith is the table that holds held, a table's entries as a run read them, each kept as it is, and
// beside them each of entries, a finished run's standard-library packages by their standard-library keys, whose key
// held does not hold, and how many of entries it took. It says it holds variants. Only an entry as whole as the
// release's generator writes one goes in (standardLibraryEntryWhole): a short one, a package checked where no fact
// analyzer's action reached it or whose answers were computed in part, would be restored by every later run that reads
// the table as if it were whole, since an entry the table holds is never written again, so it is left out and computed
// again by the next run, which costs a miss and nothing else.
//
// Each entry taken is written for any machine: a standardLibraryEntry, every file its Withheld and export data name
// renamed below goRoot to goRootMarker (see renameToGoRootMarker), and its findings left out, since no run reads a
// finding of a package it restores from a table, which it never restores when Analyze was given it, and a finding
// names this machine's files. Export data is shallow, so each package's is read against the packages it imports, among
// entries taken and those of held that readAgainst names, which are the reading run's own keys: held may also hold an
// older entry of the same path, under the key of a Go installation since changed, which no read may take for this
// run's. An entry importing a package neither holds, or whose export data does not read against what it imports, is
// left out, as is everything above it, since it could not be restored from the table either. It refuses an entry that
// names a file outside goRoot, which no other machine could resolve, an entry in another format, and one that holds
// the walk of its files, which only a package Analyze was given has. Its facts and its persisted reads are still gob's
// bytes from the process that ran, which CanonicalStandardLibraryTable writes again for the release.
func standardLibraryTableWith(
	entries map[cache.Key]StandardLibraryRunEntry,
	held map[cache.Key]cache.StandardLibraryEntry,
	readAgainst map[cache.Key]bool,
	goRoot string,
	variants []cache.StandardLibraryVariant,
) ([]byte, int, error) {
	rename := renameToGoRootMarker(goRoot)
	// In the keys' order, so the same entries refuse with the same error.
	keys := slices.SortedFunc(maps.Keys(entries), compareCacheKeys)
	decoded := make(map[cache.Key]cacheEntry, len(entries))
	var taking []cache.Key
	// Entries the run computed under a key held already, as a package whose held entry did not restore: held keeps its
	// own, and the run's stands for its path in the reads below, since it is what the run's packages were checked
	// against.
	var recomputed []cache.Key
	for _, key := range keys {
		var entry cacheEntry
		if decodeError := gob.NewDecoder(bytes.NewReader(entries[key].Entry)).Decode(&entry); decodeError != nil {
			return nil, 0, fmt.Errorf("writing the standard library's table: decoding the entry of %s: %w",
				entries[key].PackagePath, decodeError)
		}
		if entry.Version != CacheFormatVersion {
			return nil, 0, fmt.Errorf("writing the standard library's table: the entry of %s is in format %q, not "+
				"%q", entries[key].PackagePath, entry.Version, CacheFormatVersion)
		}
		if _, kept := held[key]; kept {
			decoded[key] = entry
			recomputed = append(recomputed, key)
			continue
		}
		if !standardLibraryEntryWhole(entries[key], entry) {
			continue
		}
		decoded[key] = entry
		taking = append(taking, key)
	}
	// The packages a read may reach: those of held the reading run keyed, and those taken, which stand for their path
	// over any of held's, since they are this run's.
	readable := map[string]exportDataSource{}
	for key := range readAgainst {
		heldEntry, kept := held[key]
		if !kept {
			continue
		}
		tableEntry, decodeError := decodeStandardLibraryEntry(heldEntry.Entry)
		if decodeError != nil {
			continue
		}
		readable[tableEntry.PackagePath] = exportDataSource{imports: tableEntry.Imports,
			exportData: heldEntry.ExportData}
	}
	for _, key := range slices.Concat(recomputed, taking) {
		readable[entries[key].PackagePath] = exportDataSource{imports: decoded[key].Imports,
			exportData: entries[key].ExportData}
	}
	// An entry importing a package nothing readable holds is left out, which may leave out what imports it in turn.
	for dropping := true; dropping; {
		dropping = false
		taking = slices.DeleteFunc(taking, func(key cache.Key) bool {
			for _, imported := range decoded[key].Imports {
				if _, reachable := readable[imported]; !reachable {
					delete(readable, entries[key].PackagePath)
					dropping = true
					return true
				}
			}
			return false
		})
	}
	exportData := newStandardLibraryExportData()
	for path, source := range readable {
		exportData.add(path, source)
	}
	portable := make(map[cache.Key]cache.StandardLibraryEntry, len(held)+len(taking))
	maps.Copy(portable, held)
	added := 0
	for _, key := range taking {
		collected, entry := entries[key], decoded[key]
		if len(entry.Files) > 0 {
			return nil, 0, fmt.Errorf("writing the standard library's table: the entry of %s holds the walk of its "+
				"files, as only a package the run was asked to analyze does", collected.PackagePath)
		}
		withheld, renameError := renameWithheldFiles(entry.Withheld, rename)
		if renameError != nil {
			return nil, 0, fmt.Errorf("writing the standard library's table: the entry of %s: %w",
				collected.PackagePath, renameError)
		}
		tableEntry := standardLibraryEntry{
			PackagePath:     collected.PackagePath,
			Version:         entry.Version,
			Imports:         entry.Imports,
			Withheld:        withheld,
			Output:          entry.Output,
			Answers:         entry.Answers,
			ReadsIncomplete: entry.ReadsIncomplete,
		}
		for _, read := range entry.Reads {
			// The owner's key names the generating run's store, which no run that reads the table holds.
			read.OwnerKey = nil
			tableEntry.Reads = append(tableEntry.Reads, read)
		}
		for _, slot := range slices.Sorted(maps.Keys(entry.Analyzers)) {
			stored := entry.Analyzers[slot]
			stored.Findings = nil
			tableEntry.Analyzers = append(tableEntry.Analyzers, slotAnalysis{Slot: slot, Analysis: stored})
		}
		encoded, encodeError := encodeStandardLibraryEntry(tableEntry)
		if encodeError != nil {
			return nil, 0, fmt.Errorf("writing the standard library's table: %w", encodeError)
		}
		var renamed []byte
		if collected.ExportData != nil {
			read, readFileSet, readError := exportData.readPackage(collected.PackagePath)
			if readError != nil {
				// Read against an import whose export data does not read, a held entry's damaged one among them: left
				// out, as is whatever reads against it, and computed again by the next run.
				continue
			}
			renamed, renameError = renameExportDataFiles(read, readFileSet, rename)
			if renameError != nil {
				return nil, 0, fmt.Errorf("writing the standard library's table: %w", renameError)
			}
		}
		portable[key] = cache.StandardLibraryEntry{Entry: encoded, ExportData: renamed, Reads: collected.Reads}
		added++
	}
	return cache.WriteStandardLibraryTable(portable, variants), added, nil
}

// standardLibraryEntryWhole reports whether collected, decoded as entry, is as whole as an entry the release's
// generator writes, which every table entry must be, whoever writes it: a table hit is taken as whole, and an entry a
// table holds is never written again. It reads three things.
//
//   - Facts: entry holds an analysis of every analyzer that learns facts in the run that computed it, so a package
//     checked where no fact analyzer's action reached it, as one type-checked only for the types of a package above it
//     the run restored, is left out (#e6wnn8r found debug/pe, debug/buildinfo, hash/maphash and regexp written so).
//   - Answers: they were computed whole (demandOf), every kind complete, rather than those the run's questions
//     happened to compute, which a later run would have to load the package from source to add to.
//   - Reads: the run judged them once its reads were whole (PersistCollectedReads), so a table never takes an entry
//     whose reads a run still had to walk.
func standardLibraryEntryWhole(collected StandardLibraryRunEntry, entry cacheEntry) bool {
	for _, slot := range collected.FactAnalyzers {
		if _, analyzed := entry.Analyzers[slot]; !analyzed {
			return false
		}
	}
	if !collected.AnswersWhole || !collected.ReadsJudged {
		return false
	}
	for _, kind := range entry.Answers {
		if !kind.Complete {
			return false
		}
	}
	return true
}

// CanonicalStandardLibraryTable is the one table that holds every entry and every variant of tables, each written by
// StandardLibraryTableOf, with each entry written again, in the order of the keys, its facts decoded to their values,
// encoded anew and sorted, so the same tables give the same bytes in any fresh process. A fact's gob bytes hold the
// number gob gave its type in the process that encoded it, which is the order the process first met each type: a run
// encodes entries as many at once as there are cores, so the same fact comes out of two runs numbered apart, and the
// same package's entry under two variants would be held twice. The checker hands a package's facts over from a map, in
// no order, so they are sorted too. Written again here, in one order, gob numbers every type alike each time, and an
// entry two variants share by value is held once. Each entry's persisted reads are gob's bytes too, and are decoded and
// encoded again after it, so a record two variants share is held once as well. The caller runs it in a process that has
// encoded nothing with gob before, as the release's generator does in a child of its own. A fact is decoded by its
// type's name, among the fact types of registered's analyzers and everything they require. Two tables that hold the
// same key, or the same variant, are refused, and so is a table that holds nothing or does not parse.
func CanonicalStandardLibraryTable(tables [][]byte, registered []rule.Rule) ([]byte, error) {
	factTypes := factTypesByName(registered)
	merged := map[cache.Key]cache.StandardLibraryEntry{}
	var variants []cache.StandardLibraryVariant
	for index, contents := range tables {
		table := cache.NewStandardLibraryTable(contents)
		if problem := table.Problem(); problem != nil {
			return nil, fmt.Errorf("writing the canonical table: table %d: %w", index+1, problem)
		}
		if table.Empty() || len(table.Keys()) == 0 {
			return nil, fmt.Errorf("writing the canonical table: table %d holds nothing", index+1)
		}
		for _, variant := range table.Variants() {
			if slices.Contains(variants, variant) {
				return nil, fmt.Errorf("writing the canonical table: two tables hold %s %s with cgo %s",
					variant.GoVersion, variant.Target, variant.CgoEnabled)
			}
			variants = append(variants, variant)
		}
		for _, key := range table.Keys() {
			if _, held := merged[key]; held {
				return nil, fmt.Errorf("writing the canonical table: two tables hold the key %s", key)
			}
			// Ignored: the key is one of the table's own.
			merged[key], _ = table.Lookup(key)
		}
	}
	canonical := make(map[cache.Key]cache.StandardLibraryEntry, len(merged))
	for _, key := range slices.SortedFunc(maps.Keys(merged), compareCacheKeys) {
		entry, decodeError := decodeStandardLibraryEntry(merged[key].Entry)
		if decodeError != nil {
			return nil, fmt.Errorf("writing the canonical table: %w", decodeError)
		}
		for analysisIndex := range entry.Analyzers {
			stored := &entry.Analyzers[analysisIndex].Analysis
			for factIndex := range stored.ObjectFacts {
				fact, factError := reencodeFact(factTypes, stored.ObjectFacts[factIndex].Fact)
				if factError != nil {
					return nil, fmt.Errorf("writing the canonical table: the entry of %s: %w", entry.PackagePath,
						factError)
				}
				stored.ObjectFacts[factIndex].Fact = fact
			}
			for factIndex := range stored.PackageFacts {
				fact, factError := reencodeFact(factTypes, stored.PackageFacts[factIndex])
				if factError != nil {
					return nil, fmt.Errorf("writing the canonical table: the entry of %s: %w", entry.PackagePath,
						factError)
				}
				stored.PackageFacts[factIndex] = fact
			}
			// The checker hands a package's facts over from a map, in no order.
			slices.SortFunc(stored.ObjectFacts, func(left storedObjectFact, right storedObjectFact) int {
				return cmp.Or(cmp.Compare(left.Path, right.Path), compareStoredFacts(left.Fact, right.Fact))
			})
			slices.SortFunc(stored.PackageFacts, compareStoredFacts)
		}
		// Each answer is JSON, the same bytes in any process, so sorting is all they need.
		sortAnswers(entry.Answers)
		encoded, encodeError := encodeStandardLibraryEntry(entry)
		if encodeError != nil {
			return nil, fmt.Errorf("writing the canonical table: %w", encodeError)
		}
		var reads []byte
		if merged[key].Reads != nil {
			record, readsError := decodePersistedReads(merged[key].Reads)
			if readsError == nil {
				reads, readsError = encodePersistedReads(record)
			}
			if readsError != nil {
				return nil, fmt.Errorf("writing the canonical table: the entry of %s: %w", entry.PackagePath,
					readsError)
			}
		}
		canonical[key] = cache.StandardLibraryEntry{Entry: encoded, ExportData: merged[key].ExportData, Reads: reads}
	}
	return cache.WriteStandardLibraryTable(canonical, variants), nil
}

// compareStoredFacts orders two stored facts by their type's name, then by their bytes.
func compareStoredFacts(left storedFact, right storedFact) int {
	return cmp.Or(cmp.Compare(left.Type, right.Type), bytes.Compare(left.Encoded, right.Encoded))
}

// factTypesByName is every fact type of registered's analyzers and everything they require, by factTypeName.
func factTypesByName(registered []rule.Rule) map[string]reflect.Type {
	byName := map[string]reflect.Type{}
	seen := map[*analysis.Analyzer]bool{}
	var visit func(analyzer *analysis.Analyzer)
	visit = func(analyzer *analysis.Analyzer) {
		if seen[analyzer] {
			return
		}
		seen[analyzer] = true
		for _, fact := range analyzer.FactTypes {
			byName[factTypeName(reflect.TypeOf(fact))] = reflect.TypeOf(fact)
		}
		for _, required := range analyzer.Requires {
			visit(required)
		}
	}
	for _, registeredRule := range registered {
		visit(registeredRule.Analyzer)
	}
	return byName
}

// reencodeFact is stored decoded to a value of the fact type its name names among factTypes and encoded again in
// this process, or an error for a type none names or bytes gob refuses.
func reencodeFact(factTypes map[string]reflect.Type, stored storedFact) (storedFact, error) {
	factType, known := factTypes[stored.Type]
	if !known {
		return storedFact{}, fmt.Errorf("no analyzer learns a fact of type %s", stored.Type)
	}
	fact, isFact := reflect.New(factType.Elem()).Interface().(analysis.Fact)
	if !isFact {
		return storedFact{}, fmt.Errorf("a new %s is not a fact", stored.Type)
	}
	if decodeError := gob.NewDecoder(bytes.NewReader(stored.Encoded)).Decode(fact); decodeError != nil {
		return storedFact{}, fmt.Errorf("decoding a %s fact: %w", stored.Type, decodeError)
	}
	return encodeFact(fact)
}

// StandardLibraryDigests are what the release's check compares of a table's entry with one it recomputed: the
// package's import path, the digest of its API by value (see exportDigest), read from its export data, its output
// digest (see outputDigest), nil when the entry holds none, and the digest of its persisted reads by value, encoded
// again in this process, the zero key when it holds none. Each is by value, so two processes that computed the same
// package alike give the same digests, while their bytes may differ.
type StandardLibraryDigests struct {
	PackagePath string
	Export      cache.Key
	Output      []byte
	Reads       cache.Key
}

// StandardLibraryDigestsOf is the digests of one of a table's entries, its export data read against among, the
// entries of its variant (StandardLibraryExportDataOf), which hold every package it reaches, or an error for an entry
// that does not decode, or whose export data does not read; unsafe, which has none, has the API of the type checker's
// own.
//
// Mutates: among reads each package it has not read that the entry's imports reach
func StandardLibraryDigestsOf(
	entry cache.StandardLibraryEntry,
	among *StandardLibraryExportData,
) (StandardLibraryDigests, error) {
	decoded, decodeError := decodeStandardLibraryEntry(entry.Entry)
	if decodeError != nil {
		return StandardLibraryDigests{}, decodeError
	}
	digests := StandardLibraryDigests{PackagePath: decoded.PackagePath, Output: decoded.Output}
	if entry.Reads != nil {
		record, readsError := decodePersistedReads(entry.Reads)
		if readsError != nil {
			return StandardLibraryDigests{}, fmt.Errorf("reading the table's entry of %s: %w", decoded.PackagePath,
				readsError)
		}
		// Encoded again here, gob numbers its types as this process does, the same for two records of one value.
		encoded, encodeError := encodePersistedReads(record)
		if encodeError != nil {
			return StandardLibraryDigests{}, encodeError
		}
		digests.Reads = cache.DigestOf(encoded)
	}
	if decoded.PackagePath == "unsafe" {
		digests.Export = exportDigest(types.Unsafe)
		return digests, nil
	}
	if entry.ExportData == nil {
		return StandardLibraryDigests{}, fmt.Errorf("reading the table's entry of %s: it holds no export data",
			decoded.PackagePath)
	}
	read, readError := among.readAgainst(token.NewFileSet(), decoded.PackagePath, decoded.Imports, entry.ExportData)
	if readError != nil {
		return StandardLibraryDigests{}, fmt.Errorf("reading the export data of %s: %w", decoded.PackagePath,
			readError)
	}
	digests.Export = exportDigest(read)
	return digests, nil
}

// compareCacheKeys orders two keys by their bytes.
func compareCacheKeys(left cache.Key, right cache.Key) int {
	return bytes.Compare(left[:], right[:])
}
