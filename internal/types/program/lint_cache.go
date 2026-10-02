package program

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// The lint findings cache: what each rule reported about one file, replayed when neither the
// file nor the rule set has changed.
//
// This is the cheapest of the three caches to make correct, and the reason is worth stating
// because it does not hold for the other two. A pure rule's findings are a function of exactly
// one file's bytes. Same bytes, same rule, same findings, with nothing else in the world able
// to change the answer. So the key is a content hash and there is no invalidation graph.
//
// The word doing the work there is "pure". A rule that reads other files is not, and caching
// it under one file's hash serves a stale result forever when the OTHER file changes. Two live
// rules are cross-file today, and a third case is latent: the upstream adapter hands the whole
// program to every rule it wraps and cannot inspect what it wrapped, so it can never truthfully
// declare purity for a body it does not own.
//
// That is what CacheableRules exists to decide, and why it defaults to excluding rather than
// including. The asymmetry is the same one the codebase already chose for the type checker:
// under-declaring serves a stale finding silently and forever, over-declaring costs a cache
// miss. One of those is a correctness failure and the other is a performance one.
//
// It is wired as the run cache's second layer: when a bare run's inputs are not all unchanged, files
// whose bytes and configuration are unchanged are walked with only their uncacheable rules, and the
// cacheable rules' findings and coverage are replayed. See FindingsReuse.
//
// It was measured and left unwired once, and the reversal is worth keeping because the measurement
// method is the same and only the tree changed. On 2026-08-25, with 212 rules, walking with only the
// uncacheable ones saved about 9%: the walk visits every node whatever the rule set, so with light
// rules there was little left to skip. On 2026-10-02, with 464 rules, the same experiment in process
// on ahra's real config:
//
//	walk, all 464 rules                 about 1.8-2.6s
//	walk, the 172 uncacheable           about 1.2-1.4s
//	walk, the 44 ReadsProgram only      about 0.4s
//
// The node count is identical in all three, so the walk still cannot be skipped; what grew is the work
// rules do at each node. 164 of the 172 exclusions are type-aware only, which is what re-running
// type-aware rules on a changed file's importers would reach.
//
// Two kinds of file are never served from cache. A file with any suppression directive, because a
// directive's accounting spans every rule in the file, cached and walked alike. A file with any
// finding that carries a fix or a suggestion, because a cached finding stores neither and the fix
// phase needs the edit itself.
type LintCache struct {
	// Version is the format. A different version is a miss, never a best-effort read.
	Version int

	// Key covers everything a cacheable rule's answer depends on beyond the file's own bytes: the
	// binary, the lint config, the tsconfig chain, the project root and the rule set in order. Any
	// change to one invalidates every entry at once, which is what makes it a miss rather than a
	// stale pass. It was called RuleSetHash when the rule set was all it covered.
	Key [sha256.Size]byte

	// Entries is one record per cached file.
	Entries []LintCacheEntry

	// index maps path to position in Entries, built lazily on the first Lookup. Not serialized:
	// it is derived from Entries and rebuilding it costs less than storing it.
	index map[string]int
}

// LintCacheEntry is one file's findings, keyed by what the file contained.
type LintCacheEntry struct {
	// Path identifies the file. Kept for debugging and for eviction; the hash is what decides
	// a hit.
	Path string

	// ContentHash is the hash of the file's bytes when these findings were produced.
	ContentHash [sha256.Size]byte

	// Rules is the cacheable rules the configuration applied to this file, in order. A hit requires
	// the same list now, so an override that changes which rules reach this file is a miss even under
	// an unchanged key.
	Rules []string

	// Listening is the subset of Rules that registered a listener on this file. A replay counts them
	// as listening, which is what keeps the coverage line identical to a walked run's.
	Listening []string

	// VisitedNodes is how many nodes the full walk of this file visited. The walk counts nodes only
	// when some rule listens, so a cached file whose remaining rules listen to nothing would otherwise
	// contribute zero and change the coverage line.
	VisitedNodes int

	// Findings is what the cacheable rules reported. Empty is a real answer: it means the rules ran
	// and found nothing, which is exactly the case worth caching since most files are clean.
	Findings []LintCacheFinding
}

