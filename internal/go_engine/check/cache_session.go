package check

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Caching is the per-package findings cache a run reads and writes: the store, what every key shares but the
// loader, which the load names, the inputs a rule reads outside its pass, or nil for none, the standard library's
// entries the binary carries, or nil for none, and the digests of files the run already hashed, which the keys read
// rather than hash again (see cache.Keys), the zero value none. Under `--no-cache` it has no store, and the session
// reads the binary's entries alone: it keys only the standard library, restores only from the table, and writes
// nothing.
type Caching struct {
	Store           *cache.Store
	Context         cache.Context
	ExtraInputs     func(*packages.Package) ([]string, error)
	StandardLibrary *StandardLibraryCaching
	Files           cache.FileDigests

	// Held is the bytes the load parsed in place of a file's on disk, which the keys hash in their place (see
	// cache.Keys), nil for a run whose load read every file as it is on disk. The editor's save sets it, whose one file
	// is the buffer, so its entries are keyed by what it checked rather than by the file the editor has not saved.
	Held func(file string) ([]byte, bool)

	// Pool is the run's one set of slots, which every load and check of the run shares (#ckm0hsp): the session's own
	// work on every core, reading its hits, saving its misses, deciding a restore from the host, runs under its slots
	// (fanOut, typesHoldFunction). Nil, that work takes a goroutine per core of its own.
	Pool *bounded.Pool

	// AnswersInPart names, by import path, the packages whose entry the run writes with only the answers its
	// questions computed in them, never whole, as an entry from a build before format 10 holds them: an entry a later
	// run's question can find no answer in, so that run reloads the package (see Reload). Only a test of the reload
	// sets it.
	AnswersInPart map[string]bool
}

// StandardLibraryCaching is the standard library's entries as a run reads them from the binary: the table, and what
// every standard-library key shares but the fact analyzers and the loader, which the session names. A table that is
// nil holds nothing, as in a run that only collects entries. Collected, when it is not nil, gains each
// standard-library package the run saves to the store, by its standard-library key, for StandardLibraryTableOf; a run
// under `--no-cache` saves nothing, so it collects nothing.
type StandardLibraryCaching struct {
	Table     *cache.StandardLibraryTable
	Context   cache.StandardLibraryContext
	Collected map[cache.Key]StandardLibraryRunEntry
}

// StandardLibraryRunEntry is one standard-library package a run saved: its import path, its entry and export data as
// saveMisses encoded them, which still name this machine's files, and its reads of other packages as persistReads
// persisted them once the run's reads were whole (PersistCollectedReads), nil until then and for reads that cannot be
// persisted.
type StandardLibraryRunEntry struct {
	PackagePath string
	Entry       []byte
	ExportData  []byte
	Reads       []byte
}

