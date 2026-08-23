package program

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
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
// The measured prize: lint is roughly 323ms of a 1.57s run on the ahra tree, 95 rules over
// 3,407 files producing 129 findings across 73 files. Most files produce nothing, and an empty
// result is a real cached answer rather than an absence.
type LintCache struct {
	// RuleSetHash covers which rules ran and their order. A rule added, removed, or renamed
	// changes what the tree should report, and every stored entry becomes wrong at once. This
	// is what makes that a cache miss rather than a silent stale pass.
	RuleSetHash [sha256.Size]byte

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

	// Findings is what the pure rules reported. Empty is a real answer: it means the rules ran
	// and found nothing, which is exactly the case worth caching since most files are clean.
	Findings []LintCacheFinding
}

// LintCacheFinding is one finding, flattened.
//
// rule.Diagnostic carries a *ast.SourceFile belonging to a program that no longer exists by
// the time this is read back, so the file is implied by the entry rather than stored per
// finding.
//
// Message text is deliberately absent. It is derived from the id when a finding is rendered,
// so storing it would duplicate a value that can drift from its source; a field that must not
// drift is better missing than stale. Fixes and suggestions are counted rather than stored for
// a harder reason: a fix is a replacement over a byte range, and replaying one computed
// against different bytes would corrupt the file it claims to repair. A cached finding is a
// report, never an edit.
type LintCacheFinding struct {
	RuleName        string
	Start           int32
	End             int32
	MessageId       string
	FixCount        int32
	SuggestionCount int32
}

// HashContent is the key a cached entry is stored under.
func HashContent(content string) [sha256.Size]byte {
	return sha256.Sum256([]byte(content))
}

// HashRuleSet covers the names of every rule that ran, in order.
//
// Order is included deliberately. Two rules can report on the same range, and the order they
// ran in is the order the findings come back in; a cache that ignored order could replay a
// finding set the current rule configuration would not produce.
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

// lintCacheMagic identifies the format and its version.
//
// Same reasoning as the resolution cache: this is read back as offsets into a blob, so a file
// from an older layout parses into strings from the wrong places rather than failing. The
// version byte is what makes that loud.
var lintCacheMagic = [8]byte{'v', 'f', 'y', 'l', 'i', 'n', 't', 1}

const (
	lintEntryHeaderSize = sha256.Size + 12 // content hash, path offset+length, finding count
	lintFindingSize     = 32               // rule offset+length, start, end, message id offset+length, fix and suggestion counts
)

