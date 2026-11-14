package build_dependence

import (
	"go/types"
	"slices"
	"sort"
)

// resolvedGraph is the graph's records resolved into nodes: every object a unit declares has a node, and each node two
// bits, its name bit at twice its index and its underlying bit just after, a non-type object using only the first.
// A node is keyed by its package's ID and its local name (record.go), so a record decoded from its bytes and one
// collected from a live package resolve alike (#6a3kjvz). It is built once for the packages added so far and read,
// never written, by every judgment on it, so judgments can run concurrently.
type resolvedGraph struct {
	// packages are the records, in package ID order, so a node's index never hangs on the order packages were added.
	packages     []*packageRecord
	packageIndex map[string]int

	// typesPackages are the live packages the graph holds by their types, for the live object API (ObjectBits), which
	// asks of an object rather than a name.
	typesPackages map[*types.Package]bool

	// nodes are the nodes by key, keys each node's key, and objects each node's live object, nil for one only a
	// decoded record declares; objectNodes are the live objects' nodes, for the live object API.
	nodes       map[nodeKey]int32
	keys        []nodeKey
	objects     []types.Object
	objectNodes map[types.Object]int32

	// pathsHeld are the IDs of the packages the graph holds of each path, and resolvedPaths each path a package's
	// record names, resolved to an ID through its import closure (packageOf), each once.
	pathsHeld     map[string][]string
	resolvedPaths map[pathFrom]string

	// targets are, for each package, each object of its record's table resolved once (resolveTargets), which its
	// reads, callees and edges name by index.
	targets [][]objectTarget

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

// nodeKey is a node's key: its package's ID and its local name there.
type nodeKey struct {
	packageIdentifier string
	local             string
}

// objectTarget is one object of a record's table resolved: its node, whether it has one, and whether the graph holds
// its package.
type objectTarget struct {
	node   int32
	isNode bool
	isHeld bool
}

// pathFrom is a path one package's record names, by the package's index.
type pathFrom struct {
	packageIndex int
	path         string
}

// resolvedUnit is a unit with its declared objects, its receiver and its reads as bits: reads of objects of packages
// the graph does not hold stay recorded objects (external), for the build's DependentObject.
type resolvedUnit struct {
	packageIndex int
	record       *recordedUnit
	declares     []int32
	receiver     int32
	bits         []int32
	external     []externalRead

	// calls are the units the unit's body calls statically (recordedUnit.callees), each once, never itself: a callee
	// no unit of the graph declares is left out, and one of a package the graph does not hold, a dependency's checked
	// without syntax, is kept in externalCalls by its path and local name, a live record's and a decoded one's alike,
	// for another graph to find its unit (#ss4t5g6, #6a3kjvz).
	calls         []int
	externalCalls []ObjectName
}

// externalRead is one bit of an object of a package the graph does not hold, which the build's DependentObject
// answers for a live object, and its DependentName for an object a decoded record names, which has none: the object
// as a record names it, its live object nil in a decoded record, and through, what the read reaches through the
// object's type, which DependentName's graph reads in its place when no unit of it declares the object
// (judgment.nameAnswer).
type externalRead struct {
	object     recordedObject
	underlying bool
	through    []externalBit
}

// externalBit is one bit of an object a decoded record names by path and local name.
type externalBit struct {
	object     recordedObject
	underlying bool
}

// externalEdge is an edge from a bit of the graph to a bit of an object of a package it does not hold.
type externalEdge struct {
	fromBit int32
	to      externalRead
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
			for _, declared := range unit.declares {
				node := resolved.node(nodeKey{packageIdentifier: record.identifier, local: declared.local},
					declared.object)
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
		unit := &resolved.units[index]
		if unit.record.receiver == "" {
			continue
		}
		key := nodeKey{packageIdentifier: resolved.packages[unit.packageIndex].identifier, local: unit.record.receiver}
		if node, isNode := resolved.nodes[key]; isNode {
			unit.receiver = node
		}
	}
}

// node is key's node, made if it has none, with object its live object when one is known.
func (resolved *resolvedGraph) node(key nodeKey, object types.Object) int32 {
	if index, isNode := resolved.nodes[key]; isNode {
		if object != nil && resolved.objects[index] == nil {
			resolved.objects[index] = object
			resolved.objectNodes[object] = index
		}
		return index
	}
	index := int32(len(resolved.keys))
	resolved.nodes[key] = index
	resolved.keys = append(resolved.keys, key)
	resolved.objects = append(resolved.objects, object)
	if object != nil {
		resolved.objectNodes[object] = index
	}
	resolved.declaringUnit = append(resolved.declaringUnit, -1)
	return index
}

// packageOf is the ID of the package of path in the import closure of the package at packageIndex, and whether the
// graph holds it. A package's own path is its own. A path the graph holds one package of, which is nearly every path,
// is that package, unless the closure holds another of the path: a package and its test variants share a path, and a
// test binary's closure holds one package of each path, so the closure's is the one the record means. It is found by
// walking the closure through the records' import maps, once for each path a package names; a package the graph does
// not hold ends that walk, since its imports are not known.
func (resolved *resolvedGraph) packageOf(packageIndex int, path string) (string, bool) {
	record := resolved.packages[packageIndex]
	if path == record.path {
		return record.identifier, true
	}
	from := pathFrom{packageIndex: packageIndex, path: path}
	if identifier, isResolved := resolved.resolvedPaths[from]; isResolved {
		_, isHeld := resolved.packageIndex[identifier]
		return identifier, isHeld && identifier != ""
	}
	identifier := resolved.inClosure(record, path)
	if identifier == "" && len(resolved.pathsHeld[path]) == 1 {
		identifier = resolved.pathsHeld[path][0]
	}
	resolved.resolvedPaths[from] = identifier
	_, isHeld := resolved.packageIndex[identifier]
	return identifier, isHeld && identifier != ""
}

// inClosure is the ID of the package of path in record's import closure, as far as the records the graph holds tell
// it, and empty when the walk finds none.
func (resolved *resolvedGraph) inClosure(record *packageRecord, path string) string {
	seen := map[string]bool{record.identifier: true}
	pending := []*packageRecord{record}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if identifier, isImported := current.imports[path]; isImported {
			return identifier
		}
		for _, identifier := range current.imports {
			if seen[identifier] {
				continue
			}
			seen[identifier] = true
			if index, isHeld := resolved.packageIndex[identifier]; isHeld {
				pending = append(pending, resolved.packages[index])
			}
		}
	}
	return ""
}

