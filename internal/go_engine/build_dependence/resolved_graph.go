package build_dependence

import (
	"go/types"
	"maps"
	"slices"
	"sort"
)

// resolvedGraph is the graph's records resolved into nodes: every object a unit declares has a node, and each node two
// bits, its name bit at twice its index and its underlying bit just after, a non-type object using only the first.
// A node is keyed by its package's ID and its local name (record.go), so a record decoded from its bytes and one
// collected from a live package resolve alike (#6a3kjvz). It is built once for the packages added so far and read,
// never written, by every judgment on it, so judgments can run concurrently.
//
// Everything it holds of a unit, a node or a bit is a column, a slice indexed by the unit, node or bit, and every list
// a unit or a bit holds is a row of packed rows (packedRows), so a tree's resolution is a few dozen slices rather than
// a slice header and an allocation for each list of each unit. It points into no record's units, which it copies what
// it reads of at resolution, so a record's units can be let go once the graph is resolved.
type resolvedGraph struct {
	// packages are the records, in package ID order, so a node's index never hangs on the order packages were added.
	packages     []*packageRecord
	packageIndex map[string]int

	// typesPackages are the live packages the graph holds by their types, for the live object API (ObjectBits), which
	// asks of an object rather than a name.
	typesPackages map[*types.Package]bool

	// nodes are the nodes by key, and objects each node's live object, nil for one only a decoded record declares, so
	// the graph's nodes count len(objects); objectNodes are the live objects' nodes, for the live object API. The map
	// stays after resolution, for NameBits, unitDeclaringName and seedMethodSets: a sorted search in its place would
	// cost a sort of every node at every resolution and a slower lookup, for the bytes of one map.
	nodes       map[nodeKey]int32
	objects     []types.Object
	objectNodes map[types.Object]int32

	// pathPackages are, for each path, the index of the package of it the graph holds, -1 for a path it holds several
	// packages of; resolvedPaths are each path a package's record names, resolved through its import closure
	// (packageOf), each once, to a package's index, -1 for one the graph does not hold. resolvedPaths and targets are
	// read only while resolving, and let go after it.
	pathPackages  map[string]int32
	resolvedPaths map[pathFrom]int32

	// targets are, for each package, each object of its record's table resolved once (resolveTargets), which its
	// reads, callees and edges name by index.
	targets [][]objectTarget

	// The unit columns: each unit's package's index, its file's row in files, its start and end offsets in the file
	// the host parsed and its size in syntax nodes (column), the universe's names it uses, and a method's receiver's
	// node, -1 for any other unit.
	unitPackage       []int32
	unitFile          []int32
	unitStart         []int32
	unitEnd           []int32
	unitNodes         []int32
	unitUniverseNames []uint64
	unitReceiver      []int32

	// declares are each unit's declared objects' nodes, and bits the bits its reads touch.
	declares packedRows[int32]
	bits     packedRows[int32]

	// unitExternal are each unit's reads of objects of packages the graph does not hold, for the build's
	// DependentObject.
	unitExternal [][]externalRead

	// calls are the units each unit's body calls statically (recordedUnit.callees), each once, never itself: a callee
	// no unit of the graph declares is left out, and one of a package the graph does not hold, a dependency's checked
	// without syntax, is kept in unitExternalCalls by its path and local name, a live record's and a decoded one's
	// alike, for another graph to find its unit (#ss4t5g6, #6a3kjvz).
	calls             packedRows[int32]
	unitExternalCalls [][]ObjectName

	// packageUnits are, for each package, the half-open range of its units.
	packageUnits [][2]int32

	// files are every file of the graph's packages, with the units in each.
	files fileTable

	// declaringUnit is, for each node, the unit that declares it.
	declaringUnit []int32

	// dependents are, for each bit, the bits that depend on it (rule 3).
	dependents [][]int32

	// externalEdges are the edges to bits of objects of packages the graph does not hold, set when the build's
	// DependentObject says so.
	externalEdges []externalEdge
}

// nodeKey is a node's key: its package's index and its local name there.
type nodeKey struct {
	packageIndex int32
	local        string
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
	packageIndex int32
	path         string
}