// CacheCounts is what the cache did in one run, by package: every package the load reached is a hit, restored from
// the store or from the binary, or a miss, and a miss is written, unstorable, or neither, when it had a load error, an
// action on it did not finish, or its write failed.
type CacheCounts struct {
	// On is whether the run read and wrote the store. A run without it counts nothing but FromBinary, FactsSkipped
	// of what it restored, and Problems.
	On bool

	// Hit is the packages restored from an entry in the store, which nothing analyzed, and on the load from source
	// nothing parsed or type-checked unless the second key restored it (see SecondKeyHit).
	Hit int

	// FromBinary is the standard-library packages restored from the entries the binary carries, which nothing
	// analyzed or parsed, apart from Hit. Under `--no-cache` it is the one count a run can have.
	FromBinary int

	// SecondKeyHit is the hits restored by their second key, once their dependencies' analyses came out as before:
	// the load had checked them, since only an analysis of what is below them could say they would hit.
	SecondKeyHit int

	// Waves is how many times the run analyzed packages: at most two, the packages whose own inputs moved, and then
	// everything else that missed (see firstWave), and 0 when every package hit.
	Waves int

	// Missed is the packages checked and analyzed from source.
	Missed int

	// Written is the misses whose entry was written.
	Written int

	// WrittenThrough is, in an extra build, the packages restored from the host's run whose entry the build wrote
	// under its own key once their restore had settled (writeThrough), so its next run hits them in its own store.
	// They are hits, never misses, so Written leaves them out.
	WrittenThrough int

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

	// Reloads is the packages the run loaded from source, though the cache held them, because a question of a fact
	// computed on demand reached one restored and no store answered it (#w7zmvat wave 3), or, in an extra build, a
	// package restored from the host's run was declined (#b2t9xmf), over every load of the run, and ReloadRounds how
	// many times the run loaded and linted again to do so. ReloadRoundsBy counts each round under each reason its
	// packages were loaded again for: missingAnswer or hostDecline.
	Reloads        int
	ReloadRounds   int
	ReloadRoundsBy map[string]int

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

	// own is each package's own digest (cache.Keyed), which its second key starts from.
	own map[*packages.Package]cache.Key

	// files are the digests of every file the last Restore keyed (cache.Keyed's Files), for Files.
	files cache.FileDigests

	// stored is where each hit's entry is: under its key, or the key an alias led to.
	stored map[*packages.Package]cache.Key

	// secondKeyHits are the packages the second key restored.
	secondKeyHits map[*packages.Package]bool

	// earlier are the packages the first wave analyzed and saved, which the second wave restores from the entries just
	// encoded rather than analyzing them again.
	earlier map[*packages.Package]*restoredPackage

	// outputs are the output digests of the packages this run analyzed and saved; a hit's is in its entry.
	outputs map[*packages.Package]cache.Key

	// closures memoizes closureOf, and is cleared whenever outputs grow.
	closures map[*packages.Package]closureDigest

	// attempted are the misses a wave tried to save, so no miss is saved, or counted, twice.
	attempted map[*packages.Package]bool

	// knownFiles is every file of every package the load reached, for positionDependent, made once.
	knownFiles *knownFiles

	// given is the packages to analyze, as the load named them.
	given map[*packages.Package]bool

	// standardLibraryKeys are the standard-library packages' keys, by which the binary's table holds them, nil when the
	// run reads no table and collects nothing.
	standardLibraryKeys map[*packages.Package]cache.Key

	// fromBinary are the hits restored from the binary's table, which are never written to the store, and binaryHits
	// what the host's session of a run with extra builds holds of each, which it keeps for them (keepBinaryHit).
	fromBinary map[*packages.Package]bool
	binaryHits map[*packages.Package]binaryHit

	counts       CacheCounts
	factsSkipped atomic.Int64

	// exportDataReads counts the export data read from the store, which the load asks only of a package something
	// that missed imports.
	exportDataReads atomic.Int64

	// problemsLock guards counts.Problems, which the load's goroutines never write but Run's may.
	problemsLock sync.Mutex

	// answering is what the resolver's questions read of the session (see cache_session_answers.go), and resolver
	// the last run's, whose answers each entry written keeps. previous are the answers of each package loaded from
	// source to answer a question, from its entry under its own key, which the run did not restore.
	answering answering
	resolver  *fact_resolver.Resolver
	previous  map[*packages.Package]entryDemand

	// bodiless is the last run's load's record of what each package checked from source declares without a body,
	// which each entry written keeps (see load.Bodiless), nil for none.
	bodiless *load.Bodiless

	// trusted are the hits whose entry's findings and answers hold for the package as the run finds it (see
	// cache_session_reads.go), and byIdentifier every package the load reached, by ID, which a read names its owner by.
	trusted      map[*packages.Package]bool
	byIdentifier map[string]*packages.Package

	// keepsForBuilds is set on the host's session of a run with extra builds (KeepForBuilds), whose every Restore keys
	// the load, a store or none, into keptKeys, for the portable digests and the reads' owners of the entries it keeps
	// for them (keepForBuilds). written are the misses whose entry this run wrote to the store, which it reads back
	// rather than encode again, and buildKeys the keys a run without a store names an entry's reads' owners by
	// (readsOf).
	keepsForBuilds bool
	keptKeys       *cache.Keyed
	written        map[*packages.Package]bool
	buildKeys      map[*packages.Package]cache.Key

	// keeping is the time the host's run spent keeping its packages for the extra builds over every load: the keys a
	// run without a store computes only to keep them, and keepForBuilds.
	keeping time.Duration

	// host is the restore from the host's run of an extra build's session, nil for any other (see RestoreFromHost).
	host *hostState

	// kept is set once keepForBuilds has let go of the resolver and the answer memo, and cleared by the next Restore,
	// which starts both afresh. A read of either while it is set is a problem the run reports (readAfterKeep), never
	// a quiet empty answer (#px1yxq8).
	kept atomic.Bool
}

