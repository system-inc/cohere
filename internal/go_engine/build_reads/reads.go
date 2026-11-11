// Package build_reads holds what one package's check read of other packages on the host's run, so an extra build
// (linux, windows, freebsd) can restore the package from the host's results when everything it read is the same
// there (#b2t9xmf). The load records the foreign objects a package's type check reached, the checker records each
// fact an analyzer on it asked of another package, hit or miss, the rules record each question they ask of other
// packages past the syntax (a lookup), and any of them marks a package that can never be restored, with the first
// reason. Exactness rests on recording every read, so what cannot be recorded marks the package, never passes
// silently.
//
// Recording collects pointers and renders nothing: rendering and comparing the reads are the restore's.
package build_reads

import (
	"cmp"
	"go/types"
	"maps"
	"reflect"
	"slices"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// RestoredFromCache is why a package the findings cache restored can never be restored by its reads: its types came
// from export data, so it has no type maps to walk, and its entry keeps no reads.
const RestoredFromCache = "restored from the findings cache; its reads are not stored yet (next unit)"

// Reads is one package's reads of other packages. The rules' run walks its type reads once (RecordTypes), and every
// action on the package may add fact reads and lookups from its own goroutine, so each method takes the lock.
//
// Its state is explicit, since empty reads promise something: recorded, its type reads walked, empty when it read
// nothing; never, with the first reason; or unknown, its type reads never walked, which is no promise at all. Only
// recorded reads with no never mark can restore their package (see Restorable).
type Reads struct {
	lock sync.Mutex

	// claimed is set once a walk of the package's type reads has been taken (Claim), and recorded once it has
	// published them.
	claimed  bool
	recorded bool

	// objects are the foreign objects the package's type check reached, each once.
	objects map[types.Object]bool

	// facts are the fact reads, each object or package and fact type once, the first read's outcome kept: a fact of
	// another package is inherited before any pass runs, so a second read of it reads the same.
	facts map[factKey]Fact

	// lookups are the rules' questions of other packages, each once, the first answer kept: what a package declares
	// does not change during a run, so a second ask answers the same.
	lookups map[lookupKey]Lookup

	// never is the first reason the package can never be restored, empty while it can.
	never string
}

// Fact is one fact an analyzer on the package asked of another package: of Object, or of Package itself when Object
// is nil. Found says whether the fact was there; a miss is a read too, since a build where the fact is there would
// analyze the package differently. Value is the fact found, nil on a miss.
type Fact struct {
	Object   types.Object
	Package  *types.Package
	FactType reflect.Type
	Found    bool
	Value    analysis.Fact
}

// The questions a Lookup asks, by its Query.
const (
	// QueryScope asks the package at Path for what it declares as Name.
	QueryScope = "Scope"

	// QueryClosure asks the package's import closure for the package at Path, and that package for what it declares
	// as Name, as a rule finds io.Reader without importing io.
	QueryClosure = "Closure"

	// QueryInterface asks the import closure for an interface, of another package or the universe, that names
	// Subject, a method of the package, and that the method's receiver implements: Answer is the interface's type
	// name.
	QueryInterface = "Interface"

	// QueryMethod asks the import closure for a method of another package that Subject, a method of the package, has
	// to match, by the asking rule's measure (its name and signature, and what its receiver implements): Answer is
	// the method found, as its package declares it.
	QueryMethod = "Method"

	// QueryPackageName asks whether a package of the import closure is named Name.
	QueryPackageName = "PackageName"

	// QueryDeprecated asks whether Subject, an object of the package at Path, or that package itself when Subject is
	// nil, is deprecated: Text is the deprecation's message.
	QueryDeprecated = "Deprecated"
)

// Lookup is one question a rule asked of other packages past its package's syntax, and its answer. A miss is a read
// too, since a build where the answer is there would analyze the package differently. Query is which question (see
// QueryScope and the rest), Path the package asked of, by path, Name the name asked for, and Subject the object asked
// about, each empty where the question has none. Found says whether there was an answer: Answer is the object found,
// nil on a miss or when the answer is text, which Text holds.
type Lookup struct {
	Query   string
	Path    string
	Name    string
	Subject types.Object
	Found   bool
	Answer  types.Object
	Text    string
}

// lookupKey is what makes two lookups the same question.
type lookupKey struct {
	query   string
	path    string
	name    string
	subject types.Object
}

// factKey is what makes two fact reads the same read.
type factKey struct {
	object      types.Object
	owner       *types.Package
	factType    reflect.Type
	packageFact bool
}

// AddObject records that the package's type check reached object, another package's.
func (reads *Reads) AddObject(object types.Object) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.objects == nil {
		reads.objects = map[types.Object]bool{}
	}
	reads.objects[object] = true
}