// resolveTargets resolves every object of every record's table once: the node of the object, as the record of its
// package names it, whether it has one, and whether the graph holds its package. A record names each object once, so
// resolution looks each name up once a package rather than once a read.
func (resolved *resolvedGraph) resolveTargets() {
	resolved.targets = make([][]objectTarget, len(resolved.packages))
	for packageIndex, record := range resolved.packages {
		targets := make([]objectTarget, len(record.objects))
		for index, object := range record.objects {
			identifier, isHeld := resolved.packageOf(packageIndex, object.path)
			if !isHeld {
				continue
			}
			node, isNode := resolved.nodes[nodeKey{packageIdentifier: identifier, local: object.local}]
			targets[index] = objectTarget{node: node, isNode: isNode, isHeld: true}
		}
		resolved.targets[packageIndex] = targets
	}
}

// resolveEdges turns every record's kind edges into the bits that depend on each bit. An edge to an object of a
// package the graph holds that no unit declares is dropped, as the counter dropped every edge to an object without a
// node, and one to a package the graph does not hold is kept for the build's DependentObject.
func (resolved *resolvedGraph) resolveEdges() {
	resolved.dependents = make([][]int32, 2*len(resolved.keys))
	for packageIndex, record := range resolved.packages {
		for _, edge := range record.edges {
			from, isNode := resolved.nodes[nodeKey{packageIdentifier: record.identifier, local: edge.from}]
			if !isNode {
				continue
			}
			fromBit := bit(from, edge.fromUnderlying)
			to := resolved.targets[packageIndex][edge.to]
			if to.isNode {
				if toBit := bit(to.node, edge.toUnderlying); toBit != fromBit {
					resolved.dependents[toBit] = append(resolved.dependents[toBit], fromBit)
				}
				continue
			}
			if to.isHeld {
				continue
			}
			resolved.externalEdges = append(resolved.externalEdges, externalEdge{fromBit: fromBit,
				to: externalRead{object: record.objects[edge.to], underlying: edge.toUnderlying}})
		}
	}
}