// NewCacheSession is a session for the rules a run will run, and the report analyzers it will run beside them (see
// Options.Report), which a package's entry must hold to be restored. An analyzer the cache cannot tell from another
// leaves the cache out of the run, which reads and writes nothing then, with the reason among the counts' problems.
func NewCacheSession(caching *Caching, rules []rule.Rule, reports ...*analysis.Analyzer) *CacheSession {
	session := &CacheSession{
		caching: caching,
		ruleOf:  map[*analysis.Analyzer]rule.Rule{},
		counts:  CacheCounts{On: caching.Store != nil},
	}
	analyzers := make([]*analysis.Analyzer, 0, len(rules))
	for _, enabled := range rules {
		session.ruleOf[enabled.Analyzer] = enabled
		analyzers = append(analyzers, enabled.Analyzer)
	}
	known, knownError := newCacheAnalyzers(session.ruleOf, analyzers, reports)
	if knownError != nil {
		session.problem(knownError.Error())
		return session
	}
	session.known = known
	return session
}

// Restore computes the key of every package the load reached, reads each one's entry, and returns those that decode
// whole and hold every analyzer the run can ask of them. A standard-library package is looked up in the binary's
// table first (see restoreFromBinary), and in the store only when the table holds none; without a store it is
// looked up in the table alone, and nothing else is keyed. A package with a list error is always analyzed again. Which
// analyzers a package can be asked for depends on what misses: a package to analyze is asked for its rules'
// findings, and any package a missed package to analyze imports, directly or not, is asked for its facts, since a
// fact analyzer on that importer runs on everything it imports. So a hit that falls short becomes a miss, which can
// ask more of the rest, until nothing changes. A package the run reloads to answer a question (see Reload) is never
// restored, from the store or the table.
func (session *CacheSession) Restore(
	loader string,
	reachable []*packages.Package,
	analyzed []*packages.Package,
) map[*packages.Package]*load.Restored {
	session.opened = true
	// Every map starts empty, the keys and the files' digests too, since the engine restores a second load when the
	// long-line wrap wrote a file, and keys this restore could not compute must not leave the first load's in their
	// place, which the lint phase would save entries under, for packages it never analyzed, nor digests of the bytes
	// the wrap replaced, which an extra build would read.
	session.keys, session.own, session.files = nil, nil, cache.FileDigests{}
	session.hits = map[*packages.Package]*restoredPackage{}
	session.stored = map[*packages.Package]cache.Key{}
	session.secondKeyHits = map[*packages.Package]bool{}
	session.earlier = map[*packages.Package]*restoredPackage{}
	session.outputs = map[*packages.Package]cache.Key{}
	session.closures = map[*packages.Package]closureDigest{}
	session.attempted = map[*packages.Package]bool{}
	session.written = map[*packages.Package]bool{}
	session.buildKeys = nil
	session.standardLibraryKeys = nil
	session.fromBinary = map[*packages.Package]bool{}
	session.binaryHits = map[*packages.Package]binaryHit{}
	session.given = make(map[*packages.Package]bool, len(analyzed))
	for _, analyzedPackage := range analyzed {
		session.given[analyzedPackage] = true
	}
	// The packages questions reach are this load's; the reloads pending stay, for every load of the run. The last run's
	// resolver, its syntax and program of every package it learned, is let go before this load parses anything.
	session.resolver = nil
	session.answering.lock.Lock()
	session.answering.byTypes = nil
	session.answering.lock.Unlock()
	session.kept.Store(false)
	// The counts are of the run over this load, the one whose result stands, which is the last: a load again to check
	// a package from source to answer a question drops the pass before it (see Reload), and its packages that pass
	// saved are this one's hits. The problems stay, since every one of them was met.
	session.counts = CacheCounts{On: session.counts.On, Problems: session.counts.Problems}
	session.factsSkipped.Store(0)
	if session.known == nil {
		return nil
	}
	// The digests of every file Keys hashed, which the standard library's keys read rather than hash again; none
	// without a store, which keys only the standard library.
	var hashed cache.FileDigests
	var keyed *cache.Keyed
	session.keptKeys = nil
	// The host's run keys its load here even without a store, as an extra build's restore keys its own (see
	// restoreFromHost): only before any package has its syntax do the extra inputs read what both builds read alike.
	if session.caching.Store != nil || session.keepsForBuilds {
		keysStarted := time.Now()
		keyContext := session.caching.Context
		keyContext.Build = append(append([]string{}, keyContext.Build...), "loader="+loader)
		var keysError error
		keyed, keysError = cache.Keys(reachable, keyContext, session.caching.ExtraInputs, session.caching.Files,
			session.caching.Held)
		// Keys a run with a store computes anyway; without one, they are the keep's alone.
		if session.caching.Store == nil {
			session.keeping += time.Since(keysStarted)
		}
		if keysError != nil {
			session.problem(fmt.Sprintf("computing the keys: %v", keysError))
			return nil
		}
		if session.keepsForBuilds {
			session.keptKeys = keyed
		}
	}
	if session.caching.Store != nil {
		session.keys, session.own, hashed = keyed.Keys, keyed.Own, keyed.Files
		session.files = keyed.Files
	}
	session.restoreFromBinary(loader, reachable, hashed)
	session.readHits(reachable)
	session.readPrevious(reachable)
	session.holdAliasHits(reachable)
	// Each pass makes a hit that falls short a miss, which can only ask more of the rest.
	dropping := true
	for dropping {
		dropping = session.dropShortHits(reachable)
	}
	// What the build's own store holds is restored by the build's key, exactly; only what it does not hold is restored
	// from the host's run, by what the package read (#b2t9xmf).
	session.restoreFromHost(loader, reachable, keyed)
	for dropping = true; dropping; {
		dropping = session.dropShortHits(reachable)
	}
	session.dropShortHostHits()
	restoring := make(map[*packages.Package]*load.Restored, len(session.hits))
	for hit, restored := range session.hits {
		restoring[hit] = restored.types
	}
	return restoring
}