// AddFact records one fact read, keeping the first of reads alike.
func (reads *Reads) AddFact(fact Fact) {
	reads.AddFacts([]Fact{fact})
}

// AddFacts records fact reads, under one hold of the lock, as an action publishes the reads it gathered, keeping the
// first of reads alike.
func (reads *Reads) AddFacts(facts []Fact) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.facts == nil {
		reads.facts = make(map[factKey]Fact, len(facts))
	}
	for _, fact := range facts {
		key := factKey{object: fact.Object, owner: fact.Package, factType: fact.FactType,
			packageFact: fact.Object == nil}
		if _, recorded := reads.facts[key]; !recorded {
			reads.facts[key] = fact
		}
	}
}

// AddLookup records one question a rule asked of other packages, keeping the first of questions alike. Nil reads, as
// a run that records nothing hands a rule, record nothing.
func (reads *Reads) AddLookup(lookup Lookup) {
	if reads == nil {
		return
	}
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.lookups == nil {
		reads.lookups = map[lookupKey]Lookup{}
	}
	// A miss has no answer: a caller handing its typed nil, a (*types.TypeName)(nil), would otherwise leave Answer an
	// interface that is not nil.
	if !lookup.Found {
		lookup.Answer = nil
	}
	key := lookupKey{query: lookup.Query, path: lookup.Path, name: lookup.Name, subject: lookup.Subject}
	if _, recorded := reads.lookups[key]; !recorded {
		reads.lookups[key] = lookup
	}
}

// ScopeLookup is what declaring declares as name, nil when it declares nothing so, as declaring.Scope().Lookup(name)
// answers, the question recorded when declaring is another package than own. It is how a rule looks into another
// package's scope by name.
func (reads *Reads) ScopeLookup(own *types.Package, declaring *types.Package, name string) types.Object {
	found := declaring.Scope().Lookup(name)
	if declaring != own {
		reads.AddLookup(Lookup{Query: QueryScope, Path: declaring.Path(), Name: name, Found: found != nil,
			Answer: found})
	}
	return found
}

// RecordTypes claims and runs the walk of own's type information, info, at once (see Claim and Record). A package
// already marked never is not walked, nor one whose walk another has claimed.
func (reads *Reads) RecordTypes(own *types.Package, info *types.Info) {
	if reads.Claim() {
		reads.Record(own, info)
	}
}

// Claim takes the walk of the package's type reads for its caller, and reports whether it did: never when the reads
// are marked never, nor when another has taken the walk already. A caller that runs the walk later, on another
// goroutine, claims it first, so no other walks the package meanwhile, and nothing reads its type information while
// the walk's goroutine lets it go.
func (reads *Reads) Claim() bool {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.claimed || reads.never != "" {
		return false
	}
	reads.claimed = true
	return true
}

// Claimed reports whether the walk is taken already, or the reads are marked never, so there is nothing to walk.
func (reads *Reads) Claimed() bool {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	return reads.claimed || reads.never != ""
}

// Record walks own's type information, info, for every object of another package it reaches (see collectTypeReads),
// and publishes what it collected, marking the reads recorded. The caller holds the walk's claim (Claim).
func (reads *Reads) Record(own *types.Package, info *types.Info) {
	reads.publishTypes(collectTypeReads(own, info))
}

