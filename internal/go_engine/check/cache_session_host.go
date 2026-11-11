package check

import (
	"errors"
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// Restoring a package from the host's run in an extra build (#b2t9xmf). The host's run keeps, for each package it
// analyzed from source whose reads can hold, the entry the findings cache writes for it (keepForBuilds). An extra
// build's session restores each package its own store does not hold whose own inputs are the host's (the portable
// digest) and whose reads hold there, in two halves: what it read of other packages' types is decided by the load,
// once the package's imports are done and before anything is read (TypesHold through load.Restored.Holds); what it
// read of their facts as each action on it is restored (FactsHold), and the answers it read once the analysis is
// over (readsHold). A package restored by the load whose facts or answers do not hold has no syntax to analyze, so
// the build is loaded again with it, and every package above it, never restored from the host again.

// HostRestoreCounts is what an extra build restored from the host's run: the packages restored, by ID, sorted, how many
// the load declined (their types' reads did not hold, or their export data did not read) and check declined (their
// facts or answers did not hold), how many times the build was loaded again for those, and every decline and every
// package never a candidate by the first word of why: inputs, noEntry, never, unknown, export, T, L, F, D or A.
// Declined is why each package was declined, by ID, the last load's and check's over every load. Deciding is the time
// TypesHold and FactsHold took over every load, summed across the goroutines that asked them, so it can exceed the
// wall time it ran within.
type HostRestoreCounts struct {
	Restored        []string
	DeclinedAtLoad  int
	DeclinedAtCheck int
	ReloadRounds    int
	DeclinedBy      map[string]int
	Declined        map[string]string
	Deciding        time.Duration
}

// hostRestore is one package an extra build restores from the host's run, and what deciding it has learned: the first
// reason it was declined, and whether its facts and answers were decided.
type hostRestore struct {
	kept     *hostEntry
	restored *restoredPackage

	lock     sync.Mutex
	declined string
	settled  bool
}

// hostState is a session's restore from the host's run, nil for a session that restores nothing from it.
type hostState struct {
	host *HostRun

	// lock guards the session's hits, trusted and restoring while the load declines packages from its goroutines. A
	// verdict only reads restoring, under the read lock, so the verdicts deciding a load at once never queue on each
	// other, only on a decline.
	lock sync.RWMutex

	// deciding is the time TypesHold and FactsHold took over every load, in nanoseconds, summed across goroutines.
	deciding atomic.Int64

	// restoring are this load's packages restored from the host's run, and renderings the verdicts' memo of this load's
	// types.
	restoring  map[*packages.Package]*hostRestore
	renderings *ReadsBuild

	// excluded are the IDs never restored from the host again: each package check declined, and every package above
	// it, whose types the load judged against the restored ones. They outlive a load, as the reloads do.
	excluded map[string]bool

	// rounds counts the loads check's declines asked for; past one, the session restores nothing more from the host.
	rounds int
	off    bool

	// loadDeclines are the load's declines and the packages it never made candidates, by the first word of why, and
	// loadReasons why the load declined each, by ID, for this load alone; checkDeclines and checkReasons are check's,
	// over every load of the run.
	loadDeclines  map[string]int
	loadReasons   map[string]string
	checkDeclines map[string]int
	checkReasons  map[string]string
}

// KeepForBuilds has the session, the host's of a run with extra builds, key every load it restores for, with a store
// or without, so the run can keep its packages' entries for the extra builds (Options.KeepForBuilds).
func (session *CacheSession) KeepForBuilds() {
	session.keepsForBuilds = true
}

// RestoreFromHost has the session restore from host, the host's run, each package its own store does not hold whose
// own inputs are the host's and whose reads hold in this build.
func (session *CacheSession) RestoreFromHost(host *HostRun) {
	if host == nil {
		session.host = nil
		return
	}
	session.host = &hostState{host: host, excluded: map[string]bool{}, checkDeclines: map[string]int{},
		checkReasons: map[string]string{}}
}

// declineKeys spell out, for the record people read, the first word of each reason a package was not restored from the
// host: a reason starts with a short tag so a test can match it, but a reader of the record took `T` for a leaked type
// parameter.
var declineKeys = map[string]string{
	"T":       "typeReads",
	"L":       "lookups",
	"F":       "factReads",
	"D":       "deprecations",
	"A":       "answers",
	"never":   "neverRestorable",
	"unknown": "readsUnknown",
	"export":  "exportData",
	"inputs":  "ownInputs",
	"noEntry": "noHostEntry",
}

// reasonWord is why a package was not restored from the host, as the record counts it: the reason's first word,
// spelled out (declineKeys), or the word itself for one the table does not name, so a new reason is still counted.
func reasonWord(reason string) string {
	word, _, _ := strings.Cut(reason, " ")
	word = strings.TrimSuffix(word, ":")
	if spelled, named := declineKeys[word]; named {
		return spelled
	}
	return word
}

// restoreFromHost makes a hit of each package among reachable the session holds nothing of, with no list error and no
// reload pending, whose own inputs, by the portable digest, are those of the package of the same ID the host's run
// kept, and whose reads can hold, decoded from the host's entry. Each is decided late by the load (load.Restored.Holds)
// and dropped from the hits when it declines. keyed is the build's keys, nil for a session without a store, which keys
// reachable here for the portable digests alone.
func (session *CacheSession) restoreFromHost(loader string, reachable []*packages.Package, keyed *cache.Keyed) {
	state := session.host
	if state == nil || state.off || session.known == nil {
		return
	}
	state.restoring = map[*packages.Package]*hostRestore{}
	state.renderings = NewReadsBuild()
	state.loadDeclines = map[string]int{}
	state.loadReasons = map[string]string{}
	var portable map[*packages.Package]cache.Key
	if keyed != nil {
		portable = keyed.Portable
	} else {
		keyContext := session.caching.Context
		keyContext.Build = append(slices.Clone(keyContext.Build), "loader="+loader)
		ownKeys, keysError := cache.Keys(reachable, keyContext, session.caching.ExtraInputs, state.host.files,
			session.caching.Held)
		if keysError != nil {
			session.problem(fmt.Sprintf("computing the portable digests to restore from the host: %v", keysError))
			return
		}
		portable = ownKeys.Portable
	}
	var reading []*packages.Package
	var readingKept []*hostEntry
	for _, candidate := range reachable {
		if _, hit := session.hits[candidate]; hit || len(candidate.Errors) > 0 || session.reloading(candidate) ||
			state.excluded[candidate.ID] {
			continue
		}
		kept := state.host.entries[candidate.ID]
		switch {
		case kept == nil:
			state.loadDeclines[declineKeys["noEntry"]]++
			continue
		case kept.portable != portable[candidate]:
			state.loadDeclines[declineKeys["inputs"]]++
			continue
		}
		if restorable, why := kept.restoring.Reads.Restorable(); !restorable {
			if kept.restoring.Reads.Never() != "" {
				why = "never: " + why
			} else {
				why = "unknown: " + why
			}
			state.loadDeclines[reasonWord(why)]++
			continue
		}
		reading = append(reading, candidate)
		readingKept = append(readingKept, kept)
	}
	// The entries are read, and decoded, on every core at once, as readHits reads the store's, and made hits in
	// reachable's order after. Their export data is written only when the load reads it, in the package's own turn
	// (lazyExportData).
	read := make([]*restoredPackage, len(reading))
	readErrors := make([]error, len(reading))
	bounded.ForEach(len(reading), false, func(index int) {
		entry, readExportData, readError := readingKept[index].contents()
		if readError == nil {
			read[index], readError = decodeEntry(session.known, reading[index], entry, readExportData)
		}
		readErrors[index] = readError
	})
	for index, candidate := range reading {
		kept, restored, readError := readingKept[index], read[index], readErrors[index]
		if readError != nil {
			session.problem(fmt.Sprintf("restoring %s from the host: %v", candidate.ID, readError))
			state.loadDeclines[declineKeys["noEntry"]]++
			continue
		}
		hosted := &hostRestore{kept: kept, restored: restored}
		restored.types.Holds = session.typesHoldFunction(candidate, hosted)
		restored.types.Declined = func() { session.declineAtLoad(candidate, hosted) }
		session.hits[candidate] = restored
		session.trusted[candidate] = true
		state.restoring[candidate] = hosted
	}
}

// typesHoldFunction is the load's late decision of hosted, candidate's restore (load.Restored.Holds): its types' reads
// judged against the packages it reaches in this build, and those of them restored from the host's run. Why it
// declined is kept for the decline to count.
//
// Mutates: hosted keeps why its types' reads did not hold, which the decline that follows counts
func (session *CacheSession) typesHoldFunction(
	candidate *packages.Package,
	hosted *hostRestore,
) func(map[string]*types.Package) bool {
	return func(closure map[string]*types.Package) bool {
		// The load set the package's sizes, the build's, before deciding any package, so the first verdict tells
		// the memo its build before it renders anything.
		session.host.renderings.measureBy(candidate.TypesSizes)
		started := time.Now()
		holds, why := TypesHold(hosted.kept.restoring, closure, session.restoredBelow(candidate, closure),
			session.host.host.renderings, session.host.renderings)
		session.host.deciding.Add(int64(time.Since(started)))
		if !holds {
			hosted.lock.Lock()
			hosted.declined = why
			hosted.lock.Unlock()
		}
		return holds
	}
}

// restoredBelow are the types of the packages of closure, what candidate reaches by path, that this load restored from
// the host's run. Each is done when candidate's restore is decided, its types set and its decline, if any, counted
// (declineAtLoad), so a package still among the restores is one whose types were read from the host's export data.
func (session *CacheSession) restoredBelow(
	candidate *packages.Package,
	closure map[string]*types.Package,
) map[*types.Package]bool {
	// The load set every package's imports before it decided any, so the walk reads them without the lock; only
	// whether each reached package is still among the restores is asked under it.
	visited := map[*packages.Package]bool{}
	var reached []*packages.Package
	var collect func(importer *packages.Package)
	collect = func(importer *packages.Package) {
		for _, imported := range importer.Imports {
			if visited[imported] {
				continue
			}
			visited[imported] = true
			reached = append(reached, imported)
			collect(imported)
		}
	}
	collect(candidate)
	state := session.host
	restored := map[*types.Package]bool{}
	state.lock.RLock()
	defer state.lock.RUnlock()
	for _, imported := range reached {
		if _, fromHost := state.restoring[imported]; fromHost && imported.Types != nil &&
			closure[imported.PkgPath] == imported.Types {
			restored[imported.Types] = true
		}
	}
	return restored
}

// declineAtLoad drops candidate, which the load checks from source after all, from the hits restored from the host,
// counting why: its late decision's reason, or its export data, which did not read.
func (session *CacheSession) declineAtLoad(candidate *packages.Package, hosted *hostRestore) {
	hosted.lock.Lock()
	why := hosted.declined
	hosted.lock.Unlock()
	if why == "" {
		why = "export data that did not read"
	}
	state := session.host
	state.lock.Lock()
	defer state.lock.Unlock()
	delete(session.hits, candidate)
	delete(session.trusted, candidate)
	delete(state.restoring, candidate)
	state.loadDeclines[reasonWord(why)]++
	state.loadReasons[candidate.ID] = why
}

// dropShortHostHits forgets the packages restored from the host that dropShortHits made misses.
func (session *CacheSession) dropShortHostHits() {
	if session.host == nil {
		return
	}
	for candidate := range session.host.restoring {
		if _, hit := session.hits[candidate]; !hit {
			delete(session.host.restoring, candidate)
			delete(session.trusted, candidate)
		}
	}
}

// fromHost is the package's restore from the host's run, nil for any other package.
func (session *CacheSession) fromHost(restoring *packages.Package) *hostRestore {
	if session.host == nil {
		return nil
	}
	return session.host.restoring[restoring]
}

// decideHostRestores settles every package restored from the host's run once the analysis of graphs is over, with no
// pass running: one whose fact reads, deprecation lookups included, do not read the same in what the build's packages
// exported (buildFacts), whose answers read of other packages could not all be named, or one of which no longer holds
// (readsHold), is declined. It returns the IDs declined, sorted, which the run asks to load again, and excludes each
// of them, and every package above one, from the host's restores from then on: the load judged those above against
// the declined package's restored types, and their analysis read its restored facts. A second load's worth of declines
// turns the restore from the host off, so the build loads again at most twice for it.
func (session *CacheSession) decideHostRestores(graphs []*checker.Graph) []string {
	state := session.host
	if state == nil || len(state.restoring) == 0 {
		return nil
	}
	exported := newBuildFacts(session, graphs)
	var declined []*packages.Package
	for candidate, hosted := range state.restoring {
		hosted.lock.Lock()
		why := hosted.declined
		hosted.lock.Unlock()
		if why == "" {
			why = exported.factsHold(candidate, hosted.kept.restoring, state)
		}
		if why == "" && (hosted.restored.readsIncomplete || !session.readsHold(candidate)) {
			why = "A an answer it read of another package differs in the build, or cannot be named"
		}
		hosted.lock.Lock()
		hosted.declined, hosted.settled = why, true
		hosted.lock.Unlock()
		if why != "" {
			declined = append(declined, candidate)
			state.checkDeclines[reasonWord(why)]++
			state.checkReasons[candidate.ID] = why
		}
	}
	if len(declined) == 0 {
		return nil
	}
	state.rounds++
	if state.rounds > 1 {
		state.off = true
	}
	identifiers := make([]string, 0, len(declined))
	for _, candidate := range declined {
		identifiers = append(identifiers, candidate.ID)
	}
	for above := range session.packagesAbove(declined) {
		state.excluded[above.ID] = true
	}
	slices.Sort(identifiers)
	return identifiers
}

// packagesAbove are the packages the load reached that are among declined or import one of them, directly or not.
func (session *CacheSession) packagesAbove(declined []*packages.Package) map[*packages.Package]bool {
	above := map[*packages.Package]bool{}
	for _, candidate := range declined {
		above[candidate] = true
	}
	decided := map[*packages.Package]bool{}
	var reaches func(current *packages.Package) bool
	reaches = func(current *packages.Package) bool {
		if decided[current] {
			return above[current]
		}
		decided[current] = true
		for _, imported := range current.Imports {
			if reaches(imported) {
				above[current] = true
			}
		}
		return above[current]
	}
	for _, each := range session.byIdentifier {
		reaches(each)
	}
	return above
}

// unsettledBelow are the packages the load reached above a package restored from the host whose restore is not
// settled yet, or was declined: an analysis of one of them read what the restore gave, so no entry of it may be
// written until the restore holds (see saveMisses).
func (session *CacheSession) unsettledBelow() map[*packages.Package]bool {
	if session.host == nil || len(session.host.restoring) == 0 {
		return nil
	}
	var unsettled []*packages.Package
	for candidate, hosted := range session.host.restoring {
		hosted.lock.Lock()
		if !hosted.settled || hosted.declined != "" {
			unsettled = append(unsettled, candidate)
		}
		hosted.lock.Unlock()
	}
	if len(unsettled) == 0 {
		return nil
	}
	return session.packagesAbove(unsettled)
}

// hostCounts is what the session restored from the host's run over its last load, and what check declined over every
// load of the run.
func (session *CacheSession) hostCounts() HostRestoreCounts {
	state := session.host
	if state == nil {
		return HostRestoreCounts{}
	}
	counts := HostRestoreCounts{
		DeclinedAtLoad:  len(state.loadReasons),
		DeclinedAtCheck: len(state.checkReasons),
		ReloadRounds:    state.rounds,
		DeclinedBy:      map[string]int{},
		Declined:        map[string]string{},
		Deciding:        time.Duration(state.deciding.Load()),
	}
	for candidate, hosted := range state.restoring {
		hosted.lock.Lock()
		if hosted.declined == "" {
			counts.Restored = append(counts.Restored, candidate.ID)
		}
		hosted.lock.Unlock()
	}
	slices.Sort(counts.Restored)
	for _, reasons := range []map[string]string{state.loadReasons, state.checkReasons} {
		maps.Copy(counts.Declined, reasons)
	}
	for _, words := range []map[string]int{state.loadDeclines, state.checkDeclines} {
		for word, count := range words {
			counts.DeclinedBy[word] += count
		}
	}
	return counts
}

// keepForBuilds is what the host's run keeps of each package it analyzed from source, among analyzed and what they
// reach, whose reads can hold, for its extra builds to restore from (#b2t9xmf): the package's entry, read back from
// the store when the run wrote one there, or else encoded here, as saveMisses encodes it, and held in memory, which a
// run under `--no-cache` never stores. Each package's portable digest is the one Restore keyed it by, before the load
// gave anything its syntax (KeepForBuilds). Only a run with extra builds asks it, so a run without pays nothing.
func (session *CacheSession) keepForBuilds(
	graphs []*checker.Graph,
	analyzed []*packages.Package,
	reads map[*packages.Package]*build_reads.Reads,
	withheld map[*packages.Package]load.PackageWithheld,
	files map[*packages.Package][]storedFile,
) *HostRun {
	keyed := session.keptKeys
	if session.known == nil || keyed == nil {
		return nil
	}
	started := time.Now()
	// The reads an entry holds name each owner by its ID and its key in this run (readsOf), so the session knows every
	// package now, which a run beside the load knew only as each was restored; a run without a store keys here.
	reachable := make([]*packages.Package, 0, len(keyed.Keys))
	for keyedPackage := range keyed.Keys {
		reachable = append(reachable, keyedPackage)
	}
	session.know(reachable...)
	if session.keys == nil {
		session.buildKeys = keyed.Keys
	}
	actionsOf := map[*packages.Package][]*checker.Action{}
	for _, graph := range graphs {
		if graph == nil {
			continue
		}
		for action := range graph.All() {
			actionsOf[action.Package] = append(actionsOf[action.Package], action)
		}
	}
	host := &HostRun{entries: map[string]*hostEntry{}, renderings: NewReadsBuild(), files: keyed.Files}
	// Every package of one load has the load's sizes, the host's build's.
	for keyedPackage := range keyed.Keys {
		if keyedPackage.TypesSizes != nil {
			host.renderings.measureBy(keyedPackage.TypesSizes)
			break
		}
	}
	var encoding []*packages.Package
	for keptPackage := range keyed.Portable {
		packageReads := reads[keptPackage]
		if packageReads == nil || packageReads.Never() != "" || session.restored(keptPackage) {
			continue
		}
		kept := &hostEntry{restoring: restoringOf(keptPackage, packageReads), portable: keyed.Portable[keptPackage]}
		if session.written[keptPackage] {
			key := session.keys[keptPackage]
			kept.contents = func() ([]byte, func() ([]byte, error), error) {
				entry, stored := session.caching.Store.Get(key)
				if !stored {
					return nil, nil, fmt.Errorf("the store holds no entry of %s", keptPackage.ID)
				}
				return entry, func() ([]byte, error) {
					exportData, _ := session.caching.Store.Get(exportDataKey(key))
					return exportData, nil
				}, nil
			}
			host.entries[keptPackage.ID] = kept
			continue
		}
		unsure := session.resolver != nil && keptPackage.Types != nil && session.resolver.Unsure(keptPackage.Types)
		if session.caching.Store == nil && len(keptPackage.Errors) == 0 && !keptPackage.IllTyped &&
			len(actionsOf[keptPackage]) > 0 && finishedClean(actionsOf[keptPackage]) && !unsure {
			encoding = append(encoding, keptPackage)
			host.entries[keptPackage.ID] = kept
		}
	}
	// Each entry is encoded here, on every core, without its export data, which is most of its cost and is written
	// only when a load first reads it (lazyExportData). What it is written from is the package's types alone, so the
	// host's syntax, type maps, intermediate representation and actions all go once the keep returns, as they did
	// when the keep wrote everything: holding them for a later write marked about 4 GB on every collection of adamic's
	// builds. The types live until the last build has run (HostRun.Release).
	knownFiles := session.allFiles()
	encoded := make([]encodedEntry, len(encoding))
	encodeErrors := make([]error, len(encoding))
	bounded.ForEach(len(encoding), false, func(index int) {
		saved := encoding[index]
		encoded[index], encodeErrors[index] = encodeEntry(session.known, session.ruleOf, saved, actionsOf[saved],
			withheld[saved], session.recordedDemandOf(saved), files[saved], knownFiles, false)
	})
	for index, saved := range encoding {
		if encodeErrors[index] != nil {
			if !errors.Is(encodeErrors[index], errorUnstorable) {
				session.problem(encodeErrors[index].Error())
			}
			delete(host.entries, saved.ID)
			continue
		}
		entry := encoded[index].entry
		readExportData := lazyExportData(saved)
		host.entries[saved.ID].contents = func() ([]byte, func() ([]byte, error), error) {
			return entry, readExportData, nil
		}
	}
	host.kept = len(host.entries)
	// What the analysis built to answer questions, every package's intermediate representation, syntax and type maps,
	// is the session's to keep only for its next save; a run under `--no-cache` has none, and the extra builds that run
	// next read only the entries, so it goes now rather than outlive the host's pass through every build (#b2t9xmf).
	if session.caching.Store == nil {
		session.resolver = nil
		session.answering.lock.Lock()
		session.answering.byTypes = nil
		session.answering.lock.Unlock()
	}
	session.keeping += time.Since(started)
	host.keeping = session.keeping
	return host
}

// lazyExportData is the reader of kept's export data, written the first time a load reads it and held from then on,
// so two loads reading at once write it once, and a package no load reads, because no build restores it, never has
// it written. The reader holds only what writeExportData reads of kept, its identity, types and file set, never kept
// itself, whose syntax and type maps would otherwise live as long as the reader. Export data that cannot be written
// is the load's to decline, as export data that did not read.
func lazyExportData(kept *packages.Package) func() ([]byte, error) {
	slim := &packages.Package{ID: kept.ID, PkgPath: kept.PkgPath, Types: kept.Types, Fset: kept.Fset}
	return sync.OnceValues(func() ([]byte, error) { return writeExportData(slim) })
}

// recordedDemandOf is what an entry kept for the extra builds holds of the fact kinds computed on demand: the answers
// the run's analysis computed in saved, not every answer of it, which would build the whole of every package's
// intermediate representation for answers the builds mostly never ask, and the reads its analysis made of other
// packages. An extra build that asks one it does not hold reloads the package, as a run restoring a partial entry does
// (see Reload).
func (session *CacheSession) recordedDemandOf(saved *packages.Package) entryDemand {
	if session.resolver == nil || saved.Types == nil {
		return entryDemand{}
	}
	demanded := entryDemand{answers: sortedAnswers(session.resolver.Recorded(saved.Types), false)}
	demanded.reads, demanded.readsIncomplete = session.readsOf(saved, entryDemand{})
	return demanded
}
