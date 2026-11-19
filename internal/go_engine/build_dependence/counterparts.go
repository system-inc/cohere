package build_dependence

import (
	"go/types"
	"reflect"

	"golang.org/x/tools/go/types/objectpath"
)

// Counterparts answers one graph's Difference.DependentObject for the objects of packages it does not hold, read from
// export data, by their counterparts in another graph's judgment of the same build (#ss4t5g6): the engine's host graph
// holds what the host checked from source, and the standard library it restored from the system cache is judged on a
// graph of its own, loaded from source beside it. An object is found by its package's path, then its objectpath in the
// package as the other graph holds it, and answered with that object's bits (Dependence.ObjectBits). An object with
// no counterpart answers dependent on both bits, failing wide, and is counted: one objectpath cannot name, a function's
// local type in an exported signature say, one the path names nothing of or something else in the other graph's
// package, and any object of a package the other graph does not hold.
//
// It is one build's, asked by one judgment at a time, which asks each object once.
type Counterparts struct {
	judged   *Dependence
	packages map[string]*types.Package
	encoder  objectpath.Encoder
	counts   CounterpartCounts

	// decoded are the paths of the packages the other graph holds by records read back from the store, with no
	// types (#6a3kjvz), whose objects DependentObject answers by name (objectByName).
	decoded map[string]bool
	paths   objectPaths
	walker  *bitWalker
}

// CounterpartCounts are the objects a Counterparts was asked of: those answered by their counterpart's bits, those
// without a counterpart in a package the other graph holds, and those of a package it does not hold, the last two
// answered dependent on both bits.
type CounterpartCounts struct {
	Answered           int
	WithoutCounterpart int
	WithoutPackage     int
}

// NewCounterparts finds counterparts in graph's packages, by their paths, and answers by judged, graph's judgment of
// the build. graph is nil when there is no other graph, and then every object fails wide, counted without its
// package; judged is nil for a caller that only finds counterparts (Counterpart), and then DependentObject fails wide
// too.
func NewCounterparts(graph *Graph, judged *Dependence) *Counterparts {
	counterparts := &Counterparts{judged: judged, packages: map[string]*types.Package{}, decoded: map[string]bool{},
		walker: &bitWalker{seen: map[walkKey]bool{}}}
	if graph == nil {
		return counterparts
	}
	graph.lock.Lock()
	defer graph.lock.Unlock()
	for _, record := range graph.packages {
		switch {
		case record.typesPackage != nil:
			counterparts.packages[record.typesPackage.Path()] = record.typesPackage
		case record.path != "":
			counterparts.decoded[record.path] = true
		}
	}
	return counterparts
}

// DependentObject is object's name and underlying bits, by its counterpart, or both set when it has none. It has
// Difference.DependentObject's shape, which a graph's judgment asks.
func (counterparts *Counterparts) DependentObject(object types.Object) (bool, bool) {
	// The universe's objects are the same in every build, and no graph asks for one; this keeps a caller that does
	// from reading a nil package.
	if universal(object) {
		return false, false
	}
	if counterparts.decoded[object.Pkg().Path()] && counterparts.judged != nil {
		return counterparts.objectByName(object)
	}
	if _, isHeld := counterparts.packages[object.Pkg().Path()]; !isHeld || counterparts.judged == nil {
		counterparts.counts.WithoutPackage++
		return true, true
	}
	counterpart, isFound := counterparts.Counterpart(object)
	if !isFound {
		counterparts.counts.WithoutCounterpart++
		return true, true
	}
	counterparts.counts.Answered++
	bits := counterparts.judged.ObjectBits(counterpart)
	return bits.Name, bits.Underlying
}