// LintCacheFinding is one finding, flattened.
//
// rule.Diagnostic carries a *ast.SourceFile belonging to a program that no longer exists by
// the time this is read back, so the file is implied by the entry rather than stored per
// finding.
//
// MessageDescription holds the rendered text. An earlier version of this comment said text was
// "derived from the id when a finding is rendered" and therefore safe to omit. That was untrue,
// and the field was absent because of it. Nothing in this tree derives text from an id: there is
// no id-to-text table, and `printRuleDiagnostic` prints `Message.Description` directly. Measured
// 2026-08-25 over `internal/rules`: 340 message ids, of which 11 build their description with
// `fmt.Sprintf` from runtime values, so for those one id maps to many strings and no table could
// exist. A cached finding replaying only the id would print an empty or wrong sentence beside a
// correct file, range and rule — the right metadata carrying the wrong message, which is the one
// failure here that looks exactly like success.
//
// The text is stored rather than the 11 being refused because refusing them buys bytes with a
// standing per-rule obligation, and this format already depends on `ReadsProgram` carrying one.
// The size scales the right way: the artifact grows with a dirty tree, which is the run where
// this cache saves least, because changed files are recomputed anyway.
//
// Message ids are not unique on their own, which is a second reason the id could never have been
// the key. `unexpected` carries five distinct descriptions across six core rules, colliding across
// rules and never within one, so the honest identity of a message is the pair (RuleName,
// MessageId). Lookups key on the pair even though the text is now stored, so nobody later assumes
// ids are unique.
//
// Fixes and suggestions are counted rather than stored, and that omission IS sound: a fix is a
// replacement over a byte range, and replaying one computed against different bytes would corrupt
// the file it claims to repair. A cached finding is a report, never an edit.
type LintCacheFinding struct {
	RuleName string `json:"rule"`
	Start    int32  `json:"start"`
	End      int32  `json:"end"`

	// MessageId identifies the message within its rule. Not unique across rules; see above.
	MessageId string `json:"messageId"`

	// MessageDescription is the rendered sentence, stored because nothing can rebuild it.
	MessageDescription string `json:"messageDescription"`

	FixCount        int32 `json:"fixCount,omitempty"`
	SuggestionCount int32 `json:"suggestionCount,omitempty"`
}

// MessageKey is the honest identity of a message: the pair, never the id alone.
func (f LintCacheFinding) MessageKey() (string, string) {
	return f.RuleName, f.MessageId
}

// HashContent is the key a cached entry is stored under.
func HashContent(content string) [sha256.Size]byte {
	return sha256.Sum256([]byte(content))
}

