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
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"reflect"
	"slices"
	"strconv"
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
//
// Each sorted list, Uses, Selections, Facts and Lookups, is kept the first time it is asked and handed to every later
// caller, the same slice, until a read of its kind is added. The verdicts of every extra build ask the host's reads
// for the same lists (check.TypesHold, check.FactsHold and check.ReadFactTypes), and copying and sorting them anew for
// each build cost a run at cohere's root, with every build and no findings cache, 0.06 s of wall, 0.93 s of user
// time, 0.36 GB and 2.6 million objects allocated, and about 160 MiB of its peak, measured as the pair of release
// binaries with and without the kept lists (#f7qsmdf). A host alone asks each list once and reads level.
//
// So a list is shared, and a caller ranges over it and never sorts it, appends to it or writes an element: ranging is
// all those three do. That is safe for builds running side by side, since the lock is held for the read of a kept
// list and for the build of one, and a kept list is never written again. A read added drops the list, the next ask
// builds another slice, and a caller still ranging over the one it was handed reads it as it was. A kept list holds
// alive the objects the map beside it already holds, for as long as the Reads lives, which for the host's is until
// check.HostRun.Release lets its entries go: nothing new is kept alive, only the slices themselves.
type Reads struct {
	lock sync.Mutex

	// claimed is set once a walk of the package's type reads has been taken (Claim), and recorded once it has
	// published them.
	claimed  bool
	recorded bool

	// uses are the foreign objects the package's type check reached, each with every kind of use it made of it, and
	// selections the selections it made through another package's named type, each once.
	uses       map[ObjectUse]bool
	selections map[selectionIdentity]Selection

	// sortedUses and sortedSelections are uses and selections as Uses and Selections sort them, each kept from its
	// first ask until its map gains an entry, and nil while not kept, which an empty list kept is not. A kept list is
	// handed out, so it is dropped and never written.
	sortedUses       []ObjectUse
	sortedSelections []Selection

	// closureOnly is why every read of the package is judged by its closure, empty while each is judged by its kind.
	closureOnly string

	// facts are the fact reads, each object or package and fact type once, the first read's outcome kept: a fact of
	// another package is inherited before any pass runs, so a second read of it reads the same.
	facts map[factKey]Fact

	// sortedFacts is facts as Facts sorts them, kept and dropped as sortedUses is, nil while not kept.
	sortedFacts []Fact

	// lookups are the rules' questions of other packages, each once, the first answer kept: what a package declares
	// does not change during a run, so a second ask answers the same.
	lookups map[lookupKey]Lookup

	// sortedLookups is lookups as Lookups sorts them, kept and dropped as sortedUses is, nil while not kept.
	sortedLookups []Lookup

	// never is the first reason the package can never be restored, empty while it can.
	never string

	// functionUses are what each top-level declaration of the package uses, its signature and its body apart
	// (#kr1m304), recorded in its type reads' walk, nil while unrecorded: unrecorded uses are unknown, never empty.
	// recordsFunctionUses is whether the walk records them (RecordFunctionUses), unset in a run that reads none.
	functionUses        *FunctionUses
	recordsFunctionUses bool

	// bodyFileSet is the file set the walk renders the package's bodies by, for the body differential (#w72zxtq),
	// nil in a run that renders none (RenderBodies), bodyNamedTypes what writes the named types beside each
	// expression's type, and bodyRenderings what it rendered, nil until then.
	bodyFileSet    *token.FileSet
	bodyNamedTypes func(types.Type) string
	bodyRenderings *BodyRenderings
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

// selectionIdentity is what makes two selections the same: the receiver's type name by identity, the name selected,
// and the path through embedded fields, each index written in decimal and ended by a comma, so two paths are one
// identity only when they are one path. Within one package's reads a receiver and a name already fix the path: the
// receiver is a named type's origin, never an unnamed struct, and the one way a name reaches two paths, unexported
// names of two packages promoted into one type, never meets in one package's reads, which select only its own
// unexported names. The path is kept in the key so that rests on no such argument.
type selectionIdentity struct {
	receiver *types.TypeName
	name     string
	index    string
}

// selectionKey is selection's identity, which formats no address: a Sprintf of the three, receiver by %p, cost every
// selection of every package a format and its allocations (#deb8gq9).
func selectionKey(selection Selection) selectionIdentity {
	index := make([]byte, 0, 4*len(selection.Index))
	for _, step := range selection.Index {
		index = strconv.AppendInt(index, int64(step), 10)
		index = append(index, ',')
	}
	return selectionIdentity{receiver: selection.Receiver, name: selection.Name, index: string(index)}
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
//
// Rule is the name the asking rule was registered under, which a Method or Interface lookup names, since each rule
// answers those by its own measure, so two rules asking about one subject can answer differently and each answer is
// its own read (#dscc341). Instantiates says the asking rule matched a generic candidate of a Method lookup by
// instantiating it and validating its constraints, which can turn on a method set the lookup's candidates do not
// show, so the verdict keeps such a lookup on the closure judgment when a generic of the subject's shape is there.
type Lookup struct {
	Query        string
	Path         string
	Name         string
	Subject      types.Object
	Found        bool
	Answer       types.Object
	Text         string
	Rule         string
	Instantiates bool
}

// lookupKey is what makes two lookups the same question: one rule's, since a Method or Interface lookup is answered
// by its rule's measure.
type lookupKey struct {
	query   string
	path    string
	name    string
	subject types.Object
	rule    string
}

// factKey is what makes two fact reads the same read.
type factKey struct {
	object      types.Object
	owner       *types.Package
	factType    reflect.Type
	packageFact bool
}

// AddUse records that the package's type check made a use of kind of object, another package's. A use the reads lacked
// drops the kept list of them (Uses), and one they hold leaves it.
func (reads *Reads) AddUse(object types.Object, kind UseKind) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.uses == nil {
		reads.uses = map[ObjectUse]bool{}
	}
	use := ObjectUse{Object: object, Kind: kind}
	if _, recorded := reads.uses[use]; !recorded {
		reads.uses[use] = true
		reads.sortedUses = nil
	}
}

// AddFact records one fact read, keeping the first of reads alike.
func (reads *Reads) AddFact(fact Fact) {
	reads.AddFacts([]Fact{fact})
}

// AddFacts records fact reads, under one hold of the lock, as an action publishes the reads it gathered, keeping the
// first of reads alike. A read the reads lacked drops the kept list of them (Facts), and reads they all hold leave it.
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
			reads.sortedFacts = nil
		}
	}
}

