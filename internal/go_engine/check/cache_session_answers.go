package check

import (
	"bytes"
	"cmp"
	"encoding/gob"
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
	// never restored, and rounds how many times a pass asked for new ones, each a load and a lint of its own.
	reloaded map[string]bool
	rounds   int
}

// know adds each package of known whose types are set, which the run's questions may reach.
func (session *CacheSession) know(known ...*packages.Package) {
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
	session.answering.lock.Lock()
	defer session.answering.lock.Unlock()
	return session.answering.byTypes[packageTypes]
}

// Answers is what the run can read of the answers of the package whose types are packageTypes, for the resolver: its
// entry's, for a package restored from the binary's table, complete in every kind its release computed, or restored
// from the store by its own first key, or loaded from source to answer a question, whose entry under that key the run
// did not restore. An entry an alias or the second key led to holds the answers of other inputs than the package's
// own, which the early cutoff does not compare (#w7zmvat wave 3b), so it gives none. It is nil when there are none.
func (session *CacheSession) Answers(packageTypes *types.Package) *fact_resolver.Answers {
	asked := session.packageOf(packageTypes)
	if asked == nil {
		return nil
	}
	var held []kindAnswers
	if restored := session.hits[asked]; restored != nil {
		if key, keyed := session.keys[asked]; session.fromBinary[asked] || keyed && session.stored[asked] == key {
			held = restored.answers
		}
	} else {
		held = session.previous[asked]
	}
	if len(held) == 0 {
		return nil
	}
	answers := &fact_resolver.Answers{
		Values:   map[string]map[objectpath.Path][]byte{},
		Complete: map[string]bool{},
	}
	addAnswers(answers, held)
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
	for _, identifier := range reloads {
		if !session.answering.reloaded[identifier] {
			session.answering.reloaded[identifier] = true
			added = true
		}
	}
	if added {
		session.answering.rounds++
	}
	return added
}

// reloading reports whether the run checks the package from source to answer a question, so no load restores it.
func (session *CacheSession) reloading(restoring *packages.Package) bool {
	session.answering.lock.Lock()
	defer session.answering.lock.Unlock()
	return session.answering.reloaded[restoring.ID]
}

// reloadsOf is the IDs of the packages a question reached restored, with no answer stored, sorted.
func (session *CacheSession) reloadsOf(resolver *fact_resolver.Resolver) []string {
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
// its entry under its own first key holds, which the run did not restore, so they answer without a build and stay in
// the entry the run writes for it.
func (session *CacheSession) readPrevious(reachable []*packages.Package) {
	session.previous = map[*packages.Package][]kindAnswers{}
	if session.caching.Store == nil {
		return
	}
	for _, reloaded := range reachable {
		key, keyed := session.keys[reloaded]
		if !keyed || !session.reloading(reloaded) {
			continue
		}
		contents, stored := session.caching.Store.Get(key)
		if !stored {
			continue
		}
		var entry cacheEntry
		if decodeError := gob.NewDecoder(bytes.NewReader(contents)).Decode(&entry); decodeError != nil ||
			entry.Version != CacheFormatVersion {
			continue
		}
		session.previous[reloaded] = entry.Answers
	}
}

// answersOf is the answers the entry of saved holds: those the resolver computed whole, for a standard-library package
// the run collects for the binary's table, and otherwise those its entry held before, for a package loaded from source
// to answer a question, with every answer the run computed in it.
func (session *CacheSession) answersOf(saved *packages.Package) []kindAnswers {
	if session.resolver == nil || saved.Types == nil {
		return nil
	}
	if session.collects(saved) {
		if home := session.resolver.Package(saved.Types); home != nil {
			return sortedAnswers(session.resolver.Whole(home), true)
		}
	}
	merged := session.resolver.Recorded(saved.Types)
	complete := map[string]bool{}
	for _, kind := range session.previous[saved] {
		if merged[kind.Kind] == nil {
			merged[kind.Kind] = map[objectpath.Path][]byte{}
		}
		for _, answer := range kind.Answers {
			merged[kind.Kind][answer.Path] = answer.Value
		}
		complete[kind.Kind] = complete[kind.Kind] || kind.Complete
	}
	sorted := sortedAnswers(merged, false)
	for index := range sorted {
		sorted[index].Complete = complete[sorted[index].Kind]
	}
	return sorted
}

// rewriteEarlier writes again the entry of each package the first wave saved whose answers grew since, as the second
// wave asked of it, with every answer it now holds, as many at once as there are cores. A failed write is a problem,
// never a finding: the next run loads from source what the entry would have answered.
func (session *CacheSession) rewriteEarlier() {
	var group sync.WaitGroup
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	for saved, restored := range session.earlier {
		answers := session.answersOf(saved)
		if answerCount(answers) == answerCount(restored.answers) {
			continue
		}
		key, keyed := session.keys[saved]
		if !keyed {
			continue
		}
		slots <- struct{}{}
		group.Go(func() {
			defer func() { <-slots }()
			contents, stored := session.caching.Store.Get(key)
			if !stored {
				return
			}
			var entry cacheEntry
			if decodeError := gob.NewDecoder(bytes.NewReader(contents)).Decode(&entry); decodeError != nil {
				session.problem(fmt.Sprintf("rewriting the entry of %s with its answers: %v", saved.ID, decodeError))
				return
			}
			entry.Answers = answers
			var encoded bytes.Buffer
			if encodeError := gob.NewEncoder(&encoded).Encode(entry); encodeError != nil {
				session.problem(fmt.Sprintf("rewriting the entry of %s with its answers: %v", saved.ID, encodeError))
				return
			}
			if putError := session.caching.Store.Put(key, encoded.Bytes()); putError != nil {
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
