package check

import (
	"cmp"
	"fmt"
	"go/types"
	"maps"
	"runtime"
	"slices"
	"sync"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// kindAnswers is one kind's answers in one package, as the store and the binary's table hold them (#w7zmvat wave 3):
// each answer by its object's objectpath, sorted, and whether the package was computed whole in the kind, so an object
// with no answer has no fact.
type kindAnswers struct {
	Kind     string
	Complete bool
	Answers  []pathAnswer
}

// pathAnswer is one answer, as fact_resolver encodes it, JSON, about the object at Path.
type pathAnswer struct {
	Path  objectpath.Path
	Value []byte
}

// sortedAnswers is answers, by kind's name and objectpath, as a list sorted by both, each kind complete when complete
// is, so the same answers always encode alike.
func sortedAnswers(answers map[string]map[objectpath.Path][]byte, complete bool) []kindAnswers {
	sorted := make([]kindAnswers, 0, len(answers))
	for _, kind := range slices.Sorted(maps.Keys(answers)) {
		held := kindAnswers{Kind: kind, Complete: complete}
		for _, path := range slices.Sorted(maps.Keys(answers[kind])) {
			held.Answers = append(held.Answers, pathAnswer{Path: path, Value: answers[kind][path]})
		}
		sorted = append(sorted, held)
	}
	return sorted
}

// sortAnswers sorts answers in place, by kind's name and then each kind's by objectpath.
func sortAnswers(answers []kindAnswers) {
	slices.SortFunc(
		answers,
		func(left kindAnswers, right kindAnswers) int { return cmp.Compare(left.Kind, right.Kind) },
	)
	for index := range answers {
		slices.SortFunc(answers[index].Answers, func(left pathAnswer, right pathAnswer) int {
			return cmp.Compare(left.Path, right.Path)
		})
	}
}

// addAnswers adds held to answers, the fact_resolver's shape of what a store holds.
func addAnswers(answers *fact_resolver.Answers, held []kindAnswers) {
	for _, kind := range held {
		if answers.Values[kind.Kind] == nil {
			answers.Values[kind.Kind] = map[objectpath.Path][]byte{}
		}
		for _, answer := range kind.Answers {
			answers.Values[kind.Kind][answer.Path] = answer.Value
		}
		answers.Complete[kind.Kind] = answers.Complete[kind.Kind] || kind.Complete
	}
}

// answering is what the session knows of the packages questions reach, for fact_resolver's Store: each package by its
// types, once they are final, and the packages the run loads from source because a question reached them restored with
// no answer stored, by ID, which a later load keeps.
type answering struct {
	lock    sync.Mutex
	byTypes map[*types.Package]*packages.Package

	// reloaded are the IDs of the packages every load of the run so far checked from source to answer a question,
	// never restored, and rounds how many times a pass asked for new ones, each a load and a lint of its own. reasons
	// are why the last pass asked for each, by ID (noteReloadReasons), and roundsBy counts each round under every
	// reason of the packages it added.
	reloaded map[string]bool
	rounds   int
	reasons  map[string][]string
	roundsBy map[string]int
}

// The reasons a pass asks for a package to be loaded again, which count its rounds (answering.roundsBy): a question
// reached it restored with no answer stored, or the extra build restored it from the host's run and declined it.
const (
	reloadForMissingAnswer = "missingAnswer"
	reloadForHostDecline   = "hostDecline"
)

// readAfterKeep reports whether the session has let go of its resolver and its answer memo (keepForBuilds), and when
// it has, records as a problem that caller read one of them after, naming caller. Read then, either is empty, so a
// save would write an entry with fewer answers or reads than the run computed, and nothing would say so; the keep is
// the run's last reader, and only the next Restore makes them anew (#px1yxq8).
func (session *CacheSession) readAfterKeep(caller string) bool {
	if !session.kept.Load() {
		return false
	}
	session.problem(caller + ": it read the resolver or its answer memo after the run kept its packages for the " +
		"extra builds, which let them go")
	return true
}

// know adds each package of known whose types are set, which the run's questions may reach.
func (session *CacheSession) know(known ...*packages.Package) {
	if session.readAfterKeep("know") {
		return
	}
	session.answering.lock.Lock()
	defer session.answering.lock.Unlock()
	if session.answering.byTypes == nil {
		session.answering.byTypes = map[*types.Package]*packages.Package{}
	}
	for _, each := range known {
		if each.Types != nil {
			session.answering.byTypes[each.Types] = each
		}
	}
}

// packageOf is the package whose types are packageTypes, or nil for one the session was never told of.
func (session *CacheSession) packageOf(packageTypes *types.Package) *packages.Package {
	if session.readAfterKeep("packageOf, the package of " + packageTypes.Path()) {
		return nil
	}
	session.answering.lock.Lock()
	defer session.answering.lock.Unlock()
	return session.answering.byTypes[packageTypes]
}

// Answers is what the run can read of the answers of the package whose types are packageTypes, for the resolver: its
// entry's, for a package the run trusts the entry of (see trusted), restored from the binary's table, complete in every
// kind its release computed, or from the store by its own first key, or by an alias or its second key once its reads
// held; or, for a package loaded from source to answer a question, its entry's under its own first key, which the run
// did not restore. A trusted entry's record of what its package declares without a body comes with them. It is nil
// when there are none and no such record.
func (session *CacheSession) Answers(packageTypes *types.Package) *fact_resolver.Answers {
	asked := session.packageOf(packageTypes)
	if asked == nil {
		return nil
	}
	var held entryDemand
	if restored := session.hits[asked]; restored != nil {
		if session.trusted[asked] {
			held = entryDemand{answers: restored.answers, bodiless: restored.bodiless,
				bodiesRecorded: restored.bodiesRecorded}
		}
	} else {
		held = session.previous[asked]
	}
	if len(held.answers) == 0 && !held.bodiesRecorded {
		return nil
	}
	answers := &fact_resolver.Answers{
		Values:         map[string]map[objectpath.Path][]byte{},
		Complete:       map[string]bool{},
		BodiesRecorded: held.bodiesRecorded,
	}
	addAnswers(answers, held.answers)
	if held.bodiesRecorded {
		answers.Bodiless = make(map[objectpath.Path]bool, len(held.bodiless))
		for _, path := range held.bodiless {
			answers.Bodiless[path] = true
		}
	}
	return answers
}

// Reloaded reports whether the run loaded the package whose types are packageTypes from source to answer a question.
func (session *CacheSession) Reloaded(packageTypes *types.Package) bool {
	asked := session.packageOf(packageTypes)
	if asked == nil {
		return false
	}
	session.answering.lock.Lock()
	defer session.answering.lock.Unlock()
	return session.answering.reloaded[asked.ID]
}

// Reload has every later load of the run check the packages whose IDs are reloads from source, never restoring them, so
// the resolver learns them and answers what a question asked of them, and reports whether any of them is one no load
// checked from source for this before. One every load already did is a reload that failed, which the question's
// failure names, so loading again would change nothing.
func (session *CacheSession) Reload(reloads []string) bool {
	session.answering.lock.Lock()
	defer session.answering.lock.Unlock()
	if session.answering.reloaded == nil {
		session.answering.reloaded = map[string]bool{}
	}
	added := false
	reasons := map[string]bool{}
	for _, identifier := range reloads {
		if !session.answering.reloaded[identifier] {
			session.answering.reloaded[identifier] = true
			added = true
			for _, reason := range session.answering.reasons[identifier] {
				reasons[reason] = true
			}
		}
	}
	if added {
		session.answering.rounds++
		if session.answering.roundsBy == nil {
			session.answering.roundsBy = map[string]int{}
		}
		for reason := range reasons {
			session.answering.roundsBy[reason]++
		}
	}
	return added
}

// noteReloadReasons keeps why a pass asks for each package it names to be loaded again, which the round that loads
// them counts (Reload): missingAnswers, the IDs a question reached restored with no answer stored, and hostDeclines,
// those restored from the host's run that the build declined. Each pass's replace the last's.
func (session *CacheSession) noteReloadReasons(missingAnswers []string, hostDeclines []string) {
	session.answering.lock.Lock()
	defer session.answering.lock.Unlock()
	session.answering.reasons = map[string][]string{}
	for _, identifier := range missingAnswers {
		session.answering.reasons[identifier] = append(session.answering.reasons[identifier], reloadForMissingAnswer)
	}
	for _, identifier := range hostDeclines {
		session.answering.reasons[identifier] = append(session.answering.reasons[identifier], reloadForHostDecline)
	}
}

// reloading reports whether the run checks the package from source to answer a question, so no load restores it.
func (session *CacheSession) reloading(restoring *packages.Package) bool {
	session.answering.lock.Lock()
	defer session.answering.lock.Unlock()
	return session.answering.reloaded[restoring.ID]
}

// reloadsOf is the IDs of the packages a question reached restored, with no answer stored, sorted.
func (session *CacheSession) reloadsOf(resolver *fact_resolver.Resolver) []string {
	if session.readAfterKeep("reloadsOf") {
		return nil
	}
	var reloads []string
	for _, packageTypes := range resolver.Reloads() {
		if asked := session.packageOf(packageTypes); asked != nil {
			reloads = append(reloads, asked.ID)
		}
	}
	slices.Sort(reloads)
	return slices.Compact(reloads)
}

// readPrevious reads, for each package among reachable the run loads from source to answer a question, the answers
// and reads its entry under its own first key holds, which the run did not restore, so the answers answer without a
// build, and both stay in the entry the run writes for it.
func (session *CacheSession) readPrevious(reachable []*packages.Package) {
	session.previous = map[*packages.Package]entryDemand{}
	if session.caching.Store == nil {
		return
	}
	for _, reloaded := range reachable {
		key, keyed := session.keys[reloaded]
		if !keyed || !session.reloading(reloaded) {
			continue
		}
		entry, _, stored := storedEntryAt(session.caching.Store, key)
		if !stored {
			continue
		}
		session.previous[reloaded] = entryDemand{answers: entry.Answers, reads: entry.Reads,
			readsIncomplete: entry.ReadsIncomplete}
	}
}

// entryDemand is what an entry holds of the fact kinds computed on demand: the package's answers, the answers its
// analysis read of other packages, with whether those could all be named, and what it declares without a body, with
// whether the load recorded that (see cacheEntry's Bodiless).
type entryDemand struct {
	answers         []kindAnswers
	reads           []storedRead
	readsIncomplete bool
	bodiless        []objectpath.Path
	bodiesRecorded  bool
}

// withBodiless is demanded with what saved declares without a body, as the run's load recorded it while it held
// saved's syntax (load.Bodiless), for the entry written of saved.
func (session *CacheSession) withBodiless(saved *packages.Package, demanded entryDemand) entryDemand {
	demanded.bodiless, demanded.bodiesRecorded = session.bodiless.Of(saved)
	return demanded
}

// demandOf is what the entry of saved holds of the fact kinds computed on demand. Its answers are every answer of its
// package, computed whole, as the binary's table holds them, so a later run that restores it never reloads it to
// answer a question, when the resolver can compute them all without a reload and Caching's AnswersInPart does not
// name it; otherwise those held before, the entry's before a reload or the first wave's (held), with every answer the
// run computed in it. Its reads are those held before with every read the run's analysis of it made.
func (session *CacheSession) demandOf(saved *packages.Package, held entryDemand) entryDemand {
	if session.readAfterKeep("demandOf, the saved answers of "+saved.ID) || session.resolver == nil ||
		saved.Types == nil {
		return held
	}
	demanded := entryDemand{}
	if !session.caching.AnswersInPart[saved.PkgPath] {
		if home := session.resolver.Package(saved.Types); home != nil {
			if whole, complete := session.resolver.WholeIfComplete(home); complete {
				demanded.answers = sortedAnswers(whole, true)
			}
		}
	}
	if demanded.answers == nil {
		merged := session.resolver.Recorded(saved.Types)
		complete := map[string]bool{}
		for _, kind := range held.answers {
			if merged[kind.Kind] == nil {
				merged[kind.Kind] = map[objectpath.Path][]byte{}
			}
			for _, answer := range kind.Answers {
				merged[kind.Kind][answer.Path] = answer.Value
			}
			complete[kind.Kind] = complete[kind.Kind] || kind.Complete
		}
		demanded.answers = sortedAnswers(merged, false)
		for index := range demanded.answers {
			demanded.answers[index].Complete = complete[demanded.answers[index].Kind]
		}
	}
	demanded.reads, demanded.readsIncomplete = session.readsOf(saved, held)
	return demanded
}

// rewriteEarlier writes again the entry of each package the first wave saved whose answers or reads grew since, as
// the second wave, or a decision by second key between the waves, asked of it, with every answer and read it now
// holds, as many at once as there are cores. A failed write is a problem, never a finding: the next run loads from
// source what the entry would have answered, and misses what its reads would have held.
func (session *CacheSession) rewriteEarlier() {
	if len(session.earlier) > 0 && session.readAfterKeep("rewriteEarlier") {
		return
	}
	var group sync.WaitGroup
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	for saved, restored := range session.earlier {
		if saved.Types != nil && session.resolver.Unsure(saved.Types) {
			// Ran on a guess after its entry was written; the entry stands as written, and nothing of the guess
			// joins it.
			continue
		}
		held := entryDemand{answers: restored.answers, reads: restored.reads,
			readsIncomplete: restored.readsIncomplete}
		demanded := session.demandOf(saved, held)
		if answerCount(demanded.answers) == answerCount(restored.answers) &&
			len(demanded.reads) == len(restored.reads) && demanded.readsIncomplete == restored.readsIncomplete {
			continue
		}
		key, keyed := session.keys[saved]
		if !keyed {
			continue
		}
		slots <- struct{}{}
		group.Go(func() {
			defer func() { <-slots }()
			entry, record, stored := storedEntryAt(session.caching.Store, key)
			if !stored {
				return
			}
			// The answers are the body's, so the record rewritten holds a body of its own; a record of another build
			// naming the body it held reads a miss from then on, which checks that package from source once.
			entry.Answers = demanded.answers
			body, encodeError := encodeBody(entry)
			if encodeError != nil {
				session.problem(fmt.Sprintf("rewriting the entry of %s with its answers: %v", saved.ID, encodeError))
				return
			}
			rewritten := encodedEntry{body: body, record: entryRecord{Version: CacheFormatVersion,
				Body: contentDigest(body), Inline: body, ExportData: record.ExportData, Reads: demanded.reads,
				ReadsIncomplete: demanded.readsIncomplete}}
			if putError := session.putEntry(key, rewritten); putError != nil {
				session.problem(putError.Error())
			}
		})
	}
	group.Wait()
}

// answerCount is how many answers answers holds, over every kind.
func answerCount(answers []kindAnswers) int {
	count := 0
	for _, kind := range answers {
		count += len(kind.Answers)
	}
	return count
}
