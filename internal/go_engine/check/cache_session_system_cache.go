package check

import (
	"encoding/json"
	"flag"
	"fmt"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
)

// keyStandardLibrary keys every standard-library package among reachable (see cache.StandardLibraryKeys), reading the
// digest of each file hashed holds rather than hashing it again, under the build's context with its fact analyzers and
// loader named, as the system cache's store holds a standard-library package's entry (storeFor) and the release's
// table its own. Keys that cannot be computed are a problem, and every standard-library package is then keyed as any
// other package is, or not at all in a run without the project's store.
func (session *CacheSession) keyStandardLibrary(
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
	session.standardLibraryByPath = standardLibraryPackagesByPath(keys)
	session.standardLibraryContext = &shared
}

// StandardLibraryContext is what every standard-library key of the last Restore shared, its fact analyzers named, and
// false before a Restore keyed the standard library or when it could not.
func (session *CacheSession) StandardLibraryContext() (cache.StandardLibraryContext, bool) {
	if session.standardLibraryContext == nil {
		return cache.StandardLibraryContext{}, false
	}
	return *session.standardLibraryContext, true
}

// PersistCollectedReads gives each standard-library package the run collected an entry of (collect) its reads of
// other packages, reads, persisted by a memo measured by the run's sizes, its target's, as the release's table carries
// them beside each entry (#pcv6eff). The caller hands the reads once every type-read walk has ended, nil for a run
// that records none. Each package whose reads the run recorded is judged (ReadsJudged), which the table asks of each
// entry it takes: persisted when they can be, and kept with none when they cannot, as when marked never, which a build
// declines as unpersisted. One whose reads the run never recorded stays unjudged, so the table never takes it.
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

// collect adds a standard-library package the run just encoded, with demanded, the answers and reads its entry holds,
// to the entries the run collects for the release's table, when it collects any (see StandardLibraryCaching's
// Collected). It records what standardLibraryEntryWhole later judges the entry by: the slots of the run's fact
// analyzers, each of which a whole entry holds an analysis of, since every action on a package leaves its analysis in
// the entry, facts or none, so the entry says which ran and the session says which had to; and whether its answers
// were computed whole. An entry short of what the table takes (wholeForTable) is not collected. The caller holds the
// lock the save counts under.
func (session *CacheSession) collect(
	saved *packages.Package,
	entry encodedEntry,
	demanded entryDemand,
	actions []*checker.Action,
) {
	standardLibrary := session.caching.StandardLibrary
	if standardLibrary == nil || standardLibrary.Collected == nil {
		return
	}
	key, standard := session.standardLibraryKeys[saved]
	if !standard {
		return
	}
	if !session.wholeForTable(actions, demanded) {
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
	standardLibrary.Collected[key] = StandardLibraryRunEntry{
		PackagePath:   saved.PkgPath,
		Entry:         whole,
		ExportData:    entry.exportData,
		FactAnalyzers: factAnalyzers,
		AnswersWhole:  session.answersWholeForTable(demanded),
	}
}

// wholeForTable reports whether an entry of a package the run computed with actions, holding demanded, has the facts
// and answers standardLibraryEntryWhole takes: an action of every fact analyzer (factsForTable), and answers computed
// whole, every kind complete. Its reads are judged once they are whole (PersistCollectedReads), and the write judges
// the entry again whole. An entry short of these is left out before it is collected, or encoded when nothing else
// takes it, so a run never collects an entry only for the table to refuse it.
func (session *CacheSession) wholeForTable(actions []*checker.Action, demanded entryDemand) bool {
	if !session.factsForTable(actions) || !session.answersWholeForTable(demanded) {
		return false
	}
	for _, kind := range demanded.answers {
		if !kind.Complete {
			return false
		}
	}
	return true
}

// factsForTable reports whether actions, a package's, hold an action of every fact analyzer, each of which leaves its
// analysis in the package's entry (encodeEntry), as a table entry holds an analysis of each.
func (session *CacheSession) factsForTable(actions []*checker.Action) bool {
	ran := make(map[*analysis.Analyzer]bool, len(actions))
	for _, action := range actions {
		ran[action.Analyzer] = true
	}
	for _, analyzer := range session.known.facts {
		if !ran[analyzer] {
			return false
		}
	}
	return true
}

// answersWholeForTable reports whether demanded's answers are every answer of its package, computed whole, as a table
// entry's are. The resolver learns a package as ctrlflow's action on it ends, and answers a question only of a package
// it learned (fact_resolver's Owner), so a rule that asks a fact on demand runs ctrlflow. In a run with no ctrlflow
// nothing is ever asked, and neither is anything in a run that reads this table, since its key holds the fact
// analyzers, ctrlflow among them whenever a rule asks: the answers it holds, none, are all there will ever be.
func (session *CacheSession) answersWholeForTable(demanded entryDemand) bool {
	return demanded.answersWhole || !slices.Contains(session.known.facts, ctrlflow.Analyzer)
}

// HasCollected reports whether the session collected any standard-library entry for the release's table, whose reads
// the run then persists (PersistCollectedReads), which a run that collected none spares itself: the host's reads are
// whole only once its type-read walks end, which nothing else waits for.
func (session *CacheSession) HasCollected() bool {
	standardLibrary := session.caching.StandardLibrary
	return standardLibrary != nil && len(standardLibrary.Collected) > 0
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
