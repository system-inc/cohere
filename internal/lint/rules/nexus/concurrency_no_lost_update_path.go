package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// concurrencyNoLostUpdatePath is a variable, or a static member path rooted at a variable or `this`.
//
// With store set it is instead one entry of a store reached through a read call and a write call (see
// storeAccessOf): root, isThis and names then hold the receiver of a method store (`this.cache` for
// `this.cache.get(key)`) and are empty for a store of free functions, and store names the entry.
type concurrencyNoLostUpdatePath struct {
	root   *ast.Symbol
	isThis bool
	names  []string
	store  string
}

func (path concurrencyNoLostUpdatePath) key() string {
	prefix := "this"
	if !path.isThis {
		prefix = "symbol"
	}
	return prefix + "\x00" + strings.Join(path.names, "\x00") + "\x00\x00" + path.store
}

// sameRoot reports whether two paths start at the same variable, or both at `this`.
func (path concurrencyNoLostUpdatePath) sameRoot(other concurrencyNoLostUpdatePath) bool {
	if path.isThis || other.isThis {
		return path.isThis && other.isThis
	}
	return path.root == other.root
}

// sameStoreEntry reports whether two store targets name the same entry of the same store.
func (path concurrencyNoLostUpdatePath) sameStoreEntry(other concurrencyNoLostUpdatePath) bool {
	if path.store == "" || path.store != other.store || !path.sameRoot(other) || len(path.names) != len(other.names) {
		return false
	}
	for index, name := range path.names {
		if other.names[index] != name {
			return false
		}
	}
	return true
}