// HashRuleSet covers the names of every rule that ran, in order.
//
// Order is included deliberately, but not for the reason first written here. That reason was:
// "the order they ran in is the order the findings come back in". It is false. Findings come
// back in file-then-position order, which the walk imposes after its workers finish
// (sortDiagnostics in walk.go); rule order never decided it.
//
// The conclusion survives the correction on different grounds. A reordered rule set can change
// which findings EXIST rather than merely their sequence, because fixes applied by an earlier
// rule change the text a later rule reads, and suppression directives are consumed in the order
// rules run. So a cache keyed without rule order could replay a set the current configuration
// would not produce, which is what this hash prevents.
//
// For anyone wiring this cache: LintCacheEntry.Findings is an ordered slice, and replayed findings
// must reach the caller through the same sortDiagnostics the walk uses, so a warm run and a cold
// run return one sequence and cache state cannot decide it.
func HashRuleSet(ruleNames []string) [sha256.Size]byte {
	hash := sha256.New()
	for _, name := range ruleNames {
		hash.Write([]byte(name))
		hash.Write([]byte{0})
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

// lintCacheVersion is bumped whenever the format's meaning changes.
//
// 4: rule lists are stored once and referenced by index, and hashes are hex. Version 3 wrote each
// entry's applied and listening rules out in full; on ahra 3,605 entries shared 4 distinct rule lists
// and 24 listening lists, and the file was 53 MB and 113ms to decode, a tenth of what the cache saves.
// Versions 1 and 2 were a binary layout of offsets into an interned blob, replaced because adding
// fields to it was where its "an encoder forgot a field" bug had shipped four times.
const lintCacheVersion = 4

// lintCacheWire is the format on disk. It is kept apart from LintCache so the walk's view of an entry
// stays plain slices, and so the round-trip test, which compares LintCache's fields, proves this
// encoding carries every one of them.
type lintCacheWire struct {
	Version int                  `json:"version"`
	Key     string               `json:"key"`
	Lists   [][]string           `json:"lists"`
	Entries []lintCacheWireEntry `json:"entries"`
}

type lintCacheWireEntry struct {
	Path         string             `json:"path"`
	ContentHash  string             `json:"contentHash"`
	Rules        int                `json:"rules"`
	Listening    int                `json:"listening"`
	VisitedNodes int                `json:"visitedNodes"`
	Findings     []LintCacheFinding `json:"findings,omitempty"`
}

// Encode writes the cache.
func (c *LintCache) Encode() []byte {
	wire := lintCacheWire{Version: lintCacheVersion, Key: hex.EncodeToString(c.Key[:]), Lists: [][]string{}}
	positions := map[string]int{}
	intern := func(list []string) int {
		joined := strings.Join(list, "\x00")
		if position, seen := positions[joined]; seen {
			return position
		}
		positions[joined] = len(wire.Lists)
		wire.Lists = append(wire.Lists, append([]string{}, list...))
		return len(wire.Lists) - 1
	}
	for _, entry := range c.Entries {
		wire.Entries = append(wire.Entries, lintCacheWireEntry{
			Path:         entry.Path,
			ContentHash:  hex.EncodeToString(entry.ContentHash[:]),
			Rules:        intern(entry.Rules),
			Listening:    intern(entry.Listening),
			VisitedNodes: entry.VisitedNodes,
			Findings:     entry.Findings,
		})
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		// Every field is a plain value, so this cannot fail; if it ever does, an empty artifact
		// decodes as unreadable, which is a miss.
		return nil
	}
	return encoded
}

// ErrLintCacheUnreadable means the artifact is not one this build can use. Never a reason to
// fail a run: the answer to an unreadable cache is to run cold.
var ErrLintCacheUnreadable = errors.New("lint cache is not readable by this build")

// DecodeLintCache reads an artifact written by Encode. Anything else, including a valid artifact of
// another version, a hash of the wrong length or a list index out of range, is ErrLintCacheUnreadable:
// an index trusted past its table would hand an entry another file's rule list.
func DecodeLintCache(buffer []byte) (*LintCache, error) {
	var wire lintCacheWire
	if err := json.Unmarshal(buffer, &wire); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLintCacheUnreadable, err)
	}
	if wire.Version != lintCacheVersion {
		return nil, fmt.Errorf("%w: format version %d, this build reads %d", ErrLintCacheUnreadable, wire.Version, lintCacheVersion)
	}
	cache := &LintCache{Version: wire.Version, Entries: make([]LintCacheEntry, 0, len(wire.Entries))}
	if err := decodeHash(wire.Key, &cache.Key); err != nil {
		return nil, fmt.Errorf("%w: key: %v", ErrLintCacheUnreadable, err)
	}
	list := func(index int) ([]string, error) {
		if index < 0 || index >= len(wire.Lists) {
			return nil, fmt.Errorf("list %d of %d", index, len(wire.Lists))
		}
		return wire.Lists[index], nil
	}
	for position, stored := range wire.Entries {
		entry := LintCacheEntry{Path: stored.Path, VisitedNodes: stored.VisitedNodes, Findings: stored.Findings}
		if err := decodeHash(stored.ContentHash, &entry.ContentHash); err != nil {
			return nil, fmt.Errorf("%w: entry %d: %v", ErrLintCacheUnreadable, position, err)
		}
		var err error
		if entry.Rules, err = list(stored.Rules); err != nil {
			return nil, fmt.Errorf("%w: entry %d rules: %v", ErrLintCacheUnreadable, position, err)
		}
		if entry.Listening, err = list(stored.Listening); err != nil {
			return nil, fmt.Errorf("%w: entry %d listening: %v", ErrLintCacheUnreadable, position, err)
		}
		cache.Entries = append(cache.Entries, entry)
	}
	return cache, nil
}

func decodeHash(text string, into *[sha256.Size]byte) error {
	decoded, err := hex.DecodeString(text)
	if err != nil {
		return err
	}
	if len(decoded) != sha256.Size {
		return fmt.Errorf("%d bytes, want %d", len(decoded), sha256.Size)
	}
	copy(into[:], decoded)
	return nil
}

// Lookup returns a file's cached entry, and whether the cache had a usable answer.
//
// The second return distinguishes "cached, no findings" from "not cached", and that distinction is
// the whole correctness of this cache: a clean file and an unknown file both produce no findings, and
// treating them the same is how a cache silently reports a clean tree.
//
// A hit needs the key, the path, the bytes and the applied rule list all to match. The index is built
// on first use, so a caller that loads a cache and discards it on a key mismatch never pays for it.
func (c *LintCache) Lookup(path string, contentHash [sha256.Size]byte, key [sha256.Size]byte, rules []string) (LintCacheEntry, bool) {
	if c == nil || c.Key != key {
		return LintCacheEntry{}, false
	}
	c.ensureIndex()
	position, found := c.index[path]
	if !found {
		return LintCacheEntry{}, false
	}
	entry := c.Entries[position]
	// The path matching is not enough. A file whose contents changed has a stale entry under the same
	// path, and returning it is precisely the silent failure this cache must not have.
	if entry.ContentHash != contentHash || !equalStrings(entry.Rules, rules) {
		return LintCacheEntry{}, false
	}
	return entry, true
}

// ensureIndex builds the path index if it is missing. A caller about to share the cache across
// goroutines calls it first, since building it lazily inside concurrent lookups would race.
func (c *LintCache) ensureIndex() {
	if c.index != nil {
		return
	}
	c.index = make(map[string]int, len(c.Entries))
	for position := range c.Entries {
		c.index[c.Entries[position].Path] = position
	}
}

