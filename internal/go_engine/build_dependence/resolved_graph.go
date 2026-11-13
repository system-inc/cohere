package build_dependence

import (
	"go/types"
	"slices"
)

// resolvedGraph is the graph's records resolved into nodes: every object a unit declares has a node, and each node two
// bits, its name bit at twice its index and its underlying bit just after, a non-type object using only the first.
// It is built once for the packages added so far and read, never written, by every judgment on it, so judgments can
// run concurrently.
type resolvedGraph struct {
	// packages are the records, in package ID order, so a node's index never hangs on the order packages were added.
	packages     []*packageRecord
	packageIndex map[string]int

	// typesPackages are the packages the graph holds by their types, to tell an object no unit of a held package
	// declares from one of a package the graph does not hold, which only the build's DependentObject can answer.
	typesPackages map[*types.Package]bool

	nodes   map[types.Object]int32
	objects []types.Object

	units []resolvedUnit

	// packageUnits are, for each package, the half-open range of its units.
	packageUnits [][2]int

	// unitIndex is each unit by its package ID, file and start offset, and fileUnits each file's units by package ID
	// and file, in the order of their start offsets, for the unit holding an offset (Dependence.UnitContaining).
	unitIndex map[unitKey]int
	fileUnits map[fileKey][]int

	// declaringUnit is, for each node, the unit that declares it.
	declaringUnit []int

	// dependents are, for each bit, the bits that depend on it (rule 3).
	dependents [][]int32

	// externalEdges are the edges to bits of objects of packages the graph does not hold, set when the build's
	// DependentObject says so.
	externalEdges []externalEdge
}

// resolvedUnit is a unit with its declared objects, its receiver and its references as bits: references to objects of
// packages the graph does not hold stay objects (external), for the build's DependentObject.
type resolvedUnit struct {
	packageIndex int
	record       *unitRecord
	declares     []int32
	receiver     int32
	bits         []int32
	external     []reference

	// calls are the units the unit's body calls statically (unitRecord.callees), each once, never itself: a callee no
	// unit of the graph declares is left out, and one of a package the graph does not hold, a dependency's checked
	// without syntax, is kept in externalCalls, for another graph to find its unit (#ss4t5g6).
	calls         []int
	externalCalls []types.Object
}

// externalEdge is an edge from a bit of the graph to a bit of an object of a package it does not hold.
type externalEdge struct {
	fromBit int32
	to      reference
}

// fileKey is a file of a package: its package ID and the file a reader sees.
type fileKey struct {
	packageIdentifier string
	file              string
}

// unitKey is a unit's place: its package ID, the file a reader sees it in, and its start offset in the file parsed.
type unitKey struct {
	packageIdentifier string
	file              string
	offset            int
}

// bit is node's name bit, or with underlying its underlying bit.
func bit(node int32, underlying bool) int32 {
	if underlying {
		return 2*node + 1
	}
	return 2 * node
}

// resolveUnits gives every declared object its node and every unit its place, then each method its receiver's node.
func (resolved *resolvedGraph) resolveUnits() {
	for packageIndex, record := range resolved.packages {
		first := len(resolved.units)
		for unitIndex := range record.units {
			unit := &record.units[unitIndex]
			declares := make([]int32, 0, len(unit.declares))
			for _, object := range unit.declares {
				node := resolved.node(object)
				declares = append(declares, node)
				resolved.declaringUnit[node] = len(resolved.units)
			}
			resolved.unitIndex[unitKey{packageIdentifier: record.identifier, file: unit.file, offset: unit.start}] =
				len(resolved.units)
			// A package's units come in the order of its files' declarations, so each file's are in start order.
			inFile := fileKey{packageIdentifier: record.identifier, file: unit.file}
			resolved.fileUnits[inFile] = append(resolved.fileUnits[inFile], len(resolved.units))
			resolved.units = append(resolved.units,
				resolvedUnit{packageIndex: packageIndex, record: unit, declares: declares, receiver: -1})
		}
		resolved.packageUnits = append(resolved.packageUnits, [2]int{first, len(resolved.units)})
	}
	for index := range resolved.units {
		receiver := resolved.units[index].record.receiver
		if receiver == nil {
			continue
		}
		if node, isNode := resolved.nodes[receiver]; isNode {
			resolved.units[index].receiver = node
		}
	}
}

