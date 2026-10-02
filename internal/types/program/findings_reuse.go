package program

import (
	"crypto/sha256"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// FindingsReuse is one run's use of the findings cache: what the last run recorded, and what this
// one records for the next.
//
// The walk consults it per file. A file whose bytes, key and applied cacheable rules all match an
// entry is walked with only its uncacheable rules, and the cacheable rules' findings and coverage
// are taken from the entry. Every other file is walked in full and, if eligible, recorded.
//
// Coverage is replayed along with findings, by ruling: coverage recorded over inputs proven
// unchanged is still true of the tree, the argument the run cache settled. A replay is never
// silent about itself either: Replayed counts the files served from cache, and the lint line says
// how many, so a walked verdict and a remembered one can always be told apart.
type FindingsReuse struct {
	key      [sha256.Size]byte
	previous *LintCache

	mutex    sync.Mutex
	next     map[string]LintCacheEntry
	replayed atomic.Int64
}

// NewFindingsReuse prepares a run's reuse. A previous cache under a different key is dropped at
// once rather than consulted per file: nothing in it can be valid.
//
// The previous cache's lookup index is built here, before any worker runs. Lookup would otherwise
// build it on first use, and sixteen workers missing at once would race to build it.
func NewFindingsReuse(key [sha256.Size]byte, previous *LintCache) *FindingsReuse {
	if previous != nil && previous.Key != key {
		previous = nil
	}
	if previous != nil {
		previous.ensureIndex()
	}
	return &FindingsReuse{key: key, previous: previous, next: map[string]LintCacheEntry{}}
}

// Replayed is how many files this run served from cache.
func (r *FindingsReuse) Replayed() int {
	if r == nil {
		return 0
	}
	return int(r.replayed.Load())
}

// Recorded is what this run leaves for the next: every file it served or walked eligibly, under
// this run's key. Files this run did not reach are dropped, so the cache never outgrows the tree.
func (r *FindingsReuse) Recorded() *LintCache {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	cache := &LintCache{Version: lintCacheVersion, Key: r.key, Entries: make([]LintCacheEntry, 0, len(r.next))}
	for _, entry := range r.next {
		cache.Entries = append(cache.Entries, entry)
	}
	sort.Slice(cache.Entries, func(first, second int) bool { return cache.Entries[first].Path < cache.Entries[second].Path })
	return cache
}

// ShapeClasses splits CacheClasses' type-aware rules by what they can see of imported files: those that
// declare rule.TypeReachShapes, keyed on the shape fingerprint, and the rest, keyed on the type
// fingerprint.
func ShapeClasses(typeAware []rule.Rule) (shaped []rule.Rule, typed []rule.Rule) {
	for _, subject := range typeAware {
		if subject.TypeReach == rule.TypeReachShapes {
			shaped = append(shaped, subject)
		} else {
			typed = append(typed, subject)
		}
	}
	return shaped, typed
}

// cacheKeys is what a file's cached findings are looked up and recorded under: its bytes, the rules of
// each class that apply to it, and the two fingerprints the type-aware classes are keyed on.
type cacheKeys struct {
	contentHash      [sha256.Size]byte
	pure             []string
	typed            []string
	typeFingerprint  [sha256.Size]byte
	shaped           []string
	shapeFingerprint [sha256.Size]byte
}

// lookup reports whether a file's pure rules can be replayed, and whether each type-aware class can be
// too. Both need the first, and each also needs its own rule list and its own fingerprint unchanged: an
// importer of an edited file has unchanged bytes and a changed type fingerprint, so its pure rules replay
// and its content-keyed rules run again, while its shape-keyed rules replay unless the edit changed a shape.
func (r *FindingsReuse) lookup(path string, keys cacheKeys) (entry LintCacheEntry, pureHit bool, typedHit bool, shapedHit bool) {
	entry, pureHit = r.previous.Lookup(path, keys.contentHash, r.key, keys.pure)
	if !pureHit {
		return LintCacheEntry{}, false, false, false
	}
	r.replayed.Add(1)
	r.keep(entry)
	typedHit = equalStrings(entry.TypedRules, keys.typed) && entry.TypeFingerprint == keys.typeFingerprint
	shapedHit = equalStrings(entry.ShapedRules, keys.shaped) && entry.ShapeFingerprint == keys.shapeFingerprint
	return entry, true, typedHit, shapedHit
}

func (r *FindingsReuse) keep(entry LintCacheEntry) {
	r.mutex.Lock()
	r.next[entry.Path] = entry
	r.mutex.Unlock()
}

// ruleNames lists rules by name, in order.
func ruleNames(rules []rule.Rule) []string {
	names := make([]string, len(rules))
	for index, subject := range rules {
		names[index] = subject.Name
	}
	return names
}

// replayEntry turns a cached entry back into the walk's diagnostics and coverage for one file.
//
// typedToo and shapedToo say whether each type-aware class is replayed as well. A class that is not runs
// again on this file and counts its own coverage, so replaying its coverage here would count it twice.
func replayEntry(entry LintCacheEntry, typedToo bool, shapedToo bool, sourceFile *ast.SourceFile, diagnostics *[]rule.Diagnostic,
	reporting map[string]int, offered map[string]int, listening map[string]int) {
	replays := make(map[string]bool, len(entry.Rules)+len(entry.TypedRules)+len(entry.ShapedRules))
	for _, name := range entry.Rules {
		replays[name] = true
		offered[name]++
	}
	if typedToo {
		for _, name := range entry.TypedRules {
			replays[name] = true
			offered[name]++
		}
	}
	if shapedToo {
		for _, name := range entry.ShapedRules {
			replays[name] = true
			offered[name]++
		}
	}
	for _, name := range entry.Listening {
		if replays[name] {
			listening[name]++
		}
	}
	for _, finding := range entry.Findings {
		if !replays[finding.RuleName] {
			continue
		}
		*diagnostics = append(*diagnostics, rule.Diagnostic{
			RuleName:   finding.RuleName,
			Range:      core.NewTextRange(int(finding.Start), int(finding.End)),
			Message:    rule.Message{Id: finding.MessageId, Description: finding.MessageDescription},
			SourceFile: sourceFile,
		})
		reporting[finding.RuleName]++
	}
}

// recordableEntry builds the entry a fully walked file would leave, or reports that it must not leave
// one.
//
// A file is not recorded when any of its directives did anything or went unused, since a directive's
// accounting spans every rule in the file, cached and walked alike, and when any cacheable rule's
// finding carries a fix or a suggestion, since a cached finding stores neither and the fix phase needs
// the edit itself.
func recordableEntry(sourceFile *ast.SourceFile, keys cacheKeys, fileDiagnostics []rule.Diagnostic,
	fileListening map[string]int, visited int, silenced suppressionTally) (LintCacheEntry, bool) {
	if silenced.applied != 0 || silenced.unusedDirectives != 0 {
		return LintCacheEntry{}, false
	}
	cacheable := append(append(append([]string{}, keys.pure...), keys.typed...), keys.shaped...)
	isCacheable := make(map[string]bool, len(cacheable))
	for _, name := range cacheable {
		isCacheable[name] = true
	}
	entry := LintCacheEntry{
		Path:             sourceFile.FileName(),
		ContentHash:      keys.contentHash,
		Rules:            keys.pure,
		TypedRules:       keys.typed,
		TypeFingerprint:  keys.typeFingerprint,
		ShapedRules:      keys.shaped,
		ShapeFingerprint: keys.shapeFingerprint,
		VisitedNodes:     visited,
	}
	for _, name := range cacheable {
		if fileListening[name] > 0 {
			entry.Listening = append(entry.Listening, name)
		}
	}
	for _, diagnostic := range fileDiagnostics {
		if !isCacheable[diagnostic.RuleName] {
			continue
		}
		if len(diagnostic.Fixes) > 0 || len(diagnostic.Suggestions) > 0 {
			return LintCacheEntry{}, false
		}
		entry.Findings = append(entry.Findings, LintCacheFinding{
			RuleName:           diagnostic.RuleName,
			Start:              int32(diagnostic.Range.Pos()),
			End:                int32(diagnostic.Range.End()),
			MessageId:          diagnostic.Message.Id,
			MessageDescription: diagnostic.Message.Description,
		})
	}
	return entry, true
}

// refreshTyped is an entry whose pure part was replayed and one or both of whose type-aware classes just
// ran again: the old findings and listening of every class that replayed, and the fresh ones of every class
// that ran, under this run's fingerprints. Refused, like any recording, when a fresh finding carries a fix
// or a suggestion.
func refreshTyped(old LintCacheEntry, keys cacheKeys, typedRan bool, shapedRan bool,
	fileDiagnostics []rule.Diagnostic, fileListening map[string]int) (LintCacheEntry, bool) {
	ran := map[string]bool{}
	if typedRan {
		for _, name := range keys.typed {
			ran[name] = true
		}
	}
	if shapedRan {
		for _, name := range keys.shaped {
			ran[name] = true
		}
	}
	kept := make(map[string]bool, len(old.Rules)+len(old.TypedRules)+len(old.ShapedRules))
	for _, name := range old.Rules {
		kept[name] = true
	}
	if !typedRan {
		for _, name := range old.TypedRules {
			kept[name] = true
		}
	}
	if !shapedRan {
		for _, name := range old.ShapedRules {
			kept[name] = true
		}
	}
	refreshed := LintCacheEntry{
		Path:             old.Path,
		ContentHash:      old.ContentHash,
		Rules:            old.Rules,
		TypedRules:       keys.typed,
		TypeFingerprint:  keys.typeFingerprint,
		ShapedRules:      keys.shaped,
		ShapeFingerprint: keys.shapeFingerprint,
		VisitedNodes:     old.VisitedNodes,
	}
	for _, name := range old.Listening {
		if kept[name] {
			refreshed.Listening = append(refreshed.Listening, name)
		}
	}
	for _, names := range [][]string{keys.typed, keys.shaped} {
		for _, name := range names {
			if ran[name] && fileListening[name] > 0 {
				refreshed.Listening = append(refreshed.Listening, name)
			}
		}
	}
	for _, finding := range old.Findings {
		if kept[finding.RuleName] {
			refreshed.Findings = append(refreshed.Findings, finding)
		}
	}
	for _, diagnostic := range fileDiagnostics {
		if !ran[diagnostic.RuleName] {
			continue
		}
		if len(diagnostic.Fixes) > 0 || len(diagnostic.Suggestions) > 0 {
			return LintCacheEntry{}, false
		}
		refreshed.Findings = append(refreshed.Findings, LintCacheFinding{
			RuleName:           diagnostic.RuleName,
			Start:              int32(diagnostic.Range.Pos()),
			End:                int32(diagnostic.Range.End()),
			MessageId:          diagnostic.Message.Id,
			MessageDescription: diagnostic.Message.Description,
		})
	}
	return refreshed, true
}