// fileTable is every file of the graph's packages, by its package's ID and the file a reader sees, each a row: its
// name, and its units in the graph's order, which is each file's start order, since a package's units come in the
// order of its files' declarations (collector.collectFile) and a record keeps that order. A file's units are
// contiguous unless a //line directive names two files in one file parsed, a generator's output say, or two files
// parsed name one file; then unitOrder holds every row's units, the graph's units grouped by file. Otherwise it is
// nil, row r's units being the graph's units unitOffsets[r] up to unitOffsets[r+1], and costs nothing.
type fileTable struct {
	rows        map[fileKey]int32
	names       []string
	unitOffsets []uint32
	unitOrder   []int32

	// inStartOrder says each file's units are in the order of their start offsets, as they always are but for two
	// files parsed naming one, which UnitDependent then finds a unit of by reading each of the file's units in turn.
	inStartOrder bool
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

// bit is node's name bit, or with underlying its underlying bit.
func bit(node int32, underlying bool) int32 {
	if underlying {
		return 2*node + 1
	}
	return 2 * node
}

// resolveRecords resolves records, every package the graph holds by its ID, into nodes: each package's units and
// their nodes, then every object of each record's table, the kind edges, the calls and the reads, each a pass over
// every unit in the graph's order, so each pass fills its packed rows by appending.
func resolveRecords(records map[string]*packageRecord) *resolvedGraph {
	resolved := &resolvedGraph{
		packageIndex:  make(map[string]int, len(records)),
		typesPackages: map[*types.Package]bool{},
		pathPackages:  make(map[string]int32, len(records)),
		resolvedPaths: map[pathFrom]int32{},
	}
	for _, identifier := range slices.Sorted(maps.Keys(records)) {
		record := records[identifier]
		packageIndex := len(resolved.packages)
		resolved.packageIndex[identifier] = packageIndex
		resolved.packages = append(resolved.packages, record)
		if record.typesPackage != nil {
			resolved.typesPackages[record.typesPackage] = true
		}
		if _, isHeld := resolved.pathPackages[record.path]; isHeld {
			resolved.pathPackages[record.path] = -1
		} else {
			resolved.pathPackages[record.path] = int32(packageIndex)
		}
	}
	resolved.resolveUnits()
	resolved.resolveTargets()
	resolved.resolveEdges()
	resolved.resolveCalls()
	resolved.resolveReads()
	resolved.resolvedPaths = nil
	resolved.targets = nil
	return resolved
}

// unitCount counts the graph's units.
func (resolved *resolvedGraph) unitCount() int {
	return len(resolved.unitStart)
}

// resolveUnits gives every declared object its node and every unit its place, then each method its receiver's node.
// Every column and the nodes' map are made at their size first, from a count of the records' units and declares, so
// none grows while it fills.
func (resolved *resolvedGraph) resolveUnits() {
	units, declares, liveDeclares := 0, 0, 0
	for _, record := range resolved.packages {
		units += len(record.units)
		for unitIndex := range record.units {
			declares += len(record.units[unitIndex].declares)
			for _, declared := range record.units[unitIndex].declares {
				if declared.object != nil {
					liveDeclares++
				}
			}
		}
	}
	resolved.nodes = make(map[nodeKey]int32, declares)
	resolved.objects = make([]types.Object, 0, declares)
	resolved.objectNodes = make(map[types.Object]int32, liveDeclares)
	resolved.declaringUnit = make([]int32, 0, declares)
	resolved.unitPackage = make([]int32, 0, units)
	resolved.unitFile = make([]int32, 0, units)
	resolved.unitStart = make([]int32, 0, units)
	resolved.unitEnd = make([]int32, 0, units)
	resolved.unitNodes = make([]int32, 0, units)
	resolved.unitUniverseNames = make([]uint64, 0, units)
	resolved.unitReceiver = make([]int32, 0, units)
	resolved.declares = newPackedRows[int32](units, declares)
	resolved.packageUnits = make([][2]int32, 0, len(resolved.packages))
	resolved.files.rows = map[fileKey]int32{}
	for packageIndex, record := range resolved.packages {
		first := int32(resolved.unitCount())
		for unitIndex := range record.units {
			unit := &record.units[unitIndex]
			index := int32(resolved.unitCount())
			for _, declared := range unit.declares {
				node := resolved.node(nodeKey{packageIndex: int32(packageIndex), local: declared.local},
					declared.object)
				resolved.declares.entries = append(resolved.declares.entries, node)
				resolved.declaringUnit[node] = index
			}
			resolved.declares.closeRow()
			resolved.unitPackage = append(resolved.unitPackage, int32(packageIndex))
			resolved.unitFile = append(resolved.unitFile,
				resolved.files.row(fileKey{packageIdentifier: record.identifier, file: unit.file}))
			resolved.unitStart = append(resolved.unitStart, column(unit.start))
			resolved.unitEnd = append(resolved.unitEnd, column(unit.end))
			resolved.unitNodes = append(resolved.unitNodes, column(unit.nodes))
			resolved.unitUniverseNames = append(resolved.unitUniverseNames, unit.universeNames)
			resolved.unitReceiver = append(resolved.unitReceiver, -1)
		}
		resolved.packageUnits = append(resolved.packageUnits, [2]int32{first, int32(resolved.unitCount())})
	}
	resolved.files.placeUnits(resolved.unitFile, resolved.unitStart)
	for packageIndex, record := range resolved.packages {
		for unitIndex := range record.units {
			receiver := record.units[unitIndex].receiver
			if receiver == "" {
				continue
			}
			if node, isNode := resolved.nodes[nodeKey{packageIndex: int32(packageIndex), local: receiver}]; isNode {
				resolved.unitReceiver[resolved.packageUnits[packageIndex][0]+int32(unitIndex)] = node
			}
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
	index := int32(len(resolved.objects))
	resolved.nodes[key] = index
	resolved.objects = append(resolved.objects, object)
	if object != nil {
		resolved.objectNodes[object] = index
	}
	resolved.declaringUnit = append(resolved.declaringUnit, -1)
	return index
}

// row is the row of the file key names, made if it has none.
func (files *fileTable) row(key fileKey) int32 {
	if row, isFile := files.rows[key]; isFile {
		return row
	}
	row := int32(len(files.names))
	files.rows[key] = row
	files.names = append(files.names, key.file)
	return row
}

// placeUnits finds each row's units, from each unit's row and start offset: their range, and their order when a
// file's units are apart (fileTable). Rows are numbered in the order their first units come, so a file's units are
// contiguous, every file's, exactly when no unit's row is below the row of the unit before it.
func (files *fileTable) placeUnits(unitFile []int32, unitStart []int32) {
	rows := len(files.names)
	files.unitOffsets = make([]uint32, rows+1)
	contiguous := true
	for unit, row := range unitFile {
		files.unitOffsets[row+1]++
		if unit > 0 && row < unitFile[unit-1] {
			contiguous = false
		}
	}
	for row := range rows {
		files.unitOffsets[row+1] += files.unitOffsets[row]
	}
	files.inStartOrder = true
	lastStart := make([]int32, rows)
	for row := range lastStart {
		lastStart[row] = -1
	}
	for unit, row := range unitFile {
		if unitStart[unit] < lastStart[row] {
			files.inStartOrder = false
		}
		lastStart[row] = unitStart[unit]
	}
	if contiguous {
		return
	}
	files.unitOrder = make([]int32, len(unitFile))
	next := make([]uint32, rows)
	copy(next, files.unitOffsets[:rows])
	for unit, row := range unitFile {
		files.unitOrder[next[row]] = int32(unit)
		next[row]++
	}
}

// unitAt is the unit at position in the file table's order: the graph's own, unless a file's units are apart.
func (files *fileTable) unitAt(position uint32) int32 {
	if files.unitOrder == nil {
		return int32(position)
	}
	return files.unitOrder[position]
}

// packageOf is the index of the package of path in the import closure of the package at packageIndex, and whether
// the graph holds it. A package's own path is its own. A path the graph holds one package of, which is nearly every
// path, is that package, unless the closure holds another of the path: a package and its test variants share a path,
// and a test binary's closure holds one package of each path, so the closure's is the one the record means. It is
// found by walking the closure through the records' import maps, once for each path a package names; a package the
// graph does not hold ends that walk, since its imports are not known.
func (resolved *resolvedGraph) packageOf(packageIndex int, path string) (int32, bool) {
	record := resolved.packages[packageIndex]
	if path == record.path {
		return int32(packageIndex), true
	}
	from := pathFrom{packageIndex: int32(packageIndex), path: path}
	if held, isResolved := resolved.resolvedPaths[from]; isResolved {
		return held, held >= 0
	}
	held := int32(-1)
	if identifier := resolved.inClosure(record, path); identifier != "" {
		if index, isHeld := resolved.packageIndex[identifier]; isHeld {
			held = int32(index)
		}
	} else if only, isHeld := resolved.pathPackages[path]; isHeld {
		held = only
	}
	resolved.resolvedPaths[from] = held
	return held, held >= 0
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
			held, isHeld := resolved.packageOf(packageIndex, object.path)
			if !isHeld {
				continue
			}
			node, isNode := resolved.nodes[nodeKey{packageIndex: held, local: object.local}]
			targets[index] = objectTarget{node: node, isNode: isNode, isHeld: true}
		}
		resolved.targets[packageIndex] = targets
	}
}

// resolveEdges turns every record's kind edges into the bits that depend on each bit. An edge to an object of a
// package the graph holds that no unit declares is dropped, as the counter dropped every edge to an object without a
// node, and one to a package the graph does not hold is kept for the build's DependentObject.
func (resolved *resolvedGraph) resolveEdges() {
	resolved.dependents = make([][]int32, 2*len(resolved.objects))
	for packageIndex, record := range resolved.packages {
		for _, edge := range record.edges {
			from, isNode := resolved.nodes[nodeKey{packageIndex: int32(packageIndex), local: edge.from}]
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

// resolveReads turns every unit's reads into its bits and its external reads (resolveRead), a row of each a unit.
func (resolved *resolvedGraph) resolveReads() {
	reads := 0
	for _, record := range resolved.packages {
		for unitIndex := range record.units {
			reads += len(record.units[unitIndex].reads)
		}
	}
	resolved.bits = newPackedRows[int32](resolved.unitCount(), reads)
	resolved.unitExternal = make([][]externalRead, resolved.unitCount())
	for packageIndex, record := range resolved.packages {
		for unitIndex := range record.units {
			index := int(resolved.packageUnits[packageIndex][0]) + unitIndex
			for _, read := range record.units[unitIndex].reads {
				resolved.resolveRead(index, packageIndex, read)
			}
			resolved.bits.closeRow()
		}
	}
}

// resolveRead adds read to the unit at index, of the package at packageIndex, as bits of the row being filled, or as
// an external read when its object is of a package the graph does not hold. An object of a package the graph holds
// that no unit declares, a field of an anonymous struct in a var's initializer or a method of an interface in a type
// parameter's constraint, is read through its type, as the counter read every object without a node: what its record
// holds of it (recordedRead.through), each bit of which resolves the same way, one with no node in a package the graph
// holds dropped.
//
// Mutates: the unit gains the bits and external reads
func (resolved *resolvedGraph) resolveRead(index int, packageIndex int, read recordedRead) {
	targets := resolved.targets[packageIndex]
	objects := resolved.packages[packageIndex].objects
	if read.target >= 0 {
		target := targets[read.target]
		if target.isNode {
			resolved.bits.entries = append(resolved.bits.entries, bit(target.node, read.underlying))
			return
		}
		if !target.isHeld {
			external := externalRead{object: objects[read.target], underlying: read.underlying}
			for _, reached := range read.through {
				external.through = append(external.through,
					externalBit{object: objects[reached.object], underlying: reached.underlying})
			}
			resolved.unitExternal[index] = append(resolved.unitExternal[index], external)
			return
		}
	}
	for _, reached := range read.through {
		target := targets[reached.object]
		switch {
		case target.isNode:
			resolved.bits.entries = append(resolved.bits.entries, bit(target.node, reached.underlying))
		case !target.isHeld:
			resolved.unitExternal[index] = append(resolved.unitExternal[index],
				externalRead{object: objects[reached.object], underlying: reached.underlying})
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
// keeps each callee of a package the graph does not hold by its name.
func (resolved *resolvedGraph) resolveCalls() {
	callees := 0
	for _, record := range resolved.packages {
		for unitIndex := range record.units {
			callees += len(record.units[unitIndex].callees)
		}
	}
	resolved.calls = newPackedRows[int32](resolved.unitCount(), callees)
	resolved.unitExternalCalls = make([][]ObjectName, resolved.unitCount())
	for packageIndex, record := range resolved.packages {
		for unitIndex := range record.units {
			index := resolved.packageUnits[packageIndex][0] + int32(unitIndex)
			for _, callee := range record.units[unitIndex].callees {
				target := resolved.targets[packageIndex][callee]
				if !target.isNode {
					if !target.isHeld {
						named := record.objects[callee]
						resolved.unitExternalCalls[index] = append(resolved.unitExternalCalls[index],
							ObjectName{Path: named.path, Local: named.local})
					}
					continue
				}
				declaring := resolved.declaringUnit[target.node]
				if declaring >= 0 && declaring != index && !slices.Contains(resolved.calls.open(), declaring) {
					resolved.calls.entries = append(resolved.calls.entries, declaring)
				}
			}
			resolved.calls.closeRow()
		}
	}
}

// unitDeclaringName is UnitDeclaringName on the graph's resolution, which every build's judgment shares (CallIndex).
func (resolved *resolvedGraph) unitDeclaringName(path string, local string) (int, bool) {
	held, isHeld := resolved.pathPackages[path]
	if !isHeld || held < 0 {
		return 0, false
	}
	node, isNode := resolved.nodes[nodeKey{packageIndex: held, local: local}]
	if !isNode || resolved.declaringUnit[node] < 0 {
		return 0, false
	}
	return int(resolved.declaringUnit[node]), true
}

// fileUnits are the range in the file table's order (fileTable.unitAt) of the units of the package packageId in file,
// the one a reader sees, and false when the graph holds no such file.
func (resolved *resolvedGraph) fileUnits(packageId string, file string) (uint32, uint32, bool) {
	row, isFile := resolved.files.rows[fileKey{packageIdentifier: packageId, file: file}]
	if !isFile {
		return 0, 0, false
	}
	return resolved.files.unitOffsets[row], resolved.files.unitOffsets[row+1], true
}

// lastStartingBy is the position in the file table's order of the last of the units from first up to end that starts
// at or before offset, the units in start order, and false when none does.
func (resolved *resolvedGraph) lastStartingBy(first uint32, end uint32, offset int) (uint32, bool) {
	position := sort.Search(int(end-first), func(index int) bool {
		return int(resolved.unitStart[resolved.files.unitAt(first+uint32(index))]) > offset
	}) - 1
	if position < 0 {
		return 0, false
	}
	return first + uint32(position), true
}

// unitContaining is UnitContaining on the graph's resolution, which every build's judgment shares (CallIndex).
func (resolved *resolvedGraph) unitContaining(packageId string, file string, offset int) (int, bool) {
	first, end, isFile := resolved.fileUnits(packageId, file)
	if !isFile {
		return 0, false
	}
	// The last unit starting at or before offset is the only one that can hold it, since a file's units never overlap.
	position, isFound := resolved.lastStartingBy(first, end, offset)
	if !isFound {
		return 0, false
	}
	index := resolved.files.unitAt(position)
	if offset >= int(resolved.unitEnd[index]) {
		return 0, false
	}
	return int(index), true
}

// unitStartingAt is the unit of the package packageId that starts at offset in file, the one a reader sees, and false
// when none does: of two that start there, the later in the graph's order, the one a map from each unit's place would
// keep. A file's units are in start order, so the last starting at or before offset is the one, unless two files
// parsed name one file (fileTable.inStartOrder), and then each of its units is read, the last to start there kept.
func (resolved *resolvedGraph) unitStartingAt(packageId string, file string, offset int) (int, bool) {
	first, end, isFile := resolved.fileUnits(packageId, file)
	if !isFile {
		return 0, false
	}
	if !resolved.files.inStartOrder {
		for position := end; position > first; position-- {
			if index := resolved.files.unitAt(position - 1); int(resolved.unitStart[index]) == offset {
				return int(index), true
			}
		}
		return 0, false
	}
	position, isFound := resolved.lastStartingBy(first, end, offset)
	if !isFound {
		return 0, false
	}
	index := resolved.files.unitAt(position)
	if int(resolved.unitStart[index]) != offset {
		return 0, false
	}
	return int(index), true
}
