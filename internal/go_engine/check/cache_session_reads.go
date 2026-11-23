package check

import (
	"bytes"
	"cmp"
	"slices"

	"github.com/system-inc/go-types/types"
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
// or its standard-library key in a run that keyed no other, sorted, and whether one could not be named, an object with
// no objectpath or an owner this run did not key.
func (session *CacheSession) readsOf(saved *packages.Package, held entryDemand) ([]storedRead, bool) {
	if session.readAfterKeep("readsOf, the reads of " + saved.ID) {
		return held.reads, held.readsIncomplete
	}
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
		owner, key, keyed := session.ownerKeyOf(read.Owner)
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

// ownerKeyOf is the package whose types are ownerTypes, an answer's owner, and the key readsOf names it by: its first
// key in this run, or the key a run without a store keyed it by for its builds, or its standard-library key. A run
// without a store keys its packages only once it keeps them for its builds, after its saves, but it keys the standard
// library before any: a standard-library package reads only the standard library's, so its entry's reads are always
// named, and an extra build restoring it declines none of them as unnamed. It reports false for an owner it cannot
// name.
func (session *CacheSession) ownerKeyOf(ownerTypes *types.Package) (*packages.Package, cache.Key, bool) {
	owner := session.packageOf(ownerTypes)
	if owner == nil {
		owner = session.standardLibraryOwnerOf(ownerTypes)
	}
	if owner == nil {
		return nil, cache.Key{}, false
	}
	if key, keyed := session.keys[owner]; keyed {
		return owner, key, true
	}
	if key, keyed := session.buildKeys[owner]; keyed {
		return owner, key, true
	}
	key, keyed := session.standardLibraryKeys[owner]
	return owner, key, keyed
}

// standardLibraryOwnerOf is the standard-library package whose types are owner, among those the session keyed, or nil.
// A run beside the load tells the session of a package only as an action restores it, since no package's types are
// final as the run starts (check.Run), so a standard-library package the run computed is unknown to packageOf when a
// package above it is saved; its types are final by then, since the read was of them, so it is found by its import
// path, then by its types. A path may name two packages the standard library's keys hold: a run over the standard
// library itself loads a package beside its in-package test variant, both with the plain package's path, and the
// keyer judges both its own (cache's isStandard reads files, module and imports, never the ID). Each has its own key,
// so the package taken is the one whose types are owner's, never another under the same path.
func (session *CacheSession) standardLibraryOwnerOf(owner *types.Package) *packages.Package {
	for _, candidate := range session.standardLibraryByPath[owner.Path()] {
		if candidate.Types == owner {
			return candidate
		}
	}
	return nil
}

// standardLibraryPackagesByPath are the packages of keys by import path, each path's in ID order, for
// standardLibraryOwnerOf.
func standardLibraryPackagesByPath(keys map[*packages.Package]cache.Key) map[string][]*packages.Package {
	byPath := make(map[string][]*packages.Package, len(keys))
	for keyed := range keys {
		byPath[keyed.PkgPath] = append(byPath[keyed.PkgPath], keyed)
	}
	for _, sharing := range byPath {
		slices.SortFunc(sharing, func(left *packages.Package, right *packages.Package) int {
			return cmp.Compare(left.ID, right.ID)
		})
	}
	return byPath
}

// holdAliasHits trusts every hit whose entry is its own: one its store, the project's or the system cache's, holds
// under the package's own first key, which holds every key below it. A hit an alias led to is trusted once its reads
// hold (see readsHold), checked below it first, each package after everything it imports; one whose reads do not hold
// is a miss, which the second key may restore once the first wave has run.
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
		if key, keyed := session.keys[current]; keyed && session.stored[current] == key {
			session.trusted[current] = true
			return
		}
		if holds, _ := session.readsHold(current); holds {
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

// unnamedAnswers is why an entry whose reads could not all be named never holds: an answer it read was of an object
// with no objectpath, or of an owner its run did not key, so no build can check it. The record counts it apart from an
// answer that differs, since a build could close the gap where it could never close a platform's difference.
const unnamedAnswers = "unnamed: an answer it read of another package could not be named, so the build cannot check it"

// readsHold reports whether every read of the entry restored for current holds in this run: its owner is a package
// the load reached, and either the owner's first key is the one it had when the read was made, or the owner's answer
// now is the one read (see currentAnswer). An entry whose reads could not all be named never holds. When it does not
// hold it says why, as a reason the record counts by its first word: unnamedAnswers for reads not all named, else A
// and the first read that does not hold, its kind and its object as the read names it, its owner's ID and objectpath.
func (session *CacheSession) readsHold(current *packages.Package) (bool, string) {
	restored := session.hits[current]
	if restored == nil {
		return false, "A no entry of it is restored to read answers from"
	}
	if restored.readsIncomplete {
		return false, unnamedAnswers
	}
	for _, read := range restored.reads {
		owner := session.byIdentifier[read.Owner]
		if owner == nil {
			return false, "A the build has no package " + read.Owner + ", whose answer " + read.Kind + " of " +
				readObjectName(read) + " it read"
		}
		if key, keyed := session.keys[owner]; keyed && bytes.Equal(key[:], read.OwnerKey) {
			continue
		}
		answer, answered := session.currentAnswer(owner, read)
		if !answered {
			return false, "A the build has no answer " + read.Kind + " of " + readObjectName(read) + " to ask"
		}
		if !bytes.Equal(answer, read.Value) {
			return false, "A answer " + read.Kind + " of " + readObjectName(read) + " differs in the build"
		}
	}
	return true, ""
}

// readObjectName is the object read answers of, as a reason names it: its owner's ID and its objectpath there, the
// two that identify a read beside its kind (readsOf).
func readObjectName(read storedRead) string {
	return read.Owner + "." + string(read.Path)
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
	if session.readAfterKeep("currentAnswer, an answer of "+owner.ID) || session.resolver == nil || owner.Types == nil {
		return nil, false
	}
	return session.resolver.Probe(read.Kind, owner.Types, read.Path)
}

// holdsBySecondKey reports whether the entry decideBySecondKey just restored for current by its second key holds,
// its reads checked as readsHold checks them, and trusts it when it does.
func (session *CacheSession) holdsBySecondKey(current *packages.Package) bool {
	if holds, _ := session.readsHold(current); !holds {
		return false
	}
	session.trusted[current] = true
	return true
}
