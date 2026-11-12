package check

import (
	"fmt"

	"golang.org/x/tools/go/packages"
)

// A package's dependence set (#zwj7g88), the first piece of the walk (#154dr43): every package whose types, facts or
// answers the verdict on its reads could consult, computed from the host's side alone. An extra build visits a
// package only when its set meets the packages that changed there, so the set must hold every package a verdict
// reads, while one it holds that no read needed only costs a visit. Each kind of read reaches what the verdict asks of
// it, and the reach of a rendering is what that rendering visited: the closure digest's graph keeps, on each node, the
// type names its lines name and the types of the fields they render by their properties (typeNode), so the set and the
// digest follow the same nodes and cannot drift.

// DependenceSet is one package's dependence set (Dependence): the whole import closure, or the packages by path, such
// that if each renders in a build as it does on the host, declares the same facts and gives the same answers, the
// verdict's every read of the package holds there. A package renders as on the host when every object and type name
// of it renders alike, the lines the closure digest and the use digests are made of.
type DependenceSet struct {
	// WholeClosure is set for a package whose reads the set cannot follow one by one, whose set is its whole import
	// closure; Paths is then empty.
	WholeClosure bool

	// Paths are the packages of the set by path: each the verdict finds in the build by its path, a read object's
	// owner (find) or a Scope lookup's package (judged.closure[path]), so a path the build does not list fails the
	// verdict, and meets what changed.
	Paths map[string]bool

	// UnfoundPaths are the paths of the Closure lookups that found nothing. A Closure lookup never finds its package by
	// path: it asks the import closure (ClosureInterface, buildAnswer), so its path is read only where the closure
	// holds a package of it, and the lookup is one of three cases. Found: its answer's closure, which holds the path's
	// package, and the membership, which holds the package in the closure; no path of its own. Not found, with a
	// package of the path in the closure: that package lacked the name, or declared no interface of it, and a build
	// whose package of the path changed its files may declare it, with the same imports and name, which the membership
	// does not see, so the path is read, as the package's own. Not found, with no package of the path in the closure:
	// an absent package is no read, and it can enter the closure only through an import that changed, which the
	// membership sees. So an unfound path meets what changed when its package is in the package's import closure and
	// is affected, and never for being unlisted. The closure the walk asks is the build's, which holds the packages the
	// host's did whenever the membership is unchanged, and when it changed the membership visits the package anyway.
	UnfoundPaths map[string]bool

	// OwnFactPaths are the packages whose deprecations a Deprecated lookup read, a fact of the package itself or of an
	// object of it. A deprecation is computed from its package's own files alone: the deprecated facts analyzer
	// (honnef.co/go/tools v0.8.1, analysis/facts/deprecated/deprecated.go:31-139) requires no analyzer, imports no
	// fact, and exports a package fact from its files' doc comments (lines 64-76) and an object fact from the doc
	// comment of each declaration, field and interface method of its files, by its own type information (lines 78-139).
	// So a package whose files are the host's declares the host's deprecations, visited or not, and an own fact path
	// meets what changed only when its package's own files did, or the build does not list it. It would break if that
	// analyzer came to require one that learns facts, or to import a fact of another package
	// (TestADeprecationIsItsOwnPackagesAlone pins the first). Finding the subject by its objectpath stays the
	// counterpart's read, in Paths.
	OwnFactPaths map[string]bool

	// ClosureMembership is set for a package whose reads read which packages its import closure holds, and their
	// names: a Closure lookup, which finds the package at its path in the closure, a PackageName lookup, which asks
	// whether a package of the closure has a name, and every Scope or Closure lookup's import roots (importRoots),
	// which the build must hold. A package whose own inputs are the host's compiles the host's files, which the
	// portable digest covers, so it has the host's imports and the host's name: the closure's membership and its
	// packages' names change only through a package that changed in the build, and only through one whose imports or
	// name changed with it. So the set meets what changed when the import closure holds such a package, read against
	// what changed itself, never against what the walk visits: a visited package has the host's own inputs.
	ClosureMembership bool
}

