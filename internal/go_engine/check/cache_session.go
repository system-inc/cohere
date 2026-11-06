package check

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Caching is the per-package findings cache a run reads and writes: the store, what every key shares but the
// loader, which the load names, and the inputs a rule reads outside its pass, or nil for none.
type Caching struct {
	Store       *cache.Store
	Context     cache.Context
	ExtraInputs func(*packages.Package) ([]string, error)
}

// CacheCounts is what the cache did in one run, by package: every package the load reached is a hit or a miss, and
// a miss is written, unstorable, or neither, when it had a load error, an action on it did not finish, or its write
// failed.
type CacheCounts struct {
	// On is whether the run read and wrote the cache. A run without it counts nothing.
	On bool

	// Hit is the packages restored from an entry, which nothing parsed, type-checked or analyzed.
	Hit int

	// Missed is the packages checked and analyzed from source.
	Missed int

	// Written is the misses whose entry was written.
	Written int

	// Unstorable is the misses an entry cannot hold, a fact gob refuses or types export data cannot carry, which
	// therefore got no entry.
	Unstorable int

	// FactsDropped is the facts the written entries left out because nothing outside their package can name their
	// object, which has no objectpath (see encodeEntry).
	FactsDropped int

	// FactsSkipped is the restored facts whose object the package's export data does not hold (see restore).
	FactsSkipped int

	// WriteFailed is the misses whose entry could not be written, the first reason among Problems.
	WriteFailed int

	// Problems are what kept the cache from being read or written: keys that could not be computed, which leaves
	// every package a miss and none written, an entry that could not be encoded, and the first write that failed.
	// A problem never changes a finding.
	Problems []string
}

// CacheSession is one run's use of the findings cache, from the load to the save. The load asks it which packages it
// holds once go list has run (Restore, as a load.Restorer), so those are never parsed; Run then restores their
// findings and facts, analyzes the rest, and writes an entry for each package that missed.
type CacheSession struct {
	caching *Caching
	ruleOf  map[*analysis.Analyzer]rule.Rule
	known   *cacheAnalyzers

	// opened is set once Restore has run; keys and hits are read only after.
	opened bool
	keys   map[*packages.Package]cache.Key
	hits   map[*packages.Package]*restoredPackage

	// given is the packages to analyze, as the load named them.
	given map[*packages.Package]bool

	counts       CacheCounts
	factsSkipped atomic.Int64

	// exportDataReads counts the export data read from the store, which the load asks only of a package something
	// that missed imports.
	exportDataReads atomic.Int64

	// problemsLock guards counts.Problems, which the load's goroutines never write but Run's may.
	problemsLock sync.Mutex
}

// NewCacheSession is a session for the rules a run will run. An analyzer the cache cannot tell from another leaves
// the cache out of the run, which reads and writes nothing then, with the reason among the counts' problems.
func NewCacheSession(caching *Caching, rules []rule.Rule) *CacheSession {
	session := &CacheSession{
		caching: caching,
		ruleOf:  map[*analysis.Analyzer]rule.Rule{},
		counts:  CacheCounts{On: true},
	}
	analyzers := make([]*analysis.Analyzer, 0, len(rules))
	for _, enabled := range rules {
		session.ruleOf[enabled.Analyzer] = enabled
		analyzers = append(analyzers, enabled.Analyzer)
	}
	known, knownError := newCacheAnalyzers(session.ruleOf, analyzers)
	if knownError != nil {
		session.problem(knownError.Error())
		return session
	}
	session.known = known
	return session
}