// resolveRead adds read to unit as bits, or as an external read when its object is of a package the graph does not
// hold. An object of a package the graph holds that no unit declares, a field of an anonymous struct in a var's
// initializer or a method of an interface in a type parameter's constraint, is read through its type, as the counter
// read every object without a node: what its record holds of it (recordedRead.through), each bit of which resolves the
// same way, one with no node in a package the graph holds dropped.
//
// Mutates: unit gains the bits and external reads
func (resolved *resolvedGraph) resolveRead(unit *resolvedUnit, read recordedRead) {
	targets := resolved.targets[unit.packageIndex]
	objects := resolved.packages[unit.packageIndex].objects
	if read.target >= 0 {
		target := targets[read.target]
		if target.isNode {
			unit.bits = append(unit.bits, bit(target.node, read.underlying))
			return
		}
		if !target.isHeld {
			external := externalRead{object: objects[read.target], underlying: read.underlying}
			for _, reached := range read.through {
				external.through = append(external.through,
					externalBit{object: objects[reached.object], underlying: reached.underlying})
			}
			unit.external = append(unit.external, external)
			return
		}
	}
	for _, reached := range read.through {
		target := targets[reached.object]
		switch {
		case target.isNode:
			unit.bits = append(unit.bits, bit(target.node, reached.underlying))
		case !target.isHeld:
			unit.external = append(unit.external,
				externalRead{object: objects[reached.object], underlying: reached.underlying})
		}
	}
}

// fixedPoint spreads the seed bits to every bit that depends on one.
func (resolved *resolvedGraph) fixedPoint(seedBits []int32) []bool {
	set := make([]bool, 2*len(resolved.keys))
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
// keeps each callee of a package the graph does not hold by its name.
func (resolved *resolvedGraph) resolveCalls() {
	for index := range resolved.units {
		unit := &resolved.units[index]
		for _, callee := range unit.record.callees {
			target := resolved.targets[unit.packageIndex][callee]
			if !target.isNode {
				if !target.isHeld {
					named := resolved.packages[unit.packageIndex].objects[callee]
					unit.externalCalls = append(unit.externalCalls, ObjectName{Path: named.path, Local: named.local})
				}
				continue
			}
			declaring := resolved.declaringUnit[target.node]
			if declaring >= 0 && declaring != index && !slices.Contains(unit.calls, declaring) {
				unit.calls = append(unit.calls, declaring)
			}
		}
	}
}

// unitDeclaringName is UnitDeclaringName on the graph's resolution, which every build's judgment shares (CallIndex).
func (resolved *resolvedGraph) unitDeclaringName(path string, local string) (int, bool) {
	identifiers := resolved.pathsHeld[path]
	if len(identifiers) != 1 {
		return 0, false
	}
	node, isNode := resolved.nodes[nodeKey{packageIdentifier: identifiers[0], local: local}]
	if !isNode || resolved.declaringUnit[node] < 0 {
		return 0, false
	}
	return resolved.declaringUnit[node], true
}

// unitContaining is UnitContaining on the graph's resolution, which every build's judgment shares (CallIndex).
func (resolved *resolvedGraph) unitContaining(packageId string, file string, offset int) (int, bool) {
	inFile := resolved.fileUnits[fileKey{packageIdentifier: packageId, file: file}]
	// The last unit starting at or before offset is the only one that can hold it, since a file's units never overlap.
	position := sort.Search(len(inFile), func(index int) bool {
		return resolved.units[inFile[index]].record.start > offset
	}) - 1
	if position < 0 {
		return 0, false
	}
	index := inFile[position]
	if offset >= resolved.units[index].record.end {
		return 0, false
	}
	return index, true
}