// Encode writes the cache as a flat binary artifact.
func (c *LintCache) Encode() []byte {
	var blob []byte
	table := make(map[string]int32)
	intern := func(value string) (int32, int32) {
		if value == "" {
			return -1, 0
		}
		if offset, seen := table[value]; seen {
			return offset, int32(len(value))
		}
		offset := int32(len(blob))
		blob = append(blob, value...)
		table[value] = offset
		return offset, int32(len(value))
	}

	// Two passes: intern everything to build the blob, then lay out fixed-width records whose
	// offsets point into it.
	type entryLayout struct {
		contentHash            [sha256.Size]byte
		pathOffset, pathLength int32
		findings               []LintCacheFinding
		findingOffsets         [][4]int32 // rule offset, rule length, message id offset, message id length
	}
	layouts := make([]entryLayout, 0, len(c.Entries))
	totalFindings := 0
	for _, entry := range c.Entries {
		pathOffset, pathLength := intern(entry.Path)
		layout := entryLayout{
			contentHash: entry.ContentHash,
			pathOffset:  pathOffset,
			pathLength:  pathLength,
			findings:    entry.Findings,
		}
		for _, finding := range entry.Findings {
			ruleOffset, ruleLength := intern(finding.RuleName)
			messageOffset, messageLength := intern(finding.MessageId)
			layout.findingOffsets = append(layout.findingOffsets,
				[4]int32{ruleOffset, ruleLength, messageOffset, messageLength})
		}
		totalFindings += len(entry.Findings)
		layouts = append(layouts, layout)
	}

	headerSize := 8 + sha256.Size + 8
	body := len(layouts)*lintEntryHeaderSize + totalFindings*lintFindingSize
	buffer := make([]byte, headerSize+body+len(blob))

	copy(buffer[0:], lintCacheMagic[:])
	copy(buffer[8:], c.RuleSetHash[:])
	cursor := 8 + sha256.Size
	binary.LittleEndian.PutUint32(buffer[cursor:], uint32(len(layouts)))
	binary.LittleEndian.PutUint32(buffer[cursor+4:], uint32(len(blob)))
	cursor += 8

	for _, layout := range layouts {
		copy(buffer[cursor:], layout.contentHash[:])
		binary.LittleEndian.PutUint32(buffer[cursor+sha256.Size:], uint32(layout.pathOffset))
		binary.LittleEndian.PutUint32(buffer[cursor+sha256.Size+4:], uint32(layout.pathLength))
		binary.LittleEndian.PutUint32(buffer[cursor+sha256.Size+8:], uint32(len(layout.findings)))
		cursor += lintEntryHeaderSize

		for index, finding := range layout.findings {
			offsets := layout.findingOffsets[index]
			binary.LittleEndian.PutUint32(buffer[cursor+0:], uint32(offsets[0]))
			binary.LittleEndian.PutUint32(buffer[cursor+4:], uint32(offsets[1]))
			binary.LittleEndian.PutUint32(buffer[cursor+8:], uint32(finding.Start))
			binary.LittleEndian.PutUint32(buffer[cursor+12:], uint32(finding.End))
			binary.LittleEndian.PutUint32(buffer[cursor+16:], uint32(offsets[2]))
			binary.LittleEndian.PutUint32(buffer[cursor+20:], uint32(offsets[3]))
			binary.LittleEndian.PutUint32(buffer[cursor+24:], uint32(finding.FixCount))
			binary.LittleEndian.PutUint32(buffer[cursor+28:], uint32(finding.SuggestionCount))
			cursor += lintFindingSize
		}
	}

	copy(buffer[cursor:], blob)
	return buffer
}

// ErrLintCacheUnreadable means the artifact is not one this build can use. Never a reason to
// fail a run, only a reason to lint from source and write a fresh cache.
var ErrLintCacheUnreadable = errors.New("lint cache is not readable by this build")