// node is object's node, made if it has none.
func (resolved *resolvedGraph) node(object types.Object) int32 {
	declared := canonical(object)
	if index, isNode := resolved.nodes[declared]; isNode {
		return index
	}
	index := int32(len(resolved.objects))
	resolved.nodes[declared] = index
	resolved.objects = append(resolved.objects, declared)
	resolved.declaringUnit = append(resolved.declaringUnit, -1)
	return index
}

// resolveEdges turns every record's kind edges into the bits that depend on each bit. An edge to an object of a
// package the graph holds that no unit declares is dropped, as the counter dropped every edge to an object without a
// node, and one to a package the graph does not hold is kept for the build's DependentObject.
func (resolved *resolvedGraph) resolveEdges() {
	resolved.dependents = make([][]int32, 2*len(resolved.objects))
	for _, record := range resolved.packages {
		for _, edge := range record.edges {
			from, isNode := resolved.nodes[edge.from]
			if !isNode {
				continue
			}
			fromBit := bit(from, edge.fromUnderlying)
			if to, isNode := resolved.nodes[edge.to]; isNode {
				if toBit := bit(to, edge.toUnderlying); toBit != fromBit {
					resolved.dependents[toBit] = append(resolved.dependents[toBit], fromBit)
				}
				continue
			}
			if resolved.typesPackages[edge.to.Pkg()] {
				continue
			}
			resolved.externalEdges = append(resolved.externalEdges,
				externalEdge{fromBit: fromBit, to: reference{object: edge.to, touchesUnderlying: edge.toUnderlying}})
		}
	}
}

// resolveReference adds read to unit as a bit, or as an external reference when its object is of a package the graph
// does not hold. An object of a package the graph holds that no unit declares, a field of an anonymous struct in a
// var's initializer or a method of an interface in a type parameter's constraint, is read through its type as the
// counter read every object without a node, a constant's in full, as a local is (bodyWalk.addObject).
//
// Mutates: unit gains the bits and external references
func (resolved *resolvedGraph) resolveReference(unit *resolvedUnit, read reference, walker *bitWalker) {
	if node, isNode := resolved.nodes[read.object]; isNode {
		unit.bits = append(unit.bits, bit(node, read.touchesUnderlying))
		return
	}
	if !resolved.typesPackages[read.object.Pkg()] {
		unit.external = append(unit.external, read)
		return
	}
	_, isConstant := read.object.(*types.Const)
	for _, through := range walker.references(read.object.Type(), isConstant) {
		if node, isNode := resolved.nodes[through.object]; isNode {
			unit.bits = append(unit.bits, bit(node, through.touchesUnderlying))
		} else if !resolved.typesPackages[through.object.Pkg()] {
			unit.external = append(unit.external, through)
		}
	}
}

// fixedPoint spreads the seed bits to every bit that depends on one.
func (resolved *resolvedGraph) fixedPoint(seedBits []int32) []bool {
	set := make([]bool, 2*len(resolved.objects))
	pending := make([]int32, 0, len(seedBits))
	for _, seedBit := range seedBits {
		if !set[seedBit] {
			set[seedBit] = true
			pending = append(pending, seedBit)
		}
	}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, dependent := range resolved.dependents[current] {
			if !set[dependent] {
				set[dependent] = true
				pending = append(pending, dependent)
			}
		}
	}
	return set
}

// resolveCalls turns each unit's callees into the units that declare them, each once, never the unit itself, and
// keeps each callee of a package the graph does not hold as it is.
func (resolved *resolvedGraph) resolveCalls() {
	for index := range resolved.units {
		unit := &resolved.units[index]
		for _, callee := range unit.record.callees {
			node, isNode := resolved.nodes[callee]
			if !isNode {
				if !universal(callee) && !resolved.typesPackages[callee.Pkg()] {
					unit.externalCalls = append(unit.externalCalls, callee)
				}
				continue
			}
			declaring := resolved.declaringUnit[node]
			if declaring >= 0 && declaring != index && !slices.Contains(unit.calls, declaring) {
				unit.calls = append(unit.calls, declaring)
			}
		}
	}
}
