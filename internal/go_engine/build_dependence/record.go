package build_dependence

import (
	"cmp"
	"go/types"
	"slices"
	"strconv"
)

// A package's record names every object instead of holding it (#6a3kjvz), so a record read back from a store, with no
// types, resolves into the same graph as one collected from a live package. An object is named by its package's path
// and a local name: its objectpath; for an unexported package-level object, which objectpath refuses, its name, which a
// scope lookup finds exactly; and for an object a unit of the package declares that neither names, a blank `var _`, an
// `init`, a member of an anonymous type under an unexported var, its declaring unit and its place among that unit's
// declares, `#unit/index`. Neither form can collide with the other: no objectpath and no Go name holds `#`. A
// package-level object's objectpath is its name, so the two first forms agree wherever both name it.
//
// A path resolves to a package ID through the import closure of the package whose record names it (resolvedGraph.
// packageOf), since a package and its test variants share a path and a test binary's closure holds one package per
// path.

// recordedObject is an object as a record names it: its package's path and its local name, and object, the object
// itself in a record collected from a live package, nil in one decoded.
type recordedObject struct {
	path   string
	local  string
	object types.Object
}

// recordedBit is one bit of an object of the record's table (packageRecord.objects), by its index there: its name bit,
// or with underlying set its underlying bit.
type recordedBit struct {
	object     int32
	underlying bool
}

// recordedRead is one reference of a unit's body (bodyWalk): target, the object read, by its index in the record's
// table, with the bit the read touches, and through, what reading it reaches through its type, which resolution reads
// in its place when no unit of its package declares it (resolvedGraph.resolveRead). A read of an object no unit of
// its own package declares has no target (-1), only its expansion, and a read of an object of another package carries
// its expansion unless every object of its kind has a declaring unit: a package-level object and a concrete method.
type recordedRead struct {
	target     int32
	underlying bool
	through    []recordedBit
}

// recordedUnit is a unit as its record holds it: unitRecord's place and size, the local names of the objects it
// declares, its receiver type's local name, empty for no method, its reads, and its static callees, each one a unit
// of the graph may declare, by its index in the record's table.
type recordedUnit struct {
	file          string
	start         int
	end           int
	nodes         int
	universeNames uint64

	declares []recordedObject
	receiver string
	reads    []recordedRead
	callees  []int32
}

// recordedEdge is a kind edge as its record holds it: from, the local name of an object a unit of the package
// declares, and to, any object a unit of the graph may declare, by its index in the record's table.
type recordedEdge struct {
	from           string
	fromUnderlying bool
	to             int32
	toUnderlying   bool
}

// declaresEvery reports whether every object of object's kind has a unit declaring it in a package the graph holds:
// a package-level object, and a method of a type rather than an interface. A field or an interface method may be a
// member of an anonymous type no unit's header holds, and then is read through its type.
func declaresEvery(object types.Object) bool {
	if packageLevel(object) {
		return true
	}
	function, isFunction := object.(*types.Func)
	if !isFunction {
		return false
	}
	receiver := function.Signature().Recv()
	return receiver != nil && !types.IsInterface(receiver.Type())
}

// packageLevel reports whether object is one of its package's scope.
func packageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Pkg().Scope().Lookup(object.Name()) == object
}