// Files are the digests of every file the last Restore keyed, those its Caching gave it included, and none before a
// Restore, after one whose keys failed, or for a session without a store, which keys only the standard library.
func (session *CacheSession) Files() cache.FileDigests {
	return session.files
}

// HasStore reports whether the session reads and writes a store, which a session under `--no-cache`, reading the
// binary's entries alone, does not: such a run keeps neither go list's answer nor gofmt's verdicts.
func (session *CacheSession) HasStore() bool {
	return session.caching.Store != nil
}

// Kept is what a run kept in the session's store under key beside the entries, go list's answer for
// load.ListingStore and gofmt's verdicts among it, and nothing for a session without a store.
func (session *CacheSession) Kept(key [sha256.Size]byte) ([]byte, bool) {
	if session.caching.Store == nil {
		return nil, false
	}
	return session.caching.Store.Get(cache.Key(key))
}

// Keep keeps contents under key in the session's store, and refuses for a session without one.
func (session *CacheSession) Keep(key [sha256.Size]byte, contents []byte) error {
	if session.caching.Store == nil {
		return errors.New("keeping what the run read: the run has no store, under --no-cache")
	}
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

// readHits makes a hit, as hitAt would, of every package among reachable that has a key, no list error, no entry from
// the binary, and no reload pending (see Reload), when the store holds a whole entry of it. The entries are read and
// decoded as many at once as there are cores, since on a warm run of cohere's own root reading 1,167 of them one at a
// time was 119ms of a 268ms
// restore. A worker writes only its own slot of what it read: what it calls reads files, the session's analyzers and
// its store, none of which a restore writes, so the session's maps are written here alone, once every worker is done,
// in reachable's order.
func (session *CacheSession) readHits(reachable []*packages.Package) {
	var reading []*packages.Package
	var keys []cache.Key
	for _, restoring := range reachable {
		key, hasKey := session.keys[restoring]
		if !hasKey || len(restoring.Errors) > 0 || session.fromBinary[restoring] || session.reloading(restoring) {
			continue
		}
		reading = append(reading, restoring)
		keys = append(keys, key)
	}
	read := make([]storeHit, len(reading))
	session.fanOut(len(reading), func(index int) {
		read[index] = session.readHit(reading[index], keys[index])
	})
	for index, restoring := range reading {
		if read[index].restored != nil {
			session.hits[restoring] = read[index].restored
			session.stored[restoring] = read[index].storedKey
		}
	}
}

// storeHit is one package's entry as readHit read it: the entry decoded whole and the key it is stored under, or a nil
// restored for a miss.
type storeHit struct {
	restored  *restoredPackage
	storedKey cache.Key
}

// hitAt makes a hit of restoring when the store holds a whole entry under key, directly or through an alias, and
// reports whether it did. Which analyzers the entry must hold is dropShortHits' to judge.
func (session *CacheSession) hitAt(restoring *packages.Package, key cache.Key) bool {
	read := session.readHit(restoring, key)
	if read.restored == nil {
		return false
	}
	session.hits[restoring] = read.restored
	session.stored[restoring] = read.storedKey
	return true
}

// readHit is restoring's entry under key, directly or through an alias, decoded whole, and a nil restored when the
// store holds none that decodes. It writes nothing of the session's, so readHits runs it on many packages at once.
func (session *CacheSession) readHit(restoring *packages.Package, key cache.Key) storeHit {
	contents, storedKey, found := session.entryAt(key)
	if !found {
		return storeHit{}
	}
	restored, decodeError := decodeEntry(session.known, restoring, contents,
		session.exportDataReader(restoring, storedKey))
	if decodeError != nil {
		return storeHit{}
	}
	return storeHit{restored: restored, storedKey: storedKey}
}

// entryAt is the entry under key, or, when there is none, the entry the alias under key names, with the key it is
// stored under.
func (session *CacheSession) entryAt(key cache.Key) ([]byte, cache.Key, bool) {
	if contents, stored := session.caching.Store.Get(key); stored {
		return contents, key, true
	}
	target, aliased := session.caching.Store.Get(aliasKey(key))
	if !aliased || len(target) != len(cache.Key{}) {
		return nil, cache.Key{}, false
	}
	storedKey := cache.Key(target)
	contents, stored := session.caching.Store.Get(storedKey)
	return contents, storedKey, stored
}

// aliasKey is where the store keeps the key of the entry another key stands for: a second key, or the first key of a
// package the second key restored, so the next run with the same inputs hits on its first.
func aliasKey(key cache.Key) cache.Key {
	return sha256.Sum256(append([]byte("alias "), key[:]...))
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
		if _, hit := session.hits[each]; session.given[each] && !hit && session.earlier[each] == nil {
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
			delete(session.secondKeyHits, hit)
			delete(session.fromBinary, hit)
			dropped = true
		}
	}
	return dropped
}

