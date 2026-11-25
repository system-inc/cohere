package rule

// FileCache memoizes per-file derived work across the rules that share it.
//
// Keyed by string rather than holding named fields, each entry of whatever type its derivation computes.
// This package must not know what a comment scan is, or a scope table, or whatever the next expensive
// shared derivation turns out to be. The rule packages own those; this owns only the fact that they are
// worth computing once.
//
// Not safe for concurrent use, and it does not need to be: one cache is created per file and every
// rule for that file runs on the one goroutine that owns it. Sharing a cache across files or
// workers would be a bug, which is why nothing here has a mutex to make that look safe.
type FileCache struct {
	entries map[string]cacheEntry

	// fills counts how many times each derivation was computed, keyed the same way the entries are.
	// Once per key is the cache working.
	fills map[string]int

	// aroundFill, when set, runs each fill, so the caller can say whose work the fill is.
	//
	// This exists because per-rule timing lies about shared work. Whichever rule asks for a
	// derivation first pays for it, and since files are walked in parallel the identity of that
	// rule varies per file. Measured on three comment rules sharing one scan: 171ms, 132ms, and
	// 1.0ms for identical work, where the 1.0ms rule was simply the one that asked last. Under
	// --timing the walk sets this to bill the fill to the derivation rather than to the rule that
	// asked, which is the only reading that stays true as the order changes.
	aroundFill func(key string, fill func())
}

// Fills reports how many times each derivation was computed in this file, by key.
//
// Empty for a cache that was never filled, which is the common case for a file no comment rule
// looked at.
func (cache *FileCache) Fills() map[string]int {
	if cache == nil {
		return nil
	}
	return cache.fills
}

// SetAroundFill makes every later fill run inside around. Nil restores plain fills.
func (cache *FileCache) SetAroundFill(around func(key string, fill func())) {
	if cache == nil {
		return
	}
	cache.aroundFill = around
}
