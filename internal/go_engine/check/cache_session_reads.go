package check

import (
	"bytes"
	"cmp"
	"slices"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// The read sets (#w7zmvat wave 3b). A package's findings and answers depend on its files, on what it imports, and on
// the answers of the fact kinds computed on demand that its analysis took from other packages. Its first key covers
// all three, since it holds every key below it. Its second key covers the first two, its own files and what each
// import's output digest says, but no output digest holds those answers, since the kinds learn no facts. So an entry
// restored by anything but the package's own first key, by its second key or by an alias either one wrote, holds only
// while every answer it read still holds for its owner as the run finds it. Each entry keeps those reads, and the run
// checks them: in Restore for an alias of the first key, against the entries the run restores below it, and between
// the waves for the second key, against those and against the packages the first wave analyzed, which the resolver
// computes in now. A read that cannot be checked then is a miss, never assumed to hold.

// readsOf is the reads the entry of saved holds: those held before, the entry's before a reload or the first wave's,
// with every read the run's analysis of saved made, each naming its owner by ID and the owner's first key in this run,
// sorted, and whether one could not be named, an object with no objectpath or an owner this run did not key.
func (session *CacheSession) readsOf(saved *packages.Package, held entryDemand) ([]storedRead, bool) {
	type readIdentity struct {
		kind  string
		owner string
		path  string
	}
	merged := map[readIdentity]storedRead{}
	for _, read := range held.reads {
		merged[readIdentity{read.Kind, read.Owner, string(read.Path)}] = read
	}
	incomplete := held.readsIncomplete
	reads, complete := session.resolver.Reads(saved.Types)
	incomplete = incomplete || !complete
	for _, read := range reads {
		owner := session.packageOf(read.Owner)
		key, keyed := cache.Key{}, false
		if owner != nil {
			key, keyed = session.keys[owner]
			if !keyed {
				key, keyed = session.buildKeys[owner]
			}
		}
		if !keyed {
			incomplete = true
			continue
		}
		merged[readIdentity{read.Kind, owner.ID, string(read.Path)}] = storedRead{
			Kind:     read.Kind,
			Owner:    owner.ID,
			OwnerKey: key[:],
			Path:     read.Path,
			Value:    read.Value,
		}
	}
	sorted := make([]storedRead, 0, len(merged))
	for _, read := range merged {
		sorted = append(sorted, read)
	}
	slices.SortFunc(sorted, func(left storedRead, right storedRead) int {
		return cmp.Or(cmp.Compare(left.Owner, right.Owner), cmp.Compare(left.Kind, right.Kind),
			cmp.Compare(left.Path, right.Path))
	})
	return sorted, incomplete
}

// holdAliasHits trusts every hit whose entry is its own: one from the binary's table, keyed by everything it read,
// and one the store holds under the package's own first key, which holds every key below it. A hit an alias led to is
// trusted once its reads hold (see readsHold), checked below it first, each package after everything it imports; one
// whose reads do not hold is a miss, which the second key may restore once the first wave has run.
func (session *CacheSession) holdAliasHits(reachable []*packages.Package) {
	session.trusted = map[*packages.Package]bool{}
	session.byIdentifier = make(map[string]*packages.Package, len(reachable))
	for _, each := range reachable {
		session.byIdentifier[each.ID] = each
	}
	visited := map[*packages.Package]bool{}
	var hold func(current *packages.Package)
	hold = func(current *packages.Package) {
		if visited[current] {
			return
		}
		visited[current] = true
		for _, imported := range current.Imports {
			hold(imported)
		}
		if _, hit := session.hits[current]; !hit {
			return
		}
		if key, keyed := session.keys[current]; session.fromBinary[current] || keyed && session.stored[current] == key {
			session.trusted[current] = true
			return
		}
		if session.readsHold(current) {
			session.trusted[current] = true
			return
		}
		delete(session.hits, current)
		delete(session.stored, current)
	}
	for _, each := range reachable {
		hold(each)
	}
}

// readsHold reports whether every read of the entry restored for current holds in this run: its owner is a package
// the load reached, and either the owner's first key is the one it had when the read was made, or the owner's answer
// now is the one read (see currentAnswer). An entry whose reads could not all be named never holds.
func (session *CacheSession) readsHold(current *packages.Package) bool {
	restored := session.hits[current]
	if restored == nil || restored.readsIncomplete {
		return false
	}
	for _, read := range restored.reads {
		owner := session.byIdentifier[read.Owner]
		if owner == nil {
			return false
		}
		if key, keyed := session.keys[owner]; keyed && bytes.Equal(key[:], read.OwnerKey) {
			continue
		}
		answer, answered := session.currentAnswer(owner, read)
		if !answered || !bytes.Equal(answer, read.Value) {
			return false
		}
	}
	return true
}

// currentAnswer is the answer of read's kind at read's path in owner as the run finds it, and whether the run has it
// now: from owner's entry, when the run trusts it, its kind's answer of no fact when the entry was computed whole in
// the kind and holds none; or computed by the resolver, for a package the run's first wave analyzed (see
// fact_resolver.Probe). A package the run has not analyzed yet, or restored from an entry it does not trust, has none.
func (session *CacheSession) currentAnswer(owner *packages.Package, read storedRead) ([]byte, bool) {
	if restored := session.hits[owner]; restored != nil && session.trusted[owner] {
		for _, kind := range restored.answers {
			if kind.Kind != read.Kind {
				continue
			}
			index, found := slices.BinarySearchFunc(kind.Answers, read.Path,
				func(answer pathAnswer, path objectpath.Path) int { return cmp.Compare(answer.Path, path) })
			if found {
				return kind.Answers[index].Value, true
			}
			if kind.Complete {
				return fact_resolver.ZeroAnswer(read.Kind)
			}
		}
		return nil, false
	}
	if session.resolver == nil || owner.Types == nil {
		return nil, false
	}
	return session.resolver.Probe(read.Kind, owner.Types, read.Path)
}

// holdsBySecondKey reports whether the entry decideBySecondKey just restored for current by its second key holds,
// its reads checked as readsHold checks them, and trusts it when it does.
func (session *CacheSession) holdsBySecondKey(current *packages.Package) bool {
	if !session.readsHold(current) {
		return false
	}
	session.trusted[current] = true
	return true
}