// restored reports whether the run restores a package rather than analyzing it.
func (session *CacheSession) restored(restoring *packages.Package) bool {
	_, hit := session.hits[restoring]
	return hit
}

// restoredInWave reports whether a wave restores a package rather than analyzing it, for checker.Options.Restored: a
// hit, or a package an earlier wave analyzed.
func (session *CacheSession) restoredInWave(restoring *packages.Package) bool {
	return session.restored(restoring) || session.earlier[restoring] != nil
}

// restore gives an action on a restored package its package's own facts from the entry, for
// checker.Options.Restore. Each object fact names its object by objectpath, looked up in the package's types, which
// come from its export data. A fact whose object the export data does not hold is skipped and counted: export data
// is exactly what an importer can see of the package, so no importer can name that object, and no importer's
// finding can read its fact.
func (session *CacheSession) restore(action *checker.Action) error {
	restoring, hit := session.hits[action.Package]
	if !hit {
		restoring = session.earlier[action.Package]
	}
	if restoring == nil {
		return fmt.Errorf("restoring %s on %s: the run holds no entry of it", action.Analyzer.Name, action.Package.ID)
	}
	restored, held := restoring.analyses[action.Analyzer]
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
	// A question of a fact computed on demand reaches a restored package only once an action on it has restored it,
	// so the resolver's store knows the package by its types by then, the run beside the load's included.
	session.know(action.Package)
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

// reportOf is a report analyzer's result on a restored package, from its entry, and false for a package that missed or
// a run without the cache.
func (session *CacheSession) reportOf(analyzer *analysis.Analyzer, analyzedPackage *packages.Package) (any, bool) {
	if session == nil {
		return nil, false
	}
	restored, hit := session.hits[analyzedPackage]
	if !hit {
		return nil, false
	}
	return restored.analyses[analyzer].report, true
}

// filesOf is the stored walk of a restored package Analyze was given, or of one the first wave analyzed, whose
// syntax the second let go of as of any package it restores, and false for one that missed or a run without the
// cache.
func (session *CacheSession) filesOf(analyzedPackage *packages.Package) ([]storedFile, bool) {
	if session == nil {
		return nil, false
	}
	restored, hit := session.hits[analyzedPackage]
	if !hit {
		restored = session.earlier[analyzedPackage]
	}
	if restored == nil {
		return nil, false
	}
	return restored.files, true
}

// save writes the entry of every miss no wave has saved yet, from the last wave's graph, with every answer of its
// package (see demandOf), writes again each entry the first wave saved whose answers grew since, and counts every
// package the load reached as a hit, from the store or the binary, or a miss. A session without a store writes nothing
// and counts only what it restored and reloaded.
func (session *CacheSession) save(
	graph *checker.Graph,
	withheld map[*packages.Package]load.PackageWithheld,
	files map[*packages.Package][]storedFile,
) {
	session.counts.FactsSkipped = int(session.factsSkipped.Load())
	session.counts.FromBinary = len(session.fromBinary)
	session.answering.lock.Lock()
	session.counts.Reloads = len(session.answering.reloaded)
	session.counts.ReloadRounds = session.answering.rounds
	session.counts.ReloadRoundsBy = maps.Clone(session.answering.roundsBy)
	session.answering.lock.Unlock()
	if session.caching.Store == nil {
		return
	}
	session.rewriteEarlier()
	var missed []*packages.Package
	for keyed := range session.keys {
		if !session.restored(keyed) {
			missed = append(missed, keyed)
		}
	}
	session.saveMisses([]*checker.Graph{graph}, missed, withheld, files)
	// A package restored from the host's run is neither a hit of the build's store nor a miss: saveMisses wrote its
	// entry through to the build's store once its restore settled, and WrittenThrough counts it there.
	hostRestored := 0
	if session.host != nil {
		hostRestored = len(session.host.restoring)
	}
	session.counts.Hit = len(session.hits) - len(session.fromBinary) - hostRestored
	session.counts.Missed = len(session.keys) - len(session.hits)
	session.counts.SecondKeyHit = len(session.secondKeyHits)
}

// saveMisses writes the entry of each of missed that no wave tried to save, has no load error, and on which every
// action of graphs finished without one, from those actions, its types and what the load withheld of it, and returns
// each one's encoded entry. A written entry whose package names no file outside it in a finding gets an alias under
// its second key, once every output below it is known. The entries are encoded and written as many at once as there
// are cores: writing a package's export data is most of a cold run's save, and each package's is its own.
func (session *CacheSession) saveMisses(
	graphs []*checker.Graph,
	missed []*packages.Package,
	withheld map[*packages.Package]load.PackageWithheld,
	files map[*packages.Package][]storedFile,
) map[*packages.Package]encodedEntry {
	actionsOf := map[*packages.Package][]*checker.Action{}
	for _, graph := range graphs {
		if graph == nil {
			continue
		}
		for action := range graph.All() {
			actionsOf[action.Package] = append(actionsOf[action.Package], action)
		}
	}
	knownFiles := session.allFiles()
	// A package above one restored from the host's run whose restore is not settled, or was declined, read what the
	// restore gave, which may not stand: it is saved by a later wave, or by the build's next load, never now.
	unsettled := session.unsettledBelow()
	var saving []*packages.Package
	for _, keyed := range missed {
		if session.attempted[keyed] || unsettled[keyed] {
			continue
		}
		session.attempted[keyed] = true
		// A package whose analysis was handed a placeholder ran on a guess (fact_resolver.Unsure), and is analyzed
		// again by the round that loads what the placeholder stood for.
		unsure := !session.readAfterKeep("saveMisses, asking whether "+keyed.ID+" ran on a guess") &&
			session.resolver != nil && keyed.Types != nil && session.resolver.Unsure(keyed.Types)
		if len(keyed.Errors) == 0 && !keyed.IllTyped && finishedClean(actionsOf[keyed]) && !unsure {
			saving = append(saving, keyed)
		}
	}
	encoded := map[*packages.Package]encodedEntry{}
	var written []*packages.Package
	var countsLock sync.Mutex
	session.fanOut(len(saving), func(index int) {
		saved := saving[index]
		demanded := session.withBodiless(saved, session.demandOf(saved, session.previous[saved]))
		entry, encodeError := encodeEntry(session.known, session.ruleOf, saved, actionsOf[saved],
			withheld[saved], demanded, files[saved], knownFiles, true)
		var putError error
		// The export data goes first, so an entry is never in the store before what it needs to be read.
		if encodeError == nil && entry.exportData != nil {
			putError = session.caching.Store.Put(exportDataKey(session.keys[saved]), entry.exportData)
		}
		if encodeError == nil && putError == nil {
			putError = session.caching.Store.Put(session.keys[saved], entry.entry)
		}
		countsLock.Lock()
		defer countsLock.Unlock()
		if encodeError == nil {
			encoded[saved] = entry
			if entry.hasOutput {
				session.outputs[saved] = entry.output
			}
			session.collect(saved, entry)
		}
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
			session.counts.FactsDropped += entry.dropped
			session.counts.Written++
			session.written[saved] = true
			written = append(written, saved)
		}
	})
	// The packages restored from the host's run that have settled are written beside the misses, by the same rule of
	// what is settled below them, and given an alias under their second key with them.
	throughPositionDependent := session.writeThrough(actionsOf, unsettled, knownFiles)
	clear(session.closures)
	aliases := map[cache.Key]cache.Key{}
	for _, saved := range written {
		if encoded[saved].positionDependent {
			continue
		}
		if second, known := session.secondKey(saved); known {
			aliases[second] = session.keys[saved]
		}
	}
	for through, dependent := range throughPositionDependent {
		if dependent {
			continue
		}
		if second, known := session.secondKey(through); known {
			aliases[second] = session.keys[through]
		}
	}
	session.alias(aliases)
	return encoded
}