// Restore computes the key of every package the load reached, reads each one's entry, and returns those that decode
// whole and hold every analyzer the run can ask of them. A package with a list error is always analyzed again. Which
// analyzers a package can be asked for depends on what misses: a package to analyze is asked for its rules'
// findings, and any package a missed package to analyze imports, directly or not, is asked for its facts, since a
// fact analyzer on that importer runs on everything it imports. So a hit that falls short becomes a miss, which can
// ask more of the rest, until nothing changes.
func (session *CacheSession) Restore(
	loader string,
	reachable []*packages.Package,
	analyzed []*packages.Package,
) map[*packages.Package]*load.Restored {
	session.opened = true
	session.hits = map[*packages.Package]*restoredPackage{}
	session.given = make(map[*packages.Package]bool, len(analyzed))
	for _, analyzedPackage := range analyzed {
		session.given[analyzedPackage] = true
	}
	if session.known == nil {
		return nil
	}
	keyContext := session.caching.Context
	keyContext.Build = append(append([]string{}, keyContext.Build...), "loader="+loader)
	keys, keysError := cache.Keys(reachable, keyContext, session.caching.ExtraInputs)
	if keysError != nil {
		session.problem(fmt.Sprintf("computing the keys: %v", keysError))
		return nil
	}
	session.keys = keys
	for _, keyed := range reachable {
		key, hasKey := keys[keyed]
		if !hasKey || len(keyed.Errors) > 0 {
			continue
		}
		contents, stored := session.caching.Store.Get(key)
		if !stored {
			continue
		}
		restored, decodeError := decodeEntry(session.known, keyed, contents, session.exportDataReader(keyed, key))
		if decodeError != nil {
			continue
		}
		session.hits[keyed] = restored
	}
	// Each pass makes a hit that falls short a miss, which can only ask more of the rest.
	dropping := true
	for dropping {
		dropping = session.dropShortHits(reachable)
	}
	restoring := make(map[*packages.Package]*load.Restored, len(session.hits))
	for hit, restored := range session.hits {
		restoring[hit] = restored.types
	}
	return restoring
}

// Kept is what a run kept in the session's store under key beside the entries, go list's answer for
// load.ListingStore and gofmt's verdicts among it.
func (session *CacheSession) Kept(key [sha256.Size]byte) ([]byte, bool) {
	return session.caching.Store.Get(cache.Key(key))
}

// Keep keeps contents under key in the session's store.
func (session *CacheSession) Keep(key [sha256.Size]byte, contents []byte) error {
	return session.caching.Store.Put(cache.Key(key), contents)
}

// KeyFor is the key of what a run keeps under label that depends on the engine itself and the build, as gofmt's
// verdicts depend on the gofmt the engine links: another engine or build never reads it.
func (session *CacheSession) KeyFor(label string) [sha256.Size]byte {
	digest := sha256.New()
	for _, part := range append([]string{label, session.caching.Context.FormatVersion,
		session.caching.Context.EngineIdentity}, session.caching.Context.Build...) {
		// Ignored: a hash.Hash never returns an error from Write, and the zero byte ends each part.
		_, _ = digest.Write(append([]byte(part), 0))
	}
	return [sha256.Size]byte(digest.Sum(nil))
}

// exportDataReader reads the export data of the package under key from the store, for the load.
func (session *CacheSession) exportDataReader(keyed *packages.Package, key cache.Key) func() ([]byte, error) {
	return func() ([]byte, error) {
		session.exportDataReads.Add(1)
		exportData, stored := session.caching.Store.Get(exportDataKey(key))
		if !stored {
			return nil, fmt.Errorf("the store holds no export data for %s", keyed.ID)
		}
		return exportData, nil
	}
}

// dropShortHits makes a miss of every hit that does not hold what the run can ask of it, and reports whether there
// was one.
func (session *CacheSession) dropShortHits(reachable []*packages.Package) bool {
	askedForFacts := map[*packages.Package]bool{}
	var reach func(importer *packages.Package)
	reach = func(importer *packages.Package) {
		for _, imported := range importer.Imports {
			if !askedForFacts[imported] {
				askedForFacts[imported] = true
				reach(imported)
			}
		}
	}
	for _, each := range reachable {
		if _, hit := session.hits[each]; session.given[each] && !hit {
			reach(each)
		}
	}
	dropped := false
	for hit, restored := range session.hits {
		// The second clause is reachable: a package the load reaches that nothing analyzes or imports, as plain p
		// beside "p [p.test]" when nothing imports p, gets no action and is stored without facts. Once a root that
		// missed imports it, a fact analyzer on that root runs on it, so it must miss and be analyzed again
		// (TestAPackageStoredWithoutFactsMissesOnceARootImportsIt).
		short := session.given[hit] && !covers(restored, session.known.rules) ||
			askedForFacts[hit] && !covers(restored, session.known.facts)
		if short {
			delete(session.hits, hit)
			dropped = true
		}
	}
	return dropped
}

// restored reports whether the run restores a package rather than analyzing it, for checker.Options.Restored.
func (session *CacheSession) restored(restoring *packages.Package) bool {
	_, hit := session.hits[restoring]
	return hit
}