// AddLookup records one question a rule asked of other packages, keeping the first of questions alike. A question
// the reads lacked drops the kept list of them (Lookups), and one they hold leaves it. Nil reads, as a run that
// records nothing hands a rule, record nothing.
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
	// Two rules' Method or Interface lookups of one subject are told apart by their rule alone, so one that names
	// none could be taken for another rule's, whose answer is by another measure.
	if (lookup.Query == QueryMethod || lookup.Query == QueryInterface) && lookup.Rule == "" && reads.never == "" {
		reads.never = "its " + lookup.Query + " lookup of " + lookup.Name + " names no asking rule"
	}
	key := lookupKey{query: lookup.Query, path: lookup.Path, name: lookup.Name, subject: lookup.Subject,
		rule: lookup.Rule}
	if _, recorded := reads.lookups[key]; !recorded {
		reads.lookups[key] = lookup
		reads.sortedLookups = nil
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
// and the kind of each use, and in the same pass, when asked (RecordFunctionUses), for what each top-level declaration
// uses (see collectTypeReads), and publishes what it collected, marking the reads recorded. The caller holds the
// walk's claim (Claim), and the syntax is whole: the kinds are read from where each expression sits.
func (reads *Reads) Record(own *types.Package, info *types.Info, files []*ast.File) {
	reads.lock.Lock()
	recordsFunctionUses, fileSet, namedTypes := reads.recordsFunctionUses, reads.bodyFileSet, reads.bodyNamedTypes
	reads.lock.Unlock()
	reads.publishTypes(collectTypeReads(own, info, files, recordsFunctionUses))
	if fileSet == nil {
		return
	}
	rendered := RenderBodies(fileSet, own, info, files, nil, namedTypes)
	reads.lock.Lock()
	defer reads.lock.Unlock()
	reads.bodyRenderings = rendered
}

// RecordFunctionUses has the walk record what each top-level declaration uses as well (FunctionUses), for stage 2's
// body reach census and its body differential (#w72zxtq): asked before the walk, under either alone, since nothing
// else reads the record, so a plain run pays for none.
func (reads *Reads) RecordFunctionUses() {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	reads.recordsFunctionUses = true
}

// RenderBodiesBy has the walk render the package's bodies as well, each placed by fileSet, the load's, with the named
// types each expression's type names written by namedTypes, for the body differential (BodyRenderings): asked before
// the walk, under the differential alone, so a plain run renders nothing.
func (reads *Reads) RenderBodiesBy(fileSet *token.FileSet, namedTypes func(types.Type) string) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	reads.bodyFileSet, reads.bodyNamedTypes = fileSet, namedTypes
}

// BodyRenderings are the package's bodies as its walk rendered them, and whether it did.
func (reads *Reads) BodyRenderings() (*BodyRenderings, bool) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	return reads.bodyRenderings, reads.bodyRenderings != nil
}

// FunctionUses are what each top-level declaration of the package uses, its signature and its body apart, and
// whether they were recorded: unrecorded, they are nil, which is unknown, never empty, as they are when never asked
// for (RecordFunctionUses).
func (reads *Reads) FunctionUses() (*FunctionUses, bool) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	return reads.functionUses, reads.functionUses != nil
}