// alias keeps under each key's alias the key of the entry it stands for, as many at once as there are cores: on
// cohere's own root a cold run writes about a thousand, which one at a time took 0.2s, while hashing every second
// key took 5ms. A failed write is a problem, never a finding: the next run misses where it would have hit.
func (session *CacheSession) alias(aliases map[cache.Key]cache.Key) {
	keys := slices.Collect(maps.Keys(aliases))
	session.fanOut(len(keys), func(index int) {
		target := aliases[keys[index]]
		if putError := session.caching.Store.Put(aliasKey(keys[index]), target[:]); putError != nil {
			session.problem(putError.Error())
		}
	})
}

// fanOut runs work for every index below count, as many at once as there are cores: on the run's pool when the session
// has one (Caching.Pool), under its slots at the priority below every load's and check's work, so the session's own
// work beside another build's never runs more at once than the run's cores; otherwise on a goroutine per core of its
// own. work must keep what it makes in its own index's place.
func (session *CacheSession) fanOut(count int, work func(index int)) {
	if session.caching.Pool != nil {
		session.caching.Pool.ForEach(count, 0, work)
		return
	}
	bounded.ForEach(count, false, work)
}

// allFiles is every file of every package the load reached, made on first use.
func (session *CacheSession) allFiles() *knownFiles {
	if session.knownFiles != nil {
		return session.knownFiles
	}
	session.knownFiles = newKnownFiles()
	for keyed := range session.keys {
		session.knownFiles.addPackage(keyed)
	}
	return session.knownFiles
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
