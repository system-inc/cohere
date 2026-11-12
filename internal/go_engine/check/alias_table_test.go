package check

import (
	"crypto/sha256"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// aliasKeyNamed is a key made from name, for a table's aliases and targets.
func aliasKeyNamed(name string) cache.Key {
	return sha256.Sum256([]byte(name))
}

// A build's alias table keeps every alias a run adds, read back exact by the next run (#m7w1ncy): aliases written in
// two rounds, each by a table of its own as two runs have, are both found by a third, each with its own target, and a
// round that names an alias again moves it to its new target. The newest aliases up to the bound are kept and the
// oldest dropped. A table cut short reads as none, every alias in it a miss, and another build's context keeps a
// table of its own.
func TestABuildsAliasTableKeepsEveryAliasExactAndTheNewestWithinItsBound(test *testing.T) {
	test.Parallel()

	store := cache.Open(test.TempDir())
	context := cache.Context{FormatVersion: "test", EngineIdentity: "engine", Build: []string{"GOOS=darwin"},
		RuleSet: []string{"rule"}}
	first, second, third := aliasKeyNamed("first"), aliasKeyNamed("second"), aliasKeyNamed("third")
	if addError := newAliasTable(store, context).add(map[cache.Key]cache.Key{
		first: aliasKeyNamed("first's entry"), second: aliasKeyNamed("second's entry"),
	}, aliasTableFloor); addError != nil {
		test.Fatal(addError)
	}
	if addError := newAliasTable(store, context).add(map[cache.Key]cache.Key{
		third: aliasKeyNamed("third's entry"), second: aliasKeyNamed("second's new entry"),
	}, aliasTableFloor); addError != nil {
		test.Fatal(addError)
	}
	read := newAliasTable(store, context)
	for alias, want := range map[cache.Key]cache.Key{
		first: aliasKeyNamed("first's entry"), second: aliasKeyNamed("second's new entry"),
		third: aliasKeyNamed("third's entry"),
	} {
		if target, held := read.target(alias); !held || target != want {
			test.Errorf("alias %s reads %s held %t, want %s", alias, target, held, want)
		}
	}

	if addError := newAliasTable(store, context).add(map[cache.Key]cache.Key{
		aliasKeyNamed("fourth"): aliasKeyNamed("fourth's entry"),
	}, 3); addError != nil {
		test.Fatal(addError)
	}
	bounded := newAliasTable(store, context)
	if _, held := bounded.target(first); held {
		test.Errorf("want the oldest alias dropped at a bound of 3")
	}
	for _, kept := range []cache.Key{aliasKeyNamed("fourth"), third, second} {
		if _, held := bounded.target(kept); !held {
			test.Errorf("want alias %s kept within the bound", kept)
		}
	}

	other := context
	other.Build = []string{"GOOS=linux"}
	if _, held := newAliasTable(store, other).target(third); held {
		test.Errorf("another build's context read this build's table")
	}

	cut := newAliasTable(store, context)
	if putError := store.Put(cut.key, make([]byte, aliasPairSize-1)); putError != nil {
		test.Fatal(putError)
	}
	if _, held := cut.target(third); held {
		test.Errorf("a table cut short read an alias")
	}
}
