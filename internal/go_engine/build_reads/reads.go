// Package build_reads holds what one package's check read of other packages on the host's run, so an extra build
// (linux, windows, freebsd) can restore the package from the host's results when everything it read is the same
// there (#b2t9xmf). The run records the foreign objects a package's type check reached, each with the kind of use the
// package made of it (#mjdf7ah), the checker records each fact an analyzer on it asked of another package, hit or
// miss, the rules record each question they ask of other packages past the syntax (a lookup), and any of them marks a
// package that can never be restored, with the first reason. Exactness rests on recording every read, so what cannot
// be recorded marks the package, never passes silently, and a use whose kind cannot be told is recorded as a use of
// the whole type.
//
// Recording collects pointers and renders nothing: rendering and comparing the reads are the restore's.
package build_reads

import (
	"cmp"
	"fmt"
	"go/ast"
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

	// uses are the foreign objects the package's type check reached, each with every kind of use it made of it, and
	// selections the selections it made through another package's named type, each once.
	uses       map[ObjectUse]bool
	selections map[string]Selection

	// closureOnly is why every read of the package is judged by its closure, empty while each is judged by its kind.
	closureOnly string

	// facts are the fact reads, each object or package and fact type once, the first read's outcome kept: a fact of
	// another package is inherited before any pass runs, so a second read of it reads the same.
	facts map[factKey]Fact

	// lookups are the rules' questions of other packages, each once, the first answer kept: what a package declares
	// does not change during a run, so a second ask answers the same.
	lookups map[lookupKey]Lookup

	// never is the first reason the package can never be restored, empty while it can.
	never string
}

// UseKind is how a package used an object of another package, which decides what of the object another build must
// render alike for the package's check to read the same there (#mjdf7ah). A use the walk cannot tell the kind of is
// UseWhole, which judges the most.
type UseKind string

const (
	// UseName is a type name named and nothing more of it: a pointer to it, a parameter of a pointer to it. Its name,
	// type parameters and kind (struct, interface, basic, ...) are what the package read, not its fields.
	UseName UseKind = "Name"

	// UseMember is a field selected, a method called, or a function, variable or constant of the package: its own line,
	// its type and signature written by name, and a constant's value.
	UseMember UseKind = "Member"

	// UseUnderlying is an operation on a named type's underlying type, `+`, `len`, an index, `range`: the underlying
	// type's line, its components by name.
	UseUnderlying UseKind = "Underlying"

	// UseMethodSet is a named type's method set checked statically, an interface satisfied or asserted: every method of
	// the type and of a pointer to it an importer can see, promoted ones included, by name, index and signature.
	UseMethodSet UseKind = "MethodSet"

	// UseWhole is a use that sees all of a named type, a copy of its value, its size, a comparison, a conversion, a
	// value of it or a pointer to it handed to an empty interface: the type's whole closure, as every read was judged
	// before kinds.
	UseWhole UseKind = "Whole"
)

// ObjectUse is one kind of use the package made of an object of another package.
type ObjectUse struct {
	Object types.Object
	Kind   UseKind
}

// Selection is one selection the package made through another package's named type: the field or method Name, which
// the type checker found from Receiver, the first named type of another package on the selection's way, at Index, the
// path through embedded fields Receiver's own lookup returns, to Selected, as its package declares it. Another build
// reads the selection the same when its own lookup of Name from Receiver finds a member at the same Index whose own
// line is Selected's: nothing shallower shadows it there, and no twin at the same depth makes it ambiguous.
type Selection struct {
	Receiver *types.TypeName
	Name     string
	Index    []int
	Selected types.Object
}

// selectionKey is what makes two selections the same.
func selectionKey(selection Selection) string {
	return fmt.Sprintf("%p %s %v", selection.Receiver, selection.Name, selection.Index)
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

// AddUse records that the package's type check made a use of kind of object, another package's.
func (reads *Reads) AddUse(object types.Object, kind UseKind) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.uses == nil {
		reads.uses = map[ObjectUse]bool{}
	}
	reads.uses[ObjectUse{Object: object, Kind: kind}] = true
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

// RecordTypes claims and runs the walk of own's type information, info, and its syntax, files, at once (see Claim and
// Record). A package already marked never is not walked, nor one whose walk another has claimed.
func (reads *Reads) RecordTypes(own *types.Package, info *types.Info, files []*ast.File) {
	if reads.Claim() {
		reads.Record(own, info, files)
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

// Record walks own's type information, info, and its syntax, files, for every object of another package it reaches
// and the kind of each use (see collectTypeReads), and publishes what it collected, marking the reads recorded. The
// caller holds the walk's claim (Claim), and the syntax is whole: the kinds are read from where each expression sits.
func (reads *Reads) Record(own *types.Package, info *types.Info, files []*ast.File) {
	reads.publishTypes(collectTypeReads(own, info, files))
}

// publishTypes adds collected to the reads under one hold of the lock, and marks them recorded.
func (reads *Reads) publishTypes(collected typeReads) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.uses == nil {
		reads.uses = collected.uses
	} else {
		maps.Copy(reads.uses, collected.uses)
	}
	if reads.selections == nil {
		reads.selections = collected.selections
	} else {
		maps.Copy(reads.selections, collected.selections)
	}
	if reads.closureOnly == "" {
		reads.closureOnly = collected.closureOnly
	}
	reads.recorded = true
}

// ClosureOnly is why every read of the package is judged by its closure, as if each were a use of the whole type,
// empty while each is judged by its kind: a package whose uses could not be told apart, or that reaches past the
// type checker (go:linkname).
func (reads *Reads) ClosureOnly() string {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	return reads.closureOnly
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

// Uses are the uses the package's type check made of other packages' objects, sorted by object (see compareObjects),
// then kind.
func (reads *Reads) Uses() []ObjectUse {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	uses := make([]ObjectUse, 0, len(reads.uses))
	for use := range reads.uses {
		uses = append(uses, use)
	}
	slices.SortFunc(uses, func(left, right ObjectUse) int {
		return cmp.Or(compareObjects(left.Object, right.Object), cmp.Compare(left.Kind, right.Kind))
	})
	return uses
}

// Selections are the selections the package made through other packages' named types, sorted by receiver, name and
// index.
func (reads *Reads) Selections() []Selection {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	selections := make([]Selection, 0, len(reads.selections))
	for _, selection := range reads.selections {
		selections = append(selections, selection)
	}
	slices.SortFunc(selections, func(left, right Selection) int {
		return cmp.Or(compareObjects(left.Receiver, right.Receiver), cmp.Compare(left.Name, right.Name),
			slices.Compare(left.Index, right.Index))
	})
	return selections
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
// tells apart two fields or methods of one name on different types, and last by position, for two written alike. One
// object compared with itself is equal before anything is written: the selections a package makes through one
// receiver all compare that receiver with itself, as do the uses of one object in several kinds, and writing a large
// struct twice for each of those comparisons doubled every build's decide on deeafbf4 (#p1s9gdk).
func compareObjects(left types.Object, right types.Object) int {
	if left == right {
		return 0
	}
	order := cmp.Or(cmp.Compare(left.Pkg().Path(), right.Pkg().Path()), cmp.Compare(left.Name(), right.Name()))
	if order != 0 {
		return order
	}
	// Written only for two distinct objects of one package and name, two fields or methods of one name on different
	// types, which is rare, so a sort writes almost nothing.
	return cmp.Or(
		cmp.Compare(types.ObjectString(left, nil), types.ObjectString(right, nil)),
		cmp.Compare(left.Pos(), right.Pos()),
	)
}
