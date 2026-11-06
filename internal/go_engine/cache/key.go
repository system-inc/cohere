// Package cache is the Go engine's per-package findings cache, its keys and its store: each package as loaded is
// keyed on everything its analysis reads, its dependencies' keys among them, so an edit re-analyzes only the
// packages it reaches. What an entry holds, and when one is read or written, is the checker's; this package
// only says which inputs a package's analysis is, and keeps the bytes it is given under that.
package cache

import "encoding/hex"

// Key is one package's cache key: a sha256 over every input of its analysis.
type Key [32]byte

// String is the key in lowercase hex, as the store names its entry.
func (key Key) String() string {
	return hex.EncodeToString(key[:])
}
