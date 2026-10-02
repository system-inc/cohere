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

func (r *FindingsReuse) lookup(path string, contentHash [sha256.Size]byte, rules []string) (LintCacheEntry, bool) {
	entry, hit := r.previous.Lookup(path, contentHash, r.key, rules)
	if hit {
		r.replayed.Add(1)
		r.keep(entry)
	}
	return entry, hit
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
func replayEntry(entry LintCacheEntry, sourceFile *ast.SourceFile, diagnostics *[]rule.Diagnostic,
	reporting map[string]int, offered map[string]int, listening map[string]int) {
	for _, name := range entry.Rules {
		offered[name]++
	}
	for _, name := range entry.Listening {
		listening[name]++
	}
	for _, finding := range entry.Findings {
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
func recordableEntry(sourceFile *ast.SourceFile, contentHash [sha256.Size]byte, cacheable []string,
	fileDiagnostics []rule.Diagnostic, fileListening map[string]int, visited int, silenced suppressionTally) (LintCacheEntry, bool) {
	if silenced.applied != 0 || silenced.unusedDirectives != 0 {
		return LintCacheEntry{}, false
	}
	isCacheable := make(map[string]bool, len(cacheable))
	for _, name := range cacheable {
		isCacheable[name] = true
	}
	entry := LintCacheEntry{
		Path:         sourceFile.FileName(),
		ContentHash:  contentHash,
		Rules:        cacheable,
		VisitedNodes: visited,
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