// DependentName is the bits of the object a decoded record names by path and local name, by its counterpart in the
// other graph, and whether one of the other graph's units declares it (#6a3kjvz): it has Difference.DependentName's
// shape. An object of a package the other graph does not hold answers both bits set, failing wide, and is counted
// without its package; one of a package it holds that no unit there declares answers nothing set, for the record's
// expansion of it to be read in its place, and is counted answered, as ObjectBits answers it through its type.
func (counterparts *Counterparts) DependentName(path string, local string) (bool, bool, bool) {
	_, isHeld := counterparts.packages[path]
	if !isHeld && !counterparts.decoded[path] || counterparts.judged == nil {
		counterparts.counts.WithoutPackage++
		return true, true, false
	}
	counterparts.counts.Answered++
	bits, isDeclared := counterparts.judged.NameBits(path, local)
	return bits.Name, bits.Underlying, isDeclared
}

// objectByName is a live object's bits, of a package the other graph holds by a record read back from the store
// (#6a3kjvz): the bits of the object its name names there, as a record names it (localName), or, where no unit there
// declares it or it has no name, any bit set that reading it reaches through its type, both bits at once, as
// Dependence.ObjectBits reads an object no unit declares.
func (counterparts *Counterparts) objectByName(object types.Object) (bool, bool) {
	declared := canonical(object)
	if local := localName(&counterparts.paths, declared); local != "" {
		if bits, isDeclared := counterparts.judged.NameBits(declared.Pkg().Path(), local); isDeclared {
			counterparts.counts.Answered++
			return bits.Name, bits.Underlying
		}
	}
	counterparts.counts.Answered++
	_, isConstant := declared.(*types.Const)
	for _, reached := range counterparts.walker.references(declared.Type(), isConstant) {
		local := localName(&counterparts.paths, reached.object)
		if local == "" {
			return true, true
		}
		bits, isDeclared := counterparts.judged.NameBits(reached.object.Pkg().Path(), local)
		if !isDeclared {
			_, isHeld := counterparts.packages[reached.object.Pkg().Path()]
			if !isHeld && !counterparts.decoded[reached.object.Pkg().Path()] {
				return true, true
			}
			continue
		}
		if bits.Name && !reached.touchesUnderlying || bits.Underlying && reached.touchesUnderlying {
			return true, true
		}
	}
	return false, false
}

// Counterpart is object's counterpart in the other graph's package of its path, found by its objectpath, and false
// when that graph holds no such package or the path names nothing alike there. It counts nothing, so the facts census
// can find the objects facts are about and the functions units call (#ss4t5g6).
func (counterparts *Counterparts) Counterpart(object types.Object) (types.Object, bool) {
	if universal(object) {
		return nil, false
	}
	counterpartPackage, isHeld := counterparts.packages[object.Pkg().Path()]
	if !isHeld {
		return nil, false
	}
	// unsafe is the type checker's own package in every load, so its objects are their own counterparts.
	if counterpartPackage == object.Pkg() {
		return object, true
	}
	path, pathError := counterparts.encoder.For(object)
	if pathError != nil {
		return nil, false
	}
	found, findError := objectpath.Object(counterpartPackage, path)
	// A path names an object by its place among its siblings, a method by its index say, so the object found must be
	// the same kind of object under the same name, or the two packages are not alike there.
	if findError != nil || found.Name() != object.Name() || reflect.TypeOf(found) != reflect.TypeOf(object) {
		return nil, false
	}
	return found, true
}

// ObjectAt is the object path names in the other graph's package of packagePath, and false when that graph holds no
// such package or path names nothing there: a restored package's fact about an object its export data does not hold
// is named by its path alone.
func (counterparts *Counterparts) ObjectAt(packagePath string, path objectpath.Path) (types.Object, bool) {
	counterpartPackage, isHeld := counterparts.packages[packagePath]
	if !isHeld {
		return nil, false
	}
	found, findError := objectpath.Object(counterpartPackage, path)
	return found, findError == nil
}

// Counts are the objects asked so far.
func (counterparts *Counterparts) Counts() CounterpartCounts {
	return counterparts.counts
}