// publishTypes adds collected to the reads under one hold of the lock, marks them recorded, and keeps what each
// declaration uses, when it was recorded. It drops the kept lists of uses and of selections (Uses, Selections), both
// and whatever it added: a walk publishes once, so nothing is saved by telling whether a map gained an entry.
func (reads *Reads) publishTypes(collected typeReads) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	reads.sortedUses, reads.sortedSelections = nil, nil
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
	reads.functionUses = collected.functionUses
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

// Uses are the uses the package's type check made of other packages' objects, sorted by object (see
// objectOrdering.compare), then kind.
//
// The list is kept and shared (see Reads): every caller gets the same slice until a use is added, so a caller ranges
// over it and never sorts it, appends to it or writes an element. It is built under the lock and never written after,
// which makes it safe for builds asking side by side, and it is as long as its capacity, every use appended to a
// slice made for as many, so a caller's append copies it and cannot write into the kept array. Reads with no uses
// keep an empty list that is not nil, since a nil list is one not kept.
func (reads *Reads) Uses() []ObjectUse {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.sortedUses != nil {
		return reads.sortedUses
	}
	uses := make([]ObjectUse, 0, len(reads.uses))
	for use := range reads.uses {
		uses = append(uses, use)
	}
	var ordering objectOrdering
	slices.SortFunc(uses, func(left, right ObjectUse) int {
		return cmp.Or(ordering.compare(left.Object, right.Object), cmp.Compare(left.Kind, right.Kind))
	})
	reads.sortedUses = uses
	return uses
}

// Selections are the selections the package made through other packages' named types, sorted by receiver, name and
// index.
//
// The list is kept and shared as Uses' is (see Reads): the same slice for every caller until the walk publishes, to
// range over and never to sort, append to or write, a selection's Index included. It is built under the lock, never
// written after, and as long as its capacity, and reads with no selections keep an empty list, never nil.
func (reads *Reads) Selections() []Selection {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.sortedSelections != nil {
		return reads.sortedSelections
	}
	selections := make([]Selection, 0, len(reads.selections))
	for _, selection := range reads.selections {
		selections = append(selections, selection)
	}
	var ordering objectOrdering
	slices.SortFunc(selections, func(left, right Selection) int {
		return cmp.Or(ordering.compare(left.Receiver, right.Receiver), cmp.Compare(left.Name, right.Name),
			slices.Compare(left.Index, right.Index))
	})
	reads.sortedSelections = selections
	return selections
}

// Facts are the fact reads, sorted by owner, then object (a package fact first), fact type and outcome.
//
// The list is kept and shared as Uses' is (see Reads): the same slice for every caller until a fact read is added, to
// range over and never to sort, append to or write. It is built under the lock, never written after, and as long as
// its capacity, and reads with no fact reads keep an empty list, never nil.
func (reads *Reads) Facts() []Fact {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.sortedFacts != nil {
		return reads.sortedFacts
	}
	facts := make([]Fact, 0, len(reads.facts))
	for _, fact := range reads.facts {
		facts = append(facts, fact)
	}
	var ordering objectOrdering
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
			if order := ordering.compare(left.Object, right.Object); order != 0 {
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
	reads.sortedFacts = facts
	return facts
}

// Lookups are the rules' questions of other packages, sorted by question, package, name, subject and rule.
//
// The list is kept and shared as Uses' is (see Reads): the same slice for every caller until a question is added, to
// range over and never to sort, append to or write. It is built under the lock, never written after, and as long as
// its capacity, and reads with no lookups keep an empty list, never nil.
func (reads *Reads) Lookups() []Lookup {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.sortedLookups != nil {
		return reads.sortedLookups
	}
	lookups := make([]Lookup, 0, len(reads.lookups))
	for _, lookup := range reads.lookups {
		lookups = append(lookups, lookup)
	}
	var ordering objectOrdering
	slices.SortFunc(lookups, func(left, right Lookup) int {
		if order := cmp.Or(cmp.Compare(left.Query, right.Query), cmp.Compare(left.Path, right.Path),
			cmp.Compare(left.Name, right.Name)); order != 0 {
			return order
		}
		switch {
		case left.Subject == nil && right.Subject == nil:
			return cmp.Compare(left.Rule, right.Rule)
		case left.Subject == nil:
			return -1
		case right.Subject == nil:
			return 1
		}
		return cmp.Or(ordering.compare(left.Subject, right.Subject), cmp.Compare(left.Rule, right.Rule))
	})
	reads.sortedLookups = lookups
	return lookups
}

// Unordered are the reads in no order, each kind as Uses, Selections, Facts and Lookups give it unsorted, for a reader
// whose answer does not depend on the order, as a dependence set's does not: sorting writes out objects of one package
// and name to tell them apart, each type name with all of its type, which cost a set of every package of a large
// workspace more than the rest of it.
func (reads *Reads) Unordered() ([]ObjectUse, []Selection, []Fact, []Lookup) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	return slices.Collect(maps.Keys(reads.uses)), slices.Collect(maps.Values(reads.selections)),
		slices.Collect(maps.Values(reads.facts)), slices.Collect(maps.Values(reads.lookups))
}
