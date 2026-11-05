package program

import (
	"crypto/sha256"
	"fmt"
)

// lintCacheWire is the findings section as encoded. Rule lists are stored once and referenced by index:
// on ahra 3,605 entries share 4 distinct rule lists and 24 listening lists, and writing each out in full
// once made the file 53 MB.
type lintCacheWire struct {
	Version  int
	Key      [sha256.Size]byte
	KeyParts RunCacheKeyParts
	Lists    [][]string
	Entries  []lintCacheWireEntry

	DesignSystem *DesignSystemKey
}

// cache resolves the interned lists. An index past its table is an error rather than an empty list: an
// index trusted past its table would hand an entry another file's rule list.
//
// A findings section of another version decodes to nil rather than failing the table. The runs beside it
// are proven by their own keys, and a findings cache is only ever a miss when it is absent.
func (wire *lintCacheWire) cache() (*LintCache, error) {
	if wire.Version != lintCacheVersion {
		return nil, nil
	}
	cache := &LintCache{
		Version:      wire.Version,
		Key:          wire.Key,
		KeyParts:     wire.KeyParts,
		Entries:      make([]LintCacheEntry, 0, len(wire.Entries)),
		DesignSystem: wire.DesignSystem,
	}
	list := func(index int) ([]string, error) {
		if index < 0 || index >= len(wire.Lists) {
			return nil, fmt.Errorf("list %d of %d", index, len(wire.Lists))
		}
		return wire.Lists[index], nil
	}
	for position, stored := range wire.Entries {
		entry := LintCacheEntry{
			Path:            stored.Path,
			ContentHash:     stored.ContentHash,
			TypeFingerprint: stored.TypeFingerprint,
			VisitedNodes:    stored.VisitedNodes,
			Findings:        stored.Findings,

			ShapeFingerprint: stored.ShapeFingerprint,

			DesignFingerprint: stored.DesignFingerprint,

			DerivedFingerprint: stored.DerivedFingerprint,

			Notes: ruleNotes(stored.Notes),

			Directives: stored.Directives,
			Withheld:   stored.Withheld,

			Adamic: stored.Adamic,
		}
		var listError error
		if entry.Rules, listError = list(stored.Rules); listError != nil {
			return nil, fmt.Errorf("entry %d rules: %w", position, listError)
		}
		if entry.TypedRules, listError = list(stored.TypedRules); listError != nil {
			return nil, fmt.Errorf("entry %d typed rules: %w", position, listError)
		}
		if entry.Listening, listError = list(stored.Listening); listError != nil {
			return nil, fmt.Errorf("entry %d listening: %w", position, listError)
		}
		if entry.ShapedRules, listError = list(stored.ShapedRules); listError != nil {
			return nil, fmt.Errorf("entry %d shaped rules: %w", position, listError)
		}
		if entry.DesignRules, listError = list(stored.DesignRules); listError != nil {
			return nil, fmt.Errorf("entry %d design-system rules: %w", position, listError)
		}
		if entry.DerivedRules, listError = list(stored.DerivedRules); listError != nil {
			return nil, fmt.Errorf("entry %d derived rules: %w", position, listError)
		}
		cache.Entries = append(cache.Entries, entry)
	}
	return cache, nil
}