func equalStrings(first []string, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

// Store records one file's findings, replacing any earlier entry for the same path.
//
// Replace rather than append, because a path appearing twice makes Lookup's answer depend on which
// copy the index happened to keep. The index is invalidated rather than patched: Store is called
// once per file during a walk and Lookup runs afterward, so rebuilding once on the next lookup
// costs less than maintaining the map through every insert.
//
// Empty findings are stored deliberately. A clean file is the most valuable thing this cache holds,
// because most files are clean, and an entry saying "these bytes produce nothing" is a real answer.
// Skipping it would make a clean file indistinguishable from an unknown one, which is the exact
// ambiguity Lookup's second return exists to destroy.
func (c *LintCache) Store(entry LintCacheEntry) {
	if c == nil {
		return
	}
	if c.index != nil {
		if position, found := c.index[entry.Path]; found {
			c.Entries[position] = entry
			return
		}
	}
	for position := range c.Entries {
		if c.Entries[position].Path == entry.Path {
			c.Entries[position] = entry
			c.index = nil
			return
		}
	}
	c.Entries = append(c.Entries, entry)
	c.index = nil
}

// WriteLintCache persists the cache, creating the directory if it is missing.
//
// Written to a temporary file in the same directory and renamed into place, because several cohere
// runs can share a tree and a reader must never see a half-written artifact. Rename is atomic
// within a filesystem; writing directly to the destination is not, and a truncated cache is the
// shape that fails to decode, turning every file into a miss for no reason.
//
// A failure here is returned rather than swallowed. The next run being cold is a cost someone
// should be told about, since the symptom otherwise is a saving that quietly never appears.
func WriteLintCache(path string, cache *LintCache) error {
	if cache == nil {
		return fmt.Errorf("writing the lint cache: no cache to write")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", directory, err)
	}

	temporary, err := os.CreateTemp(directory, ".lintcache-*")
	if err != nil {
		return fmt.Errorf("creating a temporary file in %s: %w", directory, err)
	}
	temporaryName := temporary.Name()
	// Best-effort cleanup on every failure path below, so a failed write does not leave the
	// directory filling with partial artifacts across runs.
	defer func() {
		if temporaryName != "" {
			os.Remove(temporaryName)
		}
	}()

	if _, err := temporary.Write(cache.Encode()); err != nil {
		temporary.Close()
		return fmt.Errorf("writing %s: %w", temporaryName, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", temporaryName, err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("renaming %s into place at %s: %w", temporaryName, path, err)
	}
	temporaryName = ""
	return nil
}

// ReadLintCache loads a cache from disk.
//
// A missing file and an unreadable one are both reported as a miss with no error, because neither
// is a failure: the first run has no cache, and an artifact this build cannot parse is exactly what
// the version byte exists to produce. Both mean the same thing to the caller, which is to run cold.
//
// The error is returned anyway, for a caller that wants to say why. Nothing may treat it as fatal.
func ReadLintCache(path string) (*LintCache, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeLintCache(contents)
}

// CacheableRules splits a rule set into the rules whose findings may be cached per file and the
// rules that must run on every file regardless.
//
// The property being tested is purity with respect to one file's bytes. This cache keys on a
// content hash, so a rule that reads anything else can have its answer changed by an edit the key
// cannot see, and replaying it then serves a stale finding forever: zero findings on a file that
// now has one, indistinguishable from a clean tree.
//
// Two declarations disqualify a rule.
//
// ReadsProgram is the direct one. A rule touching ctx.Program reaches every source file in the run,
// so another file changing invalidates its answer while this file's hash is unmoved. Eleven rules
// declare it today, and a structural test in internal/dispatch fails any rule that reads the
// program without saying so, which is what keeps this list honest as rules are added.
//
// NeedsTypeChecker is the less obvious one, and it is included deliberately. A type-aware rule's
// answer depends on the types its file imports, so editing a dependency changes what the rule
// should report while the importing file's bytes stay identical. That is the same staleness as
// ReadsProgram arriving by a different route, and a per-file content hash cannot see either.
//
// The split is asymmetric on purpose, the same way the two declarations themselves are: including a
// rule wrongly serves stale findings silently and forever, excluding one wrongly costs a cache
// miss. Those are not comparable, so anything not provably pure is excluded. A future rule with a
// new way of reading outside its file is excluded by default only if its property is declared, so
// the declarations are the load-bearing part and this function is only the consequence.
func CacheableRules(rules []rule.Rule) (cacheable []rule.Rule, uncacheable []rule.Rule) {
	for _, subject := range rules {
		if subject.ReadsProgram || subject.NeedsTypeChecker {
			uncacheable = append(uncacheable, subject)
			continue
		}
		cacheable = append(cacheable, subject)
	}
	return cacheable, uncacheable
}
