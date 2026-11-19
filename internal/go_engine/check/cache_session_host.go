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
// digest) and whose reads hold there, in two halves: what it read of other packages' types is decided by the load, once
// the package's imports are done and before anything is read (TypesHold through load.Restored.Holds); what it read of
// their facts as each action on it is restored (FactsHold), and the answers it read once the analysis is over
// (readsHold). A package restored by the load whose facts or answers do not hold has no syntax to analyze, so the build
// is loaded again with it checked from source, never restored from the host again, and every package above it decided
// again in the reload, against it as checked there (decideHostRestores). A package whose restore held is written
// through to the build's own store under its own key (writeThrough), so the build's next run hits it there, even when
// the host, hitting its own store, keeps nothing to restore it from.

// HostRestoreCounts is what an extra build restored from the host's run: the packages the walk left untouched, by ID,
// sorted, and how many it visited, among the ones the host kept, over the last load (untouchedOf), the packages
// restored, by ID, sorted, how many
// the load declined (their types' reads did not hold, or their export data did not read) and check declined (their
// facts or answers did not hold), how many times the build was loaded again for those, and every decline and every
// package never a candidate by the first word of why: inputs, noEntry, sizes, never, unknown, noReads, export, T, L, F,
// D or A.
// Declined is why each package was declined, by ID, the last load's and check's over every load. Deciding is the time
// TypesHold and FactsHold took over every load, summed across the goroutines that asked them, so it can exceed the
// wall time it ran within.
type HostRestoreCounts struct {
	Untouched       []string
	Visited         int
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

	// restoring are this load's packages restored from the host's run and decided late, and renderings the verdicts'
	// memo of this load's types. untouched are this load's packages restored from the host's run that the walk left
	// untouched, never judged (untouchedOf), and visited counts the packages the host kept that the walk visited, each
	// judged, held or declined.
	restoring  map[*packages.Package]*hostRestore
	renderings *ReadsBuild
	untouched  map[*packages.Package]*hostRestore
	visited    int

	// visitsEvery has the walk visit every package the host kept, leaving none untouched (VisitEvery).
	visitsEvery bool

	// judgesSkipped, set only by a test (JudgeSkipped), is told each package the build skipped without judging as the
	// plain path judges a visited one, by which judgment and with why it declines, and skipsTold each such package as
	// a load skips it, once a load, so the test counts every load's skips; both nil in every run.
	judgesSkipped func(identifier string, judgment SkippedJudgment, why string)
	skipsTold     func(identifier string)

	// writtenThrough are this load's packages written through to the build's own store (writeThrough), each once: an
	// untouched package is settled from the start, so the first wave's save would write it, and every later one again.
	writtenThrough map[*packages.Package]bool

	// excluded are the IDs never restored from the host again: each package check declined, which the reload checks
	// from source. They outlive a load, as the reloads do.
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
// or without, so the run can keep its packages' entries for the extra builds (Options.KeepForBuilds), and keep every
// body it reads from its store decoded, for the builds whose records name the same body (decodedBodies, #qqyq61s).
func (session *CacheSession) KeepForBuilds() {
	session.keepsForBuilds = true
	session.bodies = newDecodedBodies(session.known)
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

// VisitEvery has the session's restore from the host's run visit and judge every package it restores, leaving none
// untouched, as before the walk (#qbvtkfm), so a run can be compared with the walk's. It holds only after
// RestoreFromHost.
func (session *CacheSession) VisitEvery() {
	if session.host != nil {
		session.host.visitsEvery = true
	}
}

// SkippedJudgment names how JudgeSkipped judged a package the build skipped: by TypesHold, or by unrestorableReads
// alone, for reads no verdict can judge, which the plain path declines before TypesHold is asked.
type SkippedJudgment string

// The judgments JudgeSkipped tells of.
const (
	SkippedByTypes        SkippedJudgment = "TypesHold"
	SkippedByUnrestorable SkippedJudgment = "Unrestorable"
)

// JudgeSkipped has the session's restore from the host's run also judge each package the build skips without judging,
// today each the walk leaves untouched (untouchedOf), as the plain path judges a visited one (#wcwx8vf): one whose
// reads no verdict can judge is told at once, by unrestorableReads's why; every other by TypesHold against the build's
// types once its imports are done. judged is told each, by ID, by which judgment and with why it declines, empty where
// it holds, from the load's goroutines. A skip must be a cheaper proof of what the plain path would conclude, so a test
// sets it to fail on a why. The package stays untouched whatever the judgment says, and nothing else of the run
// changes but that the load reads the types of what the package reaches, as it does for a visited one, which a
// hookless run leaves unread where nothing checked imports it. skipped is told each such package, by ID, as a load
// skips it, before it is judged, once in each load that skips it, so a test counts the skips of every load and not
// the last load's alone (HostRestoreCounts.Untouched). Only a test calls it, after RestoreFromHost; a run never does.
func (session *CacheSession) JudgeSkipped(
	skipped func(identifier string),
	judged func(identifier string, judgment SkippedJudgment, why string),
) {
	if session.host != nil {
		session.host.skipsTold = skipped
		session.host.judgesSkipped = judged
	}
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

	// A package the host kept, in a build whose sizes are not the host's, which restores nothing from it (#61qj12v).
	"sizes": "buildSizes",
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
// kept, decoded from the host's entry. Of those, the walk (untouchedOf) leaves untouched each whose dependence set
// misses what changed: a hit with no late decision and no say, handed the host's findings. Each other is visited, and
// one whose reads can hold is decided late by the load (load.Restored.Holds) and dropped from the hits when it
// declines. keyed is the build's keys, its store's or the portable digests' Restore keyed (hostPortableKeys), and nil
// when those failed, which keys reachable here again and says why. sizes are the load's: a build measured by other
// sizes than the host's restores nothing from it (hostSizesDiffer).
func (session *CacheSession) restoreFromHost(
	loader string,
	sizes types.Sizes,
	reachable []*packages.Package,
	keyed *cache.Keyed,
) {
	state := session.host
	if state == nil || state.off || session.known == nil {
		return
	}
	state.restoring = map[*packages.Package]*hostRestore{}
	state.untouched = map[*packages.Package]*hostRestore{}
	state.visited = 0
	state.writtenThrough = map[*packages.Package]bool{}
	state.renderings = NewReadsBuild()
	state.loadDeclines = map[string]int{}
	state.loadReasons = map[string]string{}
	if why := hostSizesDiffer(state.host, sizes); why != "" {
		// Every package the host kept and the build's own store does not hold would have been a candidate, so each is
		// counted, as one with no entry is.
		for _, candidate := range reachable {
			if _, hit := session.hits[candidate]; !hit && state.host.entries[candidate.ID] != nil {
				state.loadDeclines[reasonWord(why)]++
			}
		}
		return
	}
	var portable map[*packages.Package]cache.Key
	if keyed != nil {
		portable = keyed.Portable
	} else {
		ownKeys, keysError := session.portableKeys(loader, reachable)
		if keysError != nil {
			session.problem(fmt.Sprintf("computing the portable digests to restore from the host: %v", keysError))
			return
		}
		portable = ownKeys.Portable
	}
	// changes are D, by path, and those of it whose own files, or imports or name, may differ from the host's
	// (walkChanges), listed every path of the listing, and ownHits the packages outside D the build's own findings
	// cache holds, which the walk takes by their whole import closure.
	changes := walkChanges{changed: map[string]bool{}, rewritten: map[string]bool{}, reshaped: map[string]bool{}}
	listed := make(map[string]bool, len(reachable))
	var ownHits []*packages.Package
	var reading []*packages.Package
	var readingKept []*hostEntry
	for _, candidate := range reachable {
		listed[candidate.PkgPath] = true
		_, hit := session.hits[candidate]
		kept := state.host.entries[candidate.ID]
		switch {
		case len(candidate.Errors) > 0:
			changes.changed[candidate.PkgPath], changes.rewritten[candidate.PkgPath] = true, true
			changes.reshaped[candidate.PkgPath] = true
			continue
		// A package with a reload pending, or that an earlier load's check declined, has the host's own inputs, so its
		// files, imports and name.
		case session.reloading(candidate) || state.excluded[candidate.ID]:
			changes.changed[candidate.PkgPath] = true
			continue
		// A standard-library package the build holds in its own system store, which the host restored from its own
		// and so kept no entry of, is no change when its own inputs are the host's, by the portable digest: it joins
		// the walk as any package the build's own store holds does (ownHits), taken by its whole import closure, so
		// it is visited, and what reads it with it, the moment anything below it is in D. One whose inputs differ is
		// in D, as a package the host kept with other inputs is below.
		case kept == nil && hit && session.KeptBySystemCache(candidate) &&
			state.host.portables[candidate.ID] == portable[candidate] && portable[candidate] != (cache.Key{}):
			ownHits = append(ownHits, candidate)
			continue
		case kept == nil:
			changes.changed[candidate.PkgPath], changes.rewritten[candidate.PkgPath] = true, true
			changes.reshaped[candidate.PkgPath] = true
			if !hit {
				state.loadDeclines[declineKeys["noEntry"]]++
			}
			continue
		case kept.portable != portable[candidate]:
			changes.changed[candidate.PkgPath], changes.rewritten[candidate.PkgPath] = true, true
			changes.reshaped[candidate.PkgPath] = kept.restoring.Name != candidate.Name ||
				!slices.Equal(kept.restoring.Imports, importPaths(candidate))
			if !hit {
				state.loadDeclines[declineKeys["inputs"]]++
			}
			continue
		case hit:
			ownHits = append(ownHits, candidate)
			continue
		}
		reading = append(reading, candidate)
		readingKept = append(readingKept, kept)
	}
	// The entries are read, and decoded, on every core at once, as readHits reads the store's, and made hits in
	// reachable's order after. Their export data is written only when the load reads it, in the package's own turn
	// (lazyExportData). Each is decoded before the walk, which reads the answers it holds: once a run, shared by every
	// build whose analyzers decode alike (hostEntry.decoded), each build given a package and types of its own, the
	// only part of it a build writes; a build whose analyzers differ decodes the entry itself.
	read := make([]*restoredPackage, len(reading))
	readErrors := make([]error, len(reading))
	sharesDecode := decodesAlike(session.known, state.host.known)
	session.fanOut(len(reading), func(index int) {
		if sharesDecode {
			shared, decodeError := readingKept[index].decoded()
			if decodeError == nil {
				read[index] = restoredForBuild(shared)
			}
			readErrors[index] = decodeError
			return
		}
		body, record, readExportData, readError := readingKept[index].contents()
		if readError == nil {
			read[index], readError = decodeEntry(session.known, reading[index], body, record, readExportData)
		}
		readErrors[index] = readError
	})
	candidates := make([]walkCandidate, 0, len(reading)+len(ownHits))
	for _, ownHit := range ownHits {
		candidates = append(candidates, walkCandidate{candidate: ownHit, dependence: DependenceSet{WholeClosure: true}})
	}
	for index, candidate := range reading {
		if readErrors[index] != nil {
			session.problem(fmt.Sprintf("restoring %s from the host: %v", candidate.ID, readErrors[index]))
			state.loadDeclines[declineKeys["noEntry"]]++
			changes.changed[candidate.PkgPath] = true
			continue
		}
		candidates = append(candidates, walkCandidate{candidate: candidate, dependence: unitedDependence(
			state.host.renderings.Dependence(readingKept[index].restoring),
			answersDependence(read[index], session.byIdentifier))})
	}
	untouched := map[*packages.Package]bool{}
	if !state.visitsEvery {
		untouched = untouchedOf(candidates, changes, listed)
	}
	for index, candidate := range reading {
		if readErrors[index] != nil {
			continue
		}
		kept, restored := readingKept[index], read[index]
		hosted := &hostRestore{kept: kept, restored: restored}
		if untouched[candidate] {
			// Its reads are never judged: by the walk's induction they read as on the host, so its restore is settled.
			restored.types.Untouched = true
			hosted.settled = true
			if state.judgesSkipped != nil {
				session.judgeSkipped(candidate, hosted)
			}
			session.hits[candidate] = restored
			session.trusted[candidate] = true
			state.untouched[candidate] = hosted
			continue
		}
		state.visited++
		if why := unrestorableReads(kept.restoring); why != "" {
			state.loadDeclines[reasonWord(why)]++
			continue
		}
		restored.types.Holds = session.typesHoldFunction(candidate, hosted)
		restored.types.Declined = func() { session.declineAtLoad(candidate, hosted) }
		session.hits[candidate] = restored
		session.trusted[candidate] = true
		state.restoring[candidate] = hosted
	}
}

// hostSizesDiffer is why a build measured by sizes, the load's, restores nothing from host, empty when it may. The
// host's run checked each package under its own sizes, so a package of a build whose word size or maximum alignment
// differ evaluated `unsafe.Sizeof`, `unsafe.Alignof` and `unsafe.Offsetof` to other constants, and its findings,
// SA9006's and a shift's among them, turn on them where its reads of other packages do not: a package reading only
// its own pair of words reads nothing a verdict judges, held or untouched (#61qj12v). Such a package is checked from
// source, never restored by sizes it was not checked under. A build or a host measured by no sizes is declined too,
// since nothing says they are alike.
func hostSizesDiffer(host *HostRun, sizes types.Sizes) string {
	hostWordSize, hostMaximumAlignment, hostMeasured := host.renderings.measuredSizes()
	if !hostMeasured || sizes == nil {
		return "sizes: the host's run or the build is measured by no sizes"
	}
	wordSize, maximumAlignment := MeasureOf(sizes)
	if wordSize != hostWordSize || maximumAlignment != hostMaximumAlignment {
		return fmt.Sprintf("sizes: the host's run was checked under word size %d, maximum alignment %d, the build "+
			"is under %d, %d", hostWordSize, hostMaximumAlignment, wordSize, maximumAlignment)
	}
	return ""
}

// LoadedSizes are the sizes the packages of a load were checked by, each package's the load's, nil when none was
// checked.
func LoadedSizes(loaded []*packages.Package) types.Sizes {
	for _, each := range loaded {
		if each.TypesSizes != nil {
			return each.TypesSizes
		}
	}
	return nil
}

// unrestorableReads is why a visited package's reads can never hold, as a reason the record counts by its first word,
// empty for reads a verdict judges: one kept with none, or whose reads are marked never or were never recorded, is
// declined here.
func unrestorableReads(restoring Restoring) string {
	if restoring.Reads == nil {
		return noReads
	}
	if restorable, why := restoring.Reads.Restorable(); !restorable {
		if restoring.Reads.Never() != "" {
			return "never: " + why
		}
		return "unknown: " + why
	}
	return ""
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
		// A verdict is the run's work as a load's or a check's is, so it runs under one of the run's slots, at the top
		// of the scale: the package's load waits on it. The load's goroutine asks it before it takes a slot of its own
		// for the package, so it holds none here.
		if session.caching.Pool != nil {
			session.caching.Pool.Acquire(bounded.PriorityScale)
			defer session.caching.Pool.Release()
		}
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

// judgeSkipped tells a test's JudgeSkipped that the load skipped candidate, hosted's package, and how the plain path
// judges it: by unrestorableReads at once, or by TypesHold as the load decides it (typesHoldFunction), whose answer is
// told and never kept, so the package is read from the host's export data, untouched, as it would be without the
// hook.
//
// Mutates: hosted its restore gains a late decision that always holds
func (session *CacheSession) judgeSkipped(candidate *packages.Package, hosted *hostRestore) {
	if session.host.skipsTold != nil {
		session.host.skipsTold(candidate.ID)
	}
	if why := unrestorableReads(hosted.kept.restoring); why != "" {
		session.host.judgesSkipped(candidate.ID, SkippedByUnrestorable, why)
		return
	}
	hosted.restored.types.Holds = func(closure map[string]*types.Package) bool {
		session.host.renderings.measureBy(candidate.TypesSizes)
		_, why := TypesHold(hosted.kept.restoring, closure, session.restoredBelow(candidate, closure),
			session.host.host.renderings, session.host.renderings)
		session.host.judgesSkipped(candidate.ID, SkippedByTypes, why)
		return true
	}
}

// restoredBelow are the types of the packages of closure, what candidate reaches by path, that this load restored from
// the host's run, decided late or left untouched. Each is done when candidate's restore is decided, its types set and
// its decline, if any, counted (declineAtLoad), so a package still among the restores is one whose types were read from
// the host's export data.
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
		_, judged := state.restoring[imported]
		_, untouched := state.untouched[imported]
		if (judged || untouched) && imported.Types != nil && closure[imported.PkgPath] == imported.Types {
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
	for candidate := range session.host.untouched {
		if _, hit := session.hits[candidate]; !hit {
			delete(session.host.untouched, candidate)
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
// (readsHold), is declined. It returns the IDs declined, sorted, which the run asks to load again, and excludes each of
// them from the host's restores from then on: the reload checks it from source, and it is of what changed for the
// reload's walk. Each package's verdict is its own, so they are decided on every core at once (fanOutDeciding), and
// what they share, the counts, the reasons and each restore's settling, is written after, in ID order, so the result is
// the same whatever order they finish in (#9fer4tx).
//
// The packages above a decline are not excluded (#axbw09w). This load judged them against the declined package's
// restored types and analyzed them over its restored facts and answers, but the reload judges and analyzes everything
// again, so nothing of this load's judgment survives into it. In the reload, one whose dependence set meets the
// declined package is visited, and decided late again by TypesHold, FactsHold and readsHold against the declined
// package as the reload checks it from source, and restored only if every read holds there; the rest stay untouched.
// So a package re-judged above the decline holds only against the declined package's rendering from source, its facts
// and its answers as the reload computes them, by the walk's induction (untouchedOf): every package its set reaches is
// either untouched, rendering as on the host, or judged in the reload, or checked from source there, and it is held
// only where each read of it reads alike against that. A second load's worth of declines turns the restore from the
// host off, so the build loads again at most twice for it.
func (session *CacheSession) decideHostRestores(graphs []*checker.Graph) []string {
	state := session.host
	if state == nil || len(state.restoring) == 0 {
		return nil
	}
	// The candidates in ID order, so the writes below run in one order however the workers finish.
	candidates := slices.SortedFunc(maps.Keys(state.restoring), func(left, right *packages.Package) int {
		return strings.Compare(left.ID, right.ID)
	})
	// Indexed before the fan-out, so the workers only read it. It is FactsHold's walks of every candidate's closure,
	// made once, so the time deciding counts it.
	indexing := time.Now()
	closures := newClosureIndex(candidates)
	state.deciding.Add(int64(time.Since(indexing)))
	exported := newBuildFacts(session, graphs, closures)
	// Each candidate's why is its own: decided on every core at once, on the build's critical path, each written to its
	// own index. A worker only reads what the analysis left, and the facts' memo, which gathers each owner once.
	whys := make([]string, len(candidates))
	session.fanOutDeciding(len(candidates), func(index int) {
		candidate := candidates[index]
		hosted := state.restoring[candidate]
		// No candidate here was declined by the load: a restore is made afresh for each load, its why is set at load
		// only when its types do not hold (typesHoldFunction), and the load then declines it (load's decline), which
		// drops it from restoring (declineAtLoad) before the load returns, so its facts and reads are always asked
		// (#a6caffk).
		why := exported.factsHold(candidate, hosted.kept, state)
		if why == "" && (hosted.restored.readsIncomplete || !session.readsHold(candidate)) {
			why = "A an answer it read of another package differs in the build, or cannot be named"
		}
		whys[index] = why
	})
	var declined []*packages.Package
	for index, candidate := range candidates {
		hosted, why := state.restoring[candidate], whys[index]
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
	for _, candidate := range declined {
		state.excluded[candidate.ID] = true
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

// writeThrough writes, under the build's own first key, the entry of each package restored from the host's run whose
// restore has settled and held, with nothing declined below it (outside unsettled), no reload pending, nothing that
// stood in it in any load (stoodIn), every action on it restored without an error and an entry the build would write
// (throughEntry), so the build's next run hits it in its own store rather than check it from source once the host,
// hitting its own, keeps nothing to restore from (#b2t9xmf). It returns whether each package written has a finding that
// names a file outside it (see positionDependent), which keeps its second key from an alias. actionsOf are the actions
// of the run's graphs on each package and knownFiles every file the load reached.
//
// The entry is exactly as sound as the restore, and no sounder. It persists a verdict that held because what the
// package read of other packages read the same in this run: its types' reads at the load, its facts' and answers' at
// the check. Under the build's key that verdict stands for every later run that hits the key only because the key
// covers everything those reads were judged against: the package's own inputs, by the portable digest the restore
// matched; every dependency's key in this build, which holds that dependency's files, and so its types, facts and
// answers, and everything below it; and the build's context, its target, flags and rule set. A dependency whose
// output did not change but whose key did, as a comment edited, changes this key too, so the entry is not hit by it
// again: the second key, or a check from source, decides it then, as for any entry the build wrote itself.
//
// A package the walk left untouched (untouchedOf) is written through too, and its entry is sound otherwise: no verdict
// judged its reads, so it persists the walk's induction instead, that every package its dependence set reaches was
// outside what changed and so renders, declares and answers as on the host. Under the build's key that stands for
// every later run that hits it for the same reason a judged entry does: the key covers the package's own inputs and
// every dependency's key, so a later run in which anything the induction rested on moved has another key, and decides
// the package again. A build that wrote it nowhere would check it from source on its next run, once the host, hitting
// its own store, keeps nothing to restore it from.
func (session *CacheSession) writeThrough(
	actionsOf map[*packages.Package][]*checker.Action,
	unsettled map[*packages.Package]bool,
	knownFiles *knownFiles,
) map[*packages.Package]bool {
	if session.host == nil || session.caching.Store == nil && !session.KeepsSystemCache() {
		return nil
	}
	var through []*packages.Package
	var throughHosted []*hostRestore
	// An untouched package is written through as a held one is, on the walk's induction (see above).
	settledRestores := maps.Clone(session.host.restoring)
	maps.Copy(settledRestores, session.host.untouched)
	for candidate, hosted := range settledRestores {
		hosted.lock.Lock()
		held := hosted.settled && hosted.declined == ""
		hosted.lock.Unlock()
		// A package pending a reload is checked from source by the next load, which writes its entry as a miss. One
		// with no store, a project package under `--no-cache`, has no key either (keyStandardLibraryApart).
		if _, keyed := session.keys[candidate]; !held || !keyed || session.storeFor(candidate) == nil ||
			unsettled[candidate] ||
			session.host.writtenThrough[candidate] || session.stoodIn(candidate.ID) ||
			session.reloading(candidate) || !finishedClean(actionsOf[candidate]) {
			continue
		}
		through = append(through, candidate)
		throughHosted = append(throughHosted, hosted)
	}
	encoded := make([]encodedEntry, len(through))
	writable := make([]bool, len(through))
	writeErrors := make([]error, len(through))
	session.fanOutWriting(len(through), func(index int) func() {
		saved := through[index]
		_, untouched := session.host.untouched[saved]
		encoded[index], writable[index], writeErrors[index] = session.throughEntry(saved, throughHosted[index],
			actionsOf[saved], knownFiles, untouched)
		if !writable[index] || writeErrors[index] != nil {
			return nil
		}
		key := session.keys[saved]
		// The writes run with no slot held (fanOutWriting), as putEntry writes a miss's: the export data first, so an
		// entry is never in the store before what it needs to be read, and only when the store does not hold it by its
		// digest already, as it does for every package the host wrote, whose export data the build read. A
		// standard-library package goes to the system cache's store under the build's own key, as its miss would.
		hosted := throughHosted[index]
		store := session.storeFor(saved)
		return func() {
			// A record of the host's the store holds, as it holds every one the host wrote there, is named and never
			// read; a body with none, or whose record is not in this store, is held in the build's own record.
			if encoded[index].record.BodyAt == nil || !store.Holds(cache.Key(encoded[index].record.BodyAt)) {
				encoded[index].record.BodyAt = nil
				encoded[index].record.Inline, writeErrors[index] = hostBody(hosted)
				if writeErrors[index] != nil {
					return
				}
			}
			writeErrors[index] = session.putRecord(store, key, encoded[index])
		}
	})
	positionDependent := map[*packages.Package]bool{}
	for index, saved := range through {
		if writeErrors[index] != nil {
			// A failed write is a problem, never a finding: the next run checks the package from source. One to the
			// system cache's store is why the run could not keep it (SystemCacheFailure).
			if session.storeFor(saved) != session.caching.Store {
				session.systemCacheFailed(writeErrors[index])
				continue
			}
			session.problem(fmt.Sprintf("writing through the entry of %s restored from the host: %v", saved.ID,
				writeErrors[index]))
			continue
		}
		if writable[index] {
			session.host.writtenThrough[saved] = true
			// A standard-library package written through to the system cache's store is counted as its miss would be
			// there, apart from the project's.
			if session.storeFor(saved) != session.caching.Store {
				session.counts.SystemCacheWritten++
			} else {
				session.counts.WrittenThrough++
			}
			dependent, judgeError := encoded[index].positionDependent.read(saved.ID)
			if judgeError != nil {
				// Left out, the package is given no alias.
				session.problem(judgeError.Error())
				continue
			}
			positionDependent[saved] = dependent
		}
	}
	return positionDependent
}

// throughEntry is the entry writeThrough writes of saved, restored from the host's run as hosted, and whether it is
// the build's to write: the host's entry as the build restored it, its findings, facts, answers, withheld counts,
// files, imports, output digest and what it declares without a body, with the export data the build read, none for
// unsafe, as the build would write it had it checked the package itself. Its reads name each owner by its first key
// in this build, which every read held against when the restore settled.
//
// The output digest is the host's, which is the build's own only while the entry holds exactly the analyzers of the
// build's actions on saved, as encodeEntry's would, since the digest names every one of them; an entry that holds
// another set is not written. With that set the two are the same by construction: the digest is of the package's API
// by value, rendered from its types checked from source (exportDigest), and of its facts by value, and the restore
// holding is the verdict that the build's check from source would give the package the host's types and facts. It
// cannot be computed again from the types the build read, since export data drops the package's Go version, which the
// rendering holds, so a digest of those would differ from the build's own and have importers flap between a hit and a
// miss across runs. An untouched package (untouched), which no action of the build's ran on, is written with the host's
// analyzers, those of the same rules.
func (session *CacheSession) throughEntry(
	saved *packages.Package,
	hosted *hostRestore,
	actions []*checker.Action,
	knownFiles *knownFiles,
	untouched bool,
) (encodedEntry, bool, error) {
	slots := map[string]bool{}
	for _, action := range actions {
		if slot, held := session.known.slot(action.Analyzer); held {
			slots[slot] = true
		}
	}
	// An untouched package had no action of its own to compare by. Its entry is the host's for the same rules, and an
	// entry that falls short of what a later run asks is a miss there (dropShortHits), never a wrong hit.
	if !untouched && len(hosted.restored.slots) != len(slots) {
		return encodedEntry{}, false, nil
	}
	for _, slot := range hosted.restored.slots {
		if !untouched && !slots[slot] {
			return encodedEntry{}, false, nil
		}
	}
	// The body is the host's, named in the host's record, which the build's store holds once the host wrote it there;
	// one it does not hold, written to another store, as a standard-library body the host wrote to the system cache's
	// store is for a build under `--build-dependence`, which keeps none, writeThrough holds in the
	// record itself, read from the host's entry (hostBody). Nothing of the body is decoded or encoded again: the record
	// alone is the build's (#fq7dxvx).
	encoded := encodedEntry{output: hosted.restored.output, hasOutput: hosted.restored.hasOutput}
	// unsafe's entry holds no export data, as the build's own from source holds none (carriesExportData); the reader of
	// one that holds none for it would refuse (#27r8r24).
	if carriesExportData(saved) && hosted.restored.types.ReadExportData != nil {
		exportData, exportError := hosted.restored.types.ReadExportData()
		if exportError != nil {
			return encodedEntry{}, true, fmt.Errorf("reading the export data the build read: %w", exportError)
		}
		encoded.exportData = exportData
	}
	// The record names the export data the build read by its digest, which is the host's record's own when the host
	// wrote it, and new for one kept from the host's memory under `--no-cache`.
	reads, readsIncomplete := session.readsInBuild(saved.ID, hosted.restored.reads, hosted.restored.readsIncomplete)
	encoded.record = entryRecord{Version: CacheFormatVersion, Body: hosted.restored.bodyDigest,
		ExportData: exportDataDigest(encoded.exportData), Reads: reads, ReadsIncomplete: readsIncomplete}
	if hosted.kept.holdsBodyAt {
		encoded.record.BodyAt = hosted.kept.bodyAt[:]
	}
	encoded.positionDependent = judgedPosition(restoredPositionDependent(saved, hosted.restored, knownFiles))
	return encoded, true, nil
}

// hostBody is the body of hosted's entry, read from the host's run, for a build whose store does not hold it yet.
func hostBody(hosted *hostRestore) ([]byte, error) {
	body, _, _, readError := hosted.kept.contents()
	if readError != nil {
		return nil, fmt.Errorf("reading the host's entry: %w", readError)
	}
	return body, nil
}

// readsInBuild are reads, the entry of the package written through as written's, each naming its owner by the
// owner's first key in this build rather than the host's, which the build's next run compares a read's owner by
// (readsHold), and whether one could not be named: incomplete as it was, or set when the build has no owner or no key
// for a read. The restore checked every read against the owner as the build has it, so each still says what the owner
// of that key answers.
//
// Neither can happen to an entry written through, so each is a problem the run reports, and the entry is still marked
// incomplete so a next run never trusts it. Its restore held, and a restore holds only while readsHold finds every
// read's owner among the build's packages by identifier; a write-through runs only for a package with a store, and the
// session keys every owner such a package reads: with the project's store cache.Keys keys every package it reaches
// (all of them or it fails), the same packages byIdentifier names, and under `--no-cache` only the standard library
// is written through, whose packages read only the standard library's, which the session keys
// (keyStandardLibraryApart).
func (session *CacheSession) readsInBuild(written string, reads []storedRead, incomplete bool) ([]storedRead, bool) {
	inBuild := make([]storedRead, 0, len(reads))
	unnamed := incomplete
	for _, read := range reads {
		owner := session.byIdentifier[read.Owner]
		if owner == nil {
			session.problem(fmt.Sprintf("writing %s through: the build has no package %s, whose answer it read",
				written, read.Owner))
			unnamed = true
			continue
		}
		key, keyed := session.keys[owner]
		if !keyed {
			session.problem(fmt.Sprintf("writing %s through: the build keyed no %s, whose answer it read", written,
				read.Owner))
			unnamed = true
			continue
		}
		read.OwnerKey = key[:]
		inBuild = append(inBuild, read)
	}
	return inBuild, unnamed
}

// hostCounts is what the session restored from the host's run over its last load, and what check declined over every
// load of the run.
func (session *CacheSession) hostCounts() HostRestoreCounts {
	state := session.host
	if state == nil {
		return HostRestoreCounts{}
	}
	counts := HostRestoreCounts{
		Visited:         state.visited,
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
	for candidate := range state.untouched {
		counts.Untouched = append(counts.Untouched, candidate.ID)
	}
	slices.Sort(counts.Untouched)
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
// reach, whose reads can hold, for its extra builds to restore from (#b2t9xmf): the package's entry, read back from the
// store the run wrote it to, the system cache's for a standard-library package (storeFor), or else encoded here, as
// saveMisses encodes it, and held in memory, which a run under `--no-cache` never stores. A package the run restored,
// from either store, is not kept: each build reads its own. Each package's portable digest is the one Restore keyed
// it by, before the load gave anything its syntax (KeepForBuilds). Only a run with extra builds asks it, so a run
// without pays nothing. gathered gives the findings the run's gather made of each root action, which an encoded entry
// holds rather than convert them again (see encodeEntry).
func (session *CacheSession) keepForBuilds(
	graphs []*checker.Graph,
	analyzed []*packages.Package,
	reads map[*packages.Package]*build_reads.Reads,
	withheld map[*packages.Package]load.PackageWithheld,
	files map[*packages.Package][]storedFile,
	gathered func(action *checker.Action) ([]Finding, bool),
	walks *build_reads.Walks,
	asking func(owner *packages.Package, restoring Restoring),
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
	// A run without the project's store names its reads' owners by the keys it computed for its builds, though it may
	// key the standard library apart in session.keys for the system cache.
	if session.caching.Store == nil {
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
	// The bodies the session decoded from its store go to the builds with the run, which empties the memo once the
	// last build has run (HostRun.Release), rather than have it live as long as the session. The session keeps the
	// same memo, so a host that analyzes again after a reload keeps what its earlier loads decoded, and the run its
	// last keep hands the builds holds all of it.
	host := &HostRun{entries: map[string]*hostEntry{}, renderings: NewReadsBuild(), files: keyed.Files,
		bodies: session.bodies, portables: make(map[string]cache.Key, len(keyed.Portable))}
	for keyedPackage, portable := range keyed.Portable {
		host.portables[keyedPackage.ID] = portable
	}
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
			// The record the run wrote of it holds its body, which a build writing the package through names, in the
			// store the run wrote it to, the system cache's for a standard-library package (storeFor).
			store := session.storeFor(keptPackage)
			kept.bodyAt, kept.holdsBodyAt = key, true
			kept.contents = func() ([]byte, entryRecord, func() ([]byte, error), error) {
				contents, stored := store.Get(key)
				if !stored {
					return nil, entryRecord{}, nil, fmt.Errorf("the store holds no entry of %s", keptPackage.ID)
				}
				body, record, whole := storedEntry(store, contents)
				if !whole {
					return nil, entryRecord{}, nil, fmt.Errorf("the store holds no whole entry of %s", keptPackage.ID)
				}
				// The export data is the one the record names by digest, read only when a load asks for it.
				return body, record, func() ([]byte, error) {
					return readExportDataByDigest(store, keptPackage, record.ExportData)
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
	// builds. The types live until the last build has run (HostRun.Release). An entry kept is encoded without the
	// load's files, so whether its findings name another package's file is not judged: only the store's entries are
	// aliased by it, and a build writing one through judges it again from the findings (throughEntry).
	encoded := make([]encodedEntry, len(encoding))
	encodeErrors := make([]error, len(encoding))
	session.fanOut(len(encoding), func(index int) {
		saved := encoding[index]
		demanded := session.withBodiless(saved, session.recordedDemandOf(saved, keyed.Files))
		encoded[index], encodeErrors[index] = encodeEntry(session.known, session.ruleOf, saved, actionsOf[saved],
			withheld[saved], demanded, files[saved], nil, gathered, false)
	})
	for index, saved := range encoding {
		if encodeErrors[index] != nil {
			if !errors.Is(encodeErrors[index], errorUnstorable) {
				session.problem(encodeErrors[index].Error())
			}
			delete(host.entries, saved.ID)
			continue
		}
		body, record := encoded[index].body, encoded[index].record
		readExportData := lazyExportData(saved)
		host.entries[saved.ID].contents = func() ([]byte, entryRecord, func() ([]byte, error), error) {
			return body, record, readExportData, nil
		}
	}
	// Each entry knows the files the host compiled into it, which the builds' inheritance indexes (holdersOf), and
	// its package, whose walk its dependence set waits for (computeDependence).
	var owners []*packages.Package
	var ownersKept []*hostEntry
	for keptPackage := range keyed.Portable {
		if kept := host.entries[keptPackage.ID]; kept != nil {
			kept.compiledFiles = keptPackage.CompiledGoFiles
			owners = append(owners, keptPackage)
			ownersKept = append(ownersKept, kept)
		}
	}
	// Each entry is decoded the first time a build considers it, and never again by another (hostEntry.decoded).
	host.known = session.known
	for identifier, kept := range host.entries {
		kept.decoded = decodeOnce(kept, identifier, session.known)
	}
	host.kept = len(host.entries)
	// What the analysis built to answer questions, every package's intermediate representation, syntax and type maps,
	// goes now rather than outlive the host's pass through every build (#b2t9xmf), with a store or without: the keep is
	// the last of the run to read it, after the save and the reload's decision, the extra builds read only the entries,
	// and the session's next Restore starts both afresh. Kept with a store, it held about 3.3 GB at cohere's root as
	// the builds started, so a cold run with the findings cache ran them one at a time (#px1yxq8).
	session.resolver = nil
	session.answering.lock.Lock()
	session.answering.byTypes = nil
	session.answering.lock.Unlock()
	// Every kept package's dependence set, computed once by the host as its walk ends, rather than by the builds
	// racing to ask it; the keep does not wait for them.
	host.computeDependence(walks, session.caching.Pool, owners, ownersKept, asking)
	session.kept.Store(true)
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
// intermediate representation for answers the builds mostly never ask, every answer the run holds about its declared
// functions and their parameters by place (fact_resolver's PlacedIn), as an entry the store keeps holds them, each
// file's digest read from files, the digests the keep's keys hashed, and the reads its analysis made of other
// packages. An extra build that asks one it does not hold reloads the package, as a
// run restoring a partial entry does (see Reload).
func (session *CacheSession) recordedDemandOf(saved *packages.Package, files cache.FileDigests) entryDemand {
	if session.readAfterKeep("recordedDemandOf, the kept answers of "+saved.ID) || session.resolver == nil ||
		saved.Types == nil {
		return entryDemand{}
	}
	demanded := entryDemand{answers: sortedAnswers(session.resolver.Recorded(saved.Types), false)}
	if home := session.resolver.Package(saved.Types); home != nil {
		demanded.placed = session.resolver.PlacedIn(home, digestsOf(files))
	}
	demanded.reads, demanded.readsIncomplete = session.readsOf(saved, entryDemand{})
	return demanded
}

// portableKeys are reachable's keys as an extra build without a store computes them for the portable digests alone,
// reading the digests of the files the host's run hashed rather than hash them again.
func (session *CacheSession) portableKeys(loader string, reachable []*packages.Package) (*cache.Keyed, error) {
	return cache.Keys(reachable, session.keyContextOf(loader), session.caching.ExtraInputs, session.host.host.files,
		session.caching.Held)
}

// hostPortableKeys are portableKeys, nil when they cannot be computed, which restoreFromHost computes again and says
// why, so a failure is said once.
func (session *CacheSession) hostPortableKeys(loader string, reachable []*packages.Package) *cache.Keyed {
	keyed, keysError := session.portableKeys(loader, reachable)
	if keysError != nil {
		return nil
	}
	return keyed
}
