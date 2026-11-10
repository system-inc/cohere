// Package build_reads holds what one package's check read of other packages on the host's run, so an extra build
// (linux, windows, freebsd) can restore the package from the host's results when everything it read is the same
// there (#b2t9xmf). The load records the foreign objects a package's type check reached and the type arguments of
// every instance of a foreign generic in it, the checker records each fact an analyzer on it asked of another
// package, hit or miss, and either marks a package that can never be restored, with the first reason. Exactness rests
// on recording every read, so what cannot be recorded marks the package, never passes silently.
//
// Recording collects pointers and renders nothing: rendering and comparing the reads are the restore's.
package build_reads

import (
	"cmp"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// Reads is one package's reads of other packages. The load writes its type reads, then every action on the package
// may add fact reads from its own goroutine, so each method takes the lock.
type Reads struct {
	lock sync.Mutex

	// objects are the foreign objects the package's type check reached, each once.
	objects map[types.Object]bool

	// instances are each foreign generic's instances in the package, each distinct list of type arguments once.
	instances map[types.Object][]*types.TypeList

	// facts are the fact reads, each object or package and fact type once, the first read's outcome kept: a fact of
	// another package is inherited before any pass runs, so a second read of it reads the same.
	facts map[factKey]Fact

	// never is the first reason the package can never be restored, empty while it can.
	never string
}

// Instance is one instance of a foreign generic in the package: the generic, as declared, and its type arguments.
type Instance struct {
	Generic       types.Object
	TypeArguments *types.TypeList
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

// AddInstance records an instance of generic, another package's, with typeArguments, unless an identical one is
// recorded already.
func (reads *Reads) AddInstance(generic types.Object, typeArguments *types.TypeList) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.instances == nil {
		reads.instances = map[types.Object][]*types.TypeList{}
	}
	for _, recorded := range reads.instances[generic] {
		if identicalLists(recorded, typeArguments) {
			return
		}
	}
	reads.instances[generic] = append(reads.instances[generic], typeArguments)
}

// AddFact records one fact read, keeping the first of reads alike.
func (reads *Reads) AddFact(fact Fact) {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	if reads.facts == nil {
		reads.facts = map[factKey]Fact{}
	}
	key := factKey{object: fact.Object, owner: fact.Package, factType: fact.FactType, packageFact: fact.Object == nil}
	if _, recorded := reads.facts[key]; !recorded {
		reads.facts[key] = fact
	}
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

// Instances are the instances of foreign generics in the package, sorted by generic, then by type arguments as
// written.
func (reads *Reads) Instances() []Instance {
	reads.lock.Lock()
	defer reads.lock.Unlock()
	var instances []Instance
	for generic, lists := range reads.instances {
		for _, typeArguments := range lists {
			instances = append(instances, Instance{Generic: generic, TypeArguments: typeArguments})
		}
	}
	slices.SortFunc(instances, func(left, right Instance) int {
		if order := compareObjects(left.Generic, right.Generic); order != 0 {
			return order
		}
		return cmp.Compare(listString(left.TypeArguments), listString(right.TypeArguments))
	})
	return instances
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

// identicalLists reports whether two lists of type arguments are the same types in the same order.
func identicalLists(left *types.TypeList, right *types.TypeList) bool {
	if left.Len() != right.Len() {
		return false
	}
	for index := range left.Len() {
		if !types.Identical(left.At(index), right.At(index)) {
			return false
		}
	}
	return true
}

// listString is a list of type arguments as written, with package paths in full.
func listString(typeArguments *types.TypeList) string {
	written := make([]string, 0, typeArguments.Len())
	for index := range typeArguments.Len() {
		written = append(written, types.TypeString(typeArguments.At(index), nil))
	}
	return strings.Join(written, ", ")
}