// Dependence is restoring's dependence set for TypesHold and FactsHold, from the reads the host's run made of it, with
// build the host's memo, which every build shares: memoized there by the package's reads, so each package's set is
// computed once, when it is first asked, which is once the host's run is over and adds no read to it. Reads the
// verdict judges read by read reach, by kind:
//
//   - a use of the whole, and every read of a package with assembly files (Restoring.Assembly), judged by the memo's
//     whole twin: every package the closure digest's walk visits (closure);
//   - a member: its owner and the types its own line renders by their properties, read whole (member);
//   - a type name named, or its underlying type: its owner and every alias of its chain (typeName);
//   - a method set: that, and every package of a type embedded at any depth (methodSet);
//   - a selection: the receiver's owner, every package of a type embedded in the receiver at any depth, and the
//     selected member's own line (selection);
//   - a Scope lookup: its path, the answer's closure, and the closure's membership; a Closure lookup: the membership,
//     and the found answer's closure, or its path as an unfound one (UnfoundPaths); a PackageName lookup: the
//     membership (ClosureMembership); a Deprecated lookup: its path's own facts (OwnFactPaths), and the subject's
//     counterpart;
//   - a fact read: the fact's owner.
//
// Every object whose counterpart the verdict finds by objectpath reaches its owner and the packages its path's
// interfaces embed (counterpart). Persisted reads, reads never recorded or marked never, reads judged by closure alone
// (build_reads.Reads.ClosureOnly), and an Interface or Method lookup, which reads every type name of the closure with
// a method of its name, or a lookup of a kind the set does not know, are the whole import closure. The answers an
// entry read are answersDependence's.
func (build *ReadsBuild) Dependence(restoring Restoring) DependenceSet {
	if restoring.persisted != nil || restoring.Reads == nil {
		return DependenceSet{WholeClosure: true}
	}
	if known, isKnown := build.dependences.Load(restoring.Reads); isKnown {
		return knownDependence(restoring, known)
	}
	// Two verdicts may compute one package's set at once; each computes it alike, and every caller gets the one stored.
	stored, _ := build.dependences.LoadOrStore(restoring.Reads, build.dependenceOf(restoring))
	return knownDependence(restoring, stored)
}

// knownDependence is known, the memo's entry for restoring's reads, as a DependenceSet.
func knownDependence(restoring Restoring, known any) DependenceSet {
	set, isSet := known.(DependenceSet)
	if !isSet {
		// Invariant: Dependence stores only a DependenceSet.
		panic(fmt.Sprintf("check: the dependence set of %s is a %T", restoring.Path, known))
	}
	return set
}

// dependenceOf is restoring's dependence set, computed (Dependence).
func (build *ReadsBuild) dependenceOf(restoring Restoring) DependenceSet {
	if restorable, _ := restoring.Reads.Restorable(); !restorable || restoring.Reads.ClosureOnly() != "" {
		return DependenceSet{WholeClosure: true}
	}
	judged := build
	if restoring.Assembly {
		judged = build.wholeTwin()
	}
	reach := &dependenceReach{memo: judged, sizes: judged.graph.sizesForWalk(), paths: map[string]bool{},
		unfound: map[string]bool{}, ownFacts: map[string]bool{}}
	// The set does not depend on the order the reads are taken in, so they are taken unsorted.
	uses, selections, facts, lookups := restoring.Reads.Unordered()
	for _, use := range uses {
		if restoring.Assembly {
			reach.closure(use.Object)
			continue
		}
		reach.use(use)
	}
	for _, selection := range selections {
		if restoring.Assembly {
			reach.closure(selection.Receiver)
			reach.closure(selection.Selected)
			continue
		}
		reach.selection(selection)
	}
	for _, lookup := range lookups {
		if !reach.lookup(lookup) {
			return DependenceSet{WholeClosure: true}
		}
	}
	for _, fact := range facts {
		reach.add(fact.Package)
		if fact.Object != nil {
			reach.counterpart(fact.Object)
		}
	}
	return DependenceSet{Paths: reach.gathered(), UnfoundPaths: reach.unfound, OwnFactPaths: reach.ownFacts,
		ClosureMembership: reach.membership}
}

// answersDependence is what the answers' check (CacheSession.readsHold) reads of other packages for restored, an entry
// restored for a package: each answer read's owner, found by its ID among byIdentifier as readsHold finds it, by path.
// An entry whose reads could not all be named, or one naming an owner byIdentifier does not hold, never holds, so its
// set is the whole import closure.
func answersDependence(restored *restoredPackage, byIdentifier map[string]*packages.Package) DependenceSet {
	if restored.readsIncomplete {
		return DependenceSet{WholeClosure: true}
	}
	paths := map[string]bool{}
	for _, read := range restored.reads {
		owner := byIdentifier[read.Owner]
		if owner == nil {
			return DependenceSet{WholeClosure: true}
		}
		paths[owner.PkgPath] = true
	}
	return DependenceSet{Paths: paths}
}