// publishTypes adds collected to the reads under one hold of the lock, and marks them recorded.
func (reads *Reads) publishTypes(collected typeReads) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.objects == nil {
		reads.objects = collected.objects
	} else {
		maps.Copy(reads.objects, collected.objects)
	}
	reads.recorded = true
}

// Recorded reports whether the package's type reads have been walked.
func (reads *Reads) Recorded() bool {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	return reads.recorded
}

// Restorable reports whether the package may be restored by its reads, and when it may not, why: the never mark's
// reason, or, for reads never recorded, that they are unknown, which is never read as empty.
func (reads *Reads) Restorable() (bool, string) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	switch {
	case reads.never != "":
		return false, reads.never
	case !reads.recorded:
		return false, "its type reads were never recorded"
	}
	return true, ""
}

// MarkNever records that the package can never be restored, and why, unless a reason is recorded already.
func (reads *Reads) MarkNever(reason string) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.never == "" {
		reads.never = reason
	}
}

// Never is the first reason the package can never be restored, empty while it can.
func (reads *Reads) Never() string {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	return reads.never
}

// Objects are the foreign objects the package's type check reached, sorted (see compareObjects).
func (reads *Reads) Objects() []types.Object {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	objects := make([]types.Object, 0, len(reads.objects))
	for object := range reads.objects {
		objects = append(objects, object)
	}
	slices.SortFunc(objects, compareObjects)
	return objects
}

// Facts are the fact reads, sorted by owner, then object (a package fact first), fact type and outcome.
func (reads *Reads) Facts() []Fact {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	facts := make([]Fact, 0, len(reads.facts))
	for _, fact := range reads.facts {
		facts = append(facts, fact)
	}
	slices.SortFunc(facts, func(left, right Fact) int {
		if order := cmp.Compare(left.Package.Path(), right.Package.Path()); order != 0 {
			return order
		}
		switch {
		case left.Object == nil && right.Object != nil:
			return -1
		case left.Object != nil && right.Object == nil:
			return 1
		case left.Object != nil:
			if order := compareObjects(left.Object, right.Object); order != 0 {
				return order
			}
		}
		if order := cmp.Compare(left.FactType.String(), right.FactType.String()); order != 0 {
			return order
		}
		switch {
		case left.Found == right.Found:
			return 0
		case left.Found:
			return 1
		default:
			return -1
		}
	})
	return facts
}

// Lookups are the rules' questions of other packages, sorted by question, package, name and subject.
func (reads *Reads) Lookups() []Lookup {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	lookups := make([]Lookup, 0, len(reads.lookups))
	for _, lookup := range reads.lookups {
		lookups = append(lookups, lookup)
	}
	slices.SortFunc(lookups, func(left, right Lookup) int {
		if order := cmp.Or(cmp.Compare(left.Query, right.Query), cmp.Compare(left.Path, right.Path),
			cmp.Compare(left.Name, right.Name)); order != 0 {
			return order
		}
		switch {
		case left.Subject == nil && right.Subject == nil:
			return 0
		case left.Subject == nil:
			return -1
		case right.Subject == nil:
			return 1
		}
		return compareObjects(left.Subject, right.Subject)
	})
	return lookups
}

// compareObjects orders two objects by their package's path, then their name, then how go/types writes them, which
// tells apart two fields or methods of one name on different types, and last by position, for two written alike.
func compareObjects(left types.Object, right types.Object) int {
	order := cmp.Or(cmp.Compare(left.Pkg().Path(), right.Pkg().Path()), cmp.Compare(left.Name(), right.Name()))
	if order != 0 {
		return order
	}
	// Written only for two of one package and name, which is rare, so a sort writes almost nothing.
	return cmp.Or(
		cmp.Compare(types.ObjectString(left, nil), types.ObjectString(right, nil)),
		cmp.Compare(left.Pos(), right.Pos()),
	)
}
