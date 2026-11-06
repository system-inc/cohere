package check

import (
	"errors"
	"fmt"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Caching is the per-package findings cache a run reads and writes: the store, what every key shares, and the
// inputs a rule reads outside its pass, or nil for none.
type Caching struct {
	Store       *cache.Store
	Context     cache.Context
	ExtraInputs func(*packages.Package) ([]string, error)
}

// CacheCounts is what the cache did in one run, by package: every package an action reached is a hit or a miss,
// and a miss is written, unstorable, or neither, when an action on it did not finish or its write failed.
type CacheCounts struct {
	// On is whether the run read and wrote the cache. A run without it counts nothing.
	On bool

	// Hit is the packages restored from an entry, on which nothing ran.
	Hit int

	// Missed is the packages analyzed from source.
	Missed int

	// Written is the misses whose entry was written.
	Written int

	// Unstorable is the misses with a fact an entry cannot hold, an object with no objectpath or a value gob
	// refuses, which therefore got no entry.
	Unstorable int

	// WriteFailed is the misses whose entry could not be written, the first reason among Problems.
	WriteFailed int

	// Problems are what kept the cache from being read or written: keys that could not be computed, which leaves
	// every package a miss and none written, an entry that could not be encoded, and the first write that failed.
	// A problem never changes a finding.
	Problems []string
}

// cacheRun is one run's use of the cache: the keys, and the packages restored.
type cacheRun struct {
	caching *Caching
	known   *cacheAnalyzers
	keys    map[*packages.Package]cache.Key
	hits    map[*packages.Package]*restoredPackage
	counts  CacheCounts
}

// openCache computes every package's key and reads each one's entry, keeping those that decode whole as hits. A
// key that cannot be computed leaves the cache out of the run, with the reason among the counts' problems.
func openCache(
	caching *Caching,
	ruleOf map[*analysis.Analyzer]rule.Rule,
	analyzers []*analysis.Analyzer,
	analyzed []*packages.Package,
) *cacheRun {
	opened := &cacheRun{caching: caching, hits: map[*packages.Package]*restoredPackage{}, counts: CacheCounts{On: true}}
	known, knownError := newCacheAnalyzers(ruleOf, analyzers)
	if knownError != nil {
		opened.counts.Problems = append(opened.counts.Problems, knownError.Error())
		return opened
	}
	keys, keysError := cache.Keys(analyzed, caching.Context, caching.ExtraInputs)
	if keysError != nil {
		opened.counts.Problems = append(opened.counts.Problems, fmt.Sprintf("computing the keys: %v", keysError))
		return opened
	}
	opened.known, opened.keys = known, keys
	given := make(map[*packages.Package]bool, len(analyzed))
	for _, analyzedPackage := range analyzed {
		given[analyzedPackage] = true
	}
	for keyed, key := range keys {
		contents, stored := caching.Store.Get(key)
		if !stored {
			continue
		}
		restored, decodeError := decodeEntry(known, keyed, given[keyed], contents)
		if decodeError != nil {
			continue
		}
		opened.hits[keyed] = restored
	}
	return opened
}

// restored reports whether the run restores a package rather than analyzing it, for checker.Options.Restored.
func (opened *cacheRun) restored(restoring *packages.Package) bool {
	_, hit := opened.hits[restoring]
	return hit
}

// restore gives an action on a restored package its package's own facts from the entry, for
// checker.Options.Restore.
func (opened *cacheRun) restore(action *checker.Action) error {
	restored, held := opened.hits[action.Package].analyses[action.Analyzer]
	if !held {
		// decodeEntry holds every analyzer an action on the package can be for, so this is a checker that made an
		// action nobody expected, which reads as a crash rather than a rule that found nothing.
		return fmt.Errorf("restoring %s on %s: the cache entry holds nothing of it", action.Analyzer.Name,
			action.Package.ID)
	}
	for _, objectFact := range restored.objectFacts {
		action.RestoreObjectFact(objectFact.Object, objectFact.Fact)
	}
	for _, packageFact := range restored.packageFacts {
		action.RestorePackageFact(packageFact)
	}
	return nil
}

// findings is what an action on a restored package found, from its entry, and false for an action that ran.
func (opened *cacheRun) findings(action *checker.Action) ([]Finding, bool) {
	restored, hit := opened.hits[action.Package]
	if !hit {
		return nil, false
	}
	return restored.analyses[action.Analyzer].findings, true
}

// save counts every package an action reached as a hit or a miss, and writes the entry of each miss on which every
// action finished without an error.
func (opened *cacheRun) save(graph *checker.Graph, ruleOf map[*analysis.Analyzer]rule.Rule) {
	actionsOf := map[*packages.Package][]*checker.Action{}
	var order []*packages.Package
	for action := range graph.All() {
		if _, seen := actionsOf[action.Package]; !seen {
			order = append(order, action.Package)
		}
		actionsOf[action.Package] = append(actionsOf[action.Package], action)
	}
	for _, reached := range order {
		if opened.restored(reached) {
			opened.counts.Hit++
			continue
		}
		opened.counts.Missed++
		key, keyed := opened.keys[reached]
		if !keyed || !finishedClean(actionsOf[reached]) {
			continue
		}
		entry, encodeError := encodeEntry(opened.known, ruleOf, actionsOf[reached])
		if errors.Is(encodeError, errorUnstorable) {
			opened.counts.Unstorable++
			continue
		}
		if encodeError != nil {
			opened.counts.Problems = append(opened.counts.Problems, encodeError.Error())
			continue
		}
		if putError := opened.caching.Store.Put(key, entry); putError != nil {
			if opened.counts.WriteFailed == 0 {
				opened.counts.Problems = append(opened.counts.Problems, putError.Error())
			}
			opened.counts.WriteFailed++
			continue
		}
		opened.counts.Written++
	}
}

// finishedClean reports whether every action finished without an error, which an entry needs: an action that
// failed on its own or because a prerequisite did has no analysis worth keeping.
func finishedClean(actions []*checker.Action) bool {
	for _, action := range actions {
		if action.Err != nil {
			return false
		}
	}
	return true
}