// newRecord names the units and kind edges collected of one live package (collector) into its record: its ID, path,
// scope, import map and type names, each object named, and the live objects kept beside the names. It names them with
// scratch's namer and walker, and every slice it gives the record is its own, never one of units or of scratch, which
// the next package's walk writes over.
func newRecord(
	identifier string,
	workspace bool,
	typesPackage *types.Package,
	imports map[string]string,
	encoder *objectPaths,
	units []unitRecord,
	edges []kindEdge,
	scratch *workerScratch,
) *packageRecord {
	record := &packageRecord{identifier: identifier, workspace: workspace, typesPackage: typesPackage,
		imports: imports}
	if typesPackage == nil {
		return record
	}
	record.path = typesPackage.Path()
	for _, name := range typesPackage.Scope().Names() {
		if _, isTypeName := typesPackage.Scope().Lookup(name).(*types.TypeName); isTypeName {
			record.typeNames = append(record.typeNames, name)
		}
	}
	naming := scratch.namer(typesPackage, encoder, record)
	for unitIndex := range units {
		for declareIndex, object := range units[unitIndex].declares {
			if _, isDeclared := naming.declared[object]; isDeclared {
				continue
			}
			local := localName(encoder, object)
			if local == "" {
				local = "#" + strconv.Itoa(unitIndex) + "/" + strconv.Itoa(declareIndex)
			}
			naming.declared[object] = local
		}
	}
	// The collector is done with the walker, so the namer walks with it.
	walker := &scratch.walker
	record.units = make([]recordedUnit, 0, len(units))
	for unitIndex := range units {
		unit := &units[unitIndex]
		recorded := recordedUnit{file: unit.file, start: unit.start, end: unit.end, nodes: unit.nodes,
			universeNames: unit.universeNames, declares: make([]recordedObject, 0, len(unit.declares)),
			reads: make([]recordedRead, 0, len(unit.references))}
		for _, object := range unit.declares {
			recorded.declares = append(recorded.declares,
				recordedObject{path: record.path, local: naming.declared[object], object: object})
		}
		if unit.receiver != nil {
			recorded.receiver = naming.nameOf(unit.receiver)
		}
		for _, reference := range unit.references {
			read := naming.read(reference, walker)
			if read.target >= 0 || len(read.through) > 0 {
				recorded.reads = append(recorded.reads, read)
			}
		}
		naming.callees = naming.callees[:0]
		for _, callee := range unit.callees {
			if universal(callee) {
				continue
			}
			if _, isDeclared := naming.declared[callee]; naming.own(callee) && !isDeclared {
				continue
			}
			if index, isNamed := naming.recorded(callee); isNamed {
				naming.callees = append(naming.callees, index)
			}
		}
		// A copy at its size, nil for none, so the record holds nothing of the scratch's slice.
		if len(naming.callees) > 0 {
			recorded.callees = slices.Clone(naming.callees)
		}
		record.units = append(record.units, recorded)
	}
	for _, edge := range edges {
		from, isDeclared := naming.declared[edge.from]
		if !isDeclared {
			continue
		}
		// An edge to an object of the package's own that no unit declares is dropped, as resolution drops one.
		if _, isTargetDeclared := naming.declared[edge.to]; naming.own(edge.to) && !isTargetDeclared {
			continue
		}
		to, isNamed := naming.recorded(edge.to)
		if !isNamed {
			continue
		}
		naming.edges = append(naming.edges, recordedEdge{from: from, fromUnderlying: edge.fromUnderlying, to: to,
			toUnderlying: edge.toUnderlying})
	}
	// A copy at its size, nil for none, so the record holds nothing of the scratch's slice.
	if len(naming.edges) > 0 {
		record.edges = slices.Clone(naming.edges)
	}
	record.unnameable = naming.unnameable
	naming.order()
	return record
}

// compareReads orders reads by target, the bit touched, then what each reaches through its type, every object by its
// index in the record's table, which order puts in the order of the objects' names.
func compareReads(left recordedRead, right recordedRead) int {
	return cmp.Or(cmp.Compare(left.target, right.target), compareFlags(left.underlying, right.underlying),
		slices.CompareFunc(left.through, right.through, func(leftBit recordedBit, rightBit recordedBit) int {
			return cmp.Or(cmp.Compare(leftBit.object, rightBit.object),
				compareFlags(leftBit.underlying, rightBit.underlying))
		}))
}

// compareObjects orders recorded objects by path, then local name.
func compareObjects(left recordedObject, right recordedObject) int {
	return cmp.Or(cmp.Compare(left.path, right.path), cmp.Compare(left.local, right.local))
}

// compareFlags orders false before true.
func compareFlags(left bool, right bool) int {
	switch {
	case left == right:
		return 0
	case left:
		return 1
	}
	return -1
}