// restore gives an action on a restored package its package's own facts from the entry, for
// checker.Options.Restore. Each object fact names its object by objectpath, looked up in the package's types, which
// come from its export data. A fact whose object the export data does not hold is skipped and counted: export data
// is exactly what an importer can see of the package, so no importer can name that object, and no importer's
// finding can read its fact.
func (session *CacheSession) restore(action *checker.Action) error {
	restored, held := session.hits[action.Package].analyses[action.Analyzer]
	if !held {
		// dropShortHits keeps no hit that falls short of an analyzer an action on it can be for, so this is a
		// checker that made an action nobody expected, which reads as a crash rather than a rule that found nothing.
		return fmt.Errorf("restoring %s on %s: the cache entry holds nothing of it", action.Analyzer.Name,
			action.Package.ID)
	}
	if action.Package.Types == nil {
		return fmt.Errorf("restoring %s on %s: the load gave the package no types", action.Analyzer.Name,
			action.Package.ID)
	}
	for _, objectFact := range restored.objectFacts {
		object, objectError := objectpath.Object(action.Package.Types, objectFact.path)
		if objectError != nil {
			session.factsSkipped.Add(1)
			continue
		}
		action.RestoreObjectFact(object, objectFact.fact)
	}
	for _, packageFact := range restored.packageFacts {
		action.RestorePackageFact(packageFact)
	}
	return nil
}

// findingsOf is what one analyzer found on a restored package, from its entry, and false for a package that missed
// or a run without the cache.
func (session *CacheSession) findingsOf(
	analyzer *analysis.Analyzer,
	analyzedPackage *packages.Package,
) ([]Finding, bool) {
	if session == nil {
		return nil, false
	}
	restored, hit := session.hits[analyzedPackage]
	if !hit {
		return nil, false
	}
	return restored.analyses[analyzer].findings, true
}

// filesOf is the stored walk of a restored package Analyze was given, and false for one that missed or a run without
// the cache.
func (session *CacheSession) filesOf(analyzedPackage *packages.Package) ([]storedFile, bool) {
	if session == nil {
		return nil, false
	}
	restored, hit := session.hits[analyzedPackage]
	if !hit {
		return nil, false
	}
	return restored.files, true
}

// save counts every package the load reached as a hit or a miss, and writes the entry of each miss with no load
// error on which every action finished without one, from its actions, its types and what the load withheld of it.
// The entries are encoded and written as many at once as there are cores: writing a package's export data is most of
// a cold run's save, and each package's is its own.
func (session *CacheSession) save(
	graph *checker.Graph,
	withheld map[*packages.Package]load.PackageWithheld,
	files map[*packages.Package][]storedFile,
) {
	session.counts.FactsSkipped = int(session.factsSkipped.Load())
	actionsOf := map[*packages.Package][]*checker.Action{}
	for action := range graph.All() {
		actionsOf[action.Package] = append(actionsOf[action.Package], action)
	}
	var saving []*packages.Package
	for keyed := range session.keys {
		if session.restored(keyed) {
			session.counts.Hit++
			continue
		}
		session.counts.Missed++
		if len(keyed.Errors) == 0 && !keyed.IllTyped && finishedClean(actionsOf[keyed]) {
			saving = append(saving, keyed)
		}
	}
	var countsLock sync.Mutex
	var group sync.WaitGroup
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	for _, saved := range saving {
		group.Add(1)
		slots <- struct{}{}
		go func() {
			defer group.Done()
			defer func() { <-slots }()
			entry, exportData, dropped, encodeError := encodeEntry(session.known, session.ruleOf, saved,
				actionsOf[saved], withheld[saved], files[saved])
			var putError error
			// The export data goes first, so an entry is never in the store before what it needs to be read.
			if encodeError == nil && exportData != nil {
				putError = session.caching.Store.Put(exportDataKey(session.keys[saved]), exportData)
			}
			if encodeError == nil && putError == nil {
				putError = session.caching.Store.Put(session.keys[saved], entry)
			}
			countsLock.Lock()
			defer countsLock.Unlock()
			switch {
			case errors.Is(encodeError, errorUnstorable):
				session.counts.Unstorable++
			case encodeError != nil:
				session.problem(encodeError.Error())
			case putError != nil:
				if session.counts.WriteFailed == 0 {
					session.problem(putError.Error())
				}
				session.counts.WriteFailed++
			default:
				session.counts.FactsDropped += dropped
				session.counts.Written++
			}
		}()
	}
	group.Wait()
}

// problem records one reason the cache was not read or written.
func (session *CacheSession) problem(reason string) {
	session.problemsLock.Lock()
	defer session.problemsLock.Unlock()
	session.counts.Problems = append(session.counts.Problems, reason)
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