// DecodeLintCache reads an artifact written by Encode.
//
// Every offset and length is checked against the buffer before use. A truncated or foreign
// file must error rather than produce findings assembled from the wrong bytes, because a
// finding pointing at the wrong rule name and range is worse than no finding at all.
func DecodeLintCache(buffer []byte) (*LintCache, error) {
	headerSize := 8 + sha256.Size + 8
	if len(buffer) < headerSize {
		return nil, fmt.Errorf("%w: %d bytes is shorter than the header", ErrLintCacheUnreadable, len(buffer))
	}
	if string(buffer[0:8]) != string(lintCacheMagic[:]) {
		return nil, fmt.Errorf("%w: magic or version does not match", ErrLintCacheUnreadable)
	}

	cache := &LintCache{}
	copy(cache.RuleSetHash[:], buffer[8:8+sha256.Size])
	cursor := 8 + sha256.Size
	entryCount := int(binary.LittleEndian.Uint32(buffer[cursor:]))
	blobLength := int(binary.LittleEndian.Uint32(buffer[cursor+4:]))
	cursor += 8

	if entryCount < 0 || blobLength < 0 {
		return nil, fmt.Errorf("%w: header counts are negative", ErrLintCacheUnreadable)
	}

	// The blob sits at the end, and its start is only known after walking the records. Walk
	// them once to find it, bounds-checking as we go, before reading a single string.
	scan := cursor
	for index := 0; index < entryCount; index++ {
		if scan+lintEntryHeaderSize > len(buffer) {
			return nil, fmt.Errorf("%w: entry %d runs past the buffer", ErrLintCacheUnreadable, index)
		}
		findingCount := int(binary.LittleEndian.Uint32(buffer[scan+sha256.Size+8:]))
		if findingCount < 0 {
			return nil, fmt.Errorf("%w: entry %d claims %d findings", ErrLintCacheUnreadable, index, findingCount)
		}
		scan += lintEntryHeaderSize + findingCount*lintFindingSize
		if scan > len(buffer) {
			return nil, fmt.Errorf("%w: entry %d's findings run past the buffer", ErrLintCacheUnreadable, index)
		}
	}
	if scan+blobLength != len(buffer) {
		return nil, fmt.Errorf("%w: records end at %d with a %d-byte blob, which does not fill %d bytes",
			ErrLintCacheUnreadable, scan, blobLength, len(buffer))
	}
	blob := buffer[scan : scan+blobLength]

	read := func(offset, length int32) (string, error) {
		if offset < 0 {
			return "", nil
		}
		if length < 0 || int(offset)+int(length) > len(blob) {
			return "", fmt.Errorf("%w: string at %d+%d falls outside the %d-byte blob",
				ErrLintCacheUnreadable, offset, length, len(blob))
		}
		return string(blob[offset : offset+length]), nil
	}

	cache.Entries = make([]LintCacheEntry, 0, entryCount)
	for index := 0; index < entryCount; index++ {
		var entry LintCacheEntry
		copy(entry.ContentHash[:], buffer[cursor:cursor+sha256.Size])
		path, err := read(
			int32(binary.LittleEndian.Uint32(buffer[cursor+sha256.Size:])),
			int32(binary.LittleEndian.Uint32(buffer[cursor+sha256.Size+4:])))
		if err != nil {
			return nil, err
		}
		entry.Path = path
		findingCount := int(binary.LittleEndian.Uint32(buffer[cursor+sha256.Size+8:]))
		cursor += lintEntryHeaderSize

		entry.Findings = make([]LintCacheFinding, 0, findingCount)
		for findingIndex := 0; findingIndex < findingCount; findingIndex++ {
			ruleName, err := read(
				int32(binary.LittleEndian.Uint32(buffer[cursor+0:])),
				int32(binary.LittleEndian.Uint32(buffer[cursor+4:])))
			if err != nil {
				return nil, err
			}
			messageId, err := read(
				int32(binary.LittleEndian.Uint32(buffer[cursor+16:])),
				int32(binary.LittleEndian.Uint32(buffer[cursor+20:])))
			if err != nil {
				return nil, err
			}
			entry.Findings = append(entry.Findings, LintCacheFinding{
				RuleName:        ruleName,
				Start:           int32(binary.LittleEndian.Uint32(buffer[cursor+8:])),
				End:             int32(binary.LittleEndian.Uint32(buffer[cursor+12:])),
				MessageId:       messageId,
				FixCount:        int32(binary.LittleEndian.Uint32(buffer[cursor+24:])),
				SuggestionCount: int32(binary.LittleEndian.Uint32(buffer[cursor+28:])),
			})
			cursor += lintFindingSize
		}
		cache.Entries = append(cache.Entries, entry)
	}

	return cache, nil
}

// Lookup returns the cached findings for a file, and whether the cache had a usable answer.
//
// The second return distinguishes "cached, no findings" from "not cached", and that
// distinction is the whole correctness of this cache: a clean file and an unknown file both
// produce an empty slice, and treating them the same is how a cache silently reports a clean
// tree.
//
// The index is built on first use rather than at decode, so a caller that decodes a cache and
// then discards it on a rule-set mismatch never pays for it. A linear scan here would be
// 3,407 lookups against 3,407 entries on this tree, which is the kind of quadratic that hides
// until the tree doubles.
func (c *LintCache) Lookup(path string, contentHash [sha256.Size]byte, ruleSetHash [sha256.Size]byte) ([]LintCacheFinding, bool) {
	if c == nil {
		return nil, false
	}
	// A changed rule set invalidates everything at once. Checking it per lookup rather than
	// once at load keeps the caller from having to remember to.
	if c.RuleSetHash != ruleSetHash {
		return nil, false
	}
	if c.index == nil {
		c.index = make(map[string]int, len(c.Entries))
		for position := range c.Entries {
			c.index[c.Entries[position].Path] = position
		}
	}
	position, found := c.index[path]
	if !found {
		return nil, false
	}
	// The path matching is not enough. A file whose contents changed has a stale entry under
	// the same path, and returning it is precisely the silent failure this cache must not have.
	if c.Entries[position].ContentHash != contentHash {
		return nil, false
	}
	return c.Entries[position].Findings, true
}
