package build_dependence

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
)

// judgment is one build's judgment on a resolved graph: the build's difference, the units seeded so far, the seed
// bits, and the build's answers for objects of packages the graph does not hold, each asked once.
type judgment struct {
	resolved   *resolvedGraph
	difference Difference
	seedUnit   []bool
	seedBits   []int32

	// answers are the build's answers for the resolution's external objects, by their index there, each a few flags
	// (objectAsked and those after it): DependentObject's for a live object, DependentName's for a decoded name.
	answers []uint8

	// objectAnswers are DependentObject's answers for the live objects of packages the graph does not hold that are no
	// external object of it, which ObjectBits meets through an object's type, made at the first.
	objectAnswers map[types.Object]Bits
}

// The flags of an external object's answer (judgment.answers): DependentObject asked, and the name and underlying bits
// it set; DependentName asked, the bits it set, and whether a unit of the answering graph declares the object.
const (
	objectAsked uint8 = 1 << iota
	objectNameSet
	objectUnderlyingSet
	nameAsked
	nameNameSet
	nameUnderlyingSet
	nameDeclared
)

// flagWhen is flag when isSet holds, else none.
func flagWhen(isSet bool, flag uint8) uint8 {
	if isSet {
		return flag
	}
	return 0
}

// seed makes the unit at index a seed, both bits of everything it declares set.
func (judging *judgment) seed(index int) {
	judging.seedUnit[index] = true
	for _, node := range judging.resolved.declares.row(index) {
		judging.seedBits = append(judging.seedBits, bit(node, false), bit(node, true))
	}
}

// seedFiles is rule 1: every unit of a package only the host has, or in a file the build does not compile, is a seed.
func (judging *judgment) seedFiles() {
	hostOnlyFiles := map[string]map[string]bool{}
	for identifier, differing := range judging.difference.Packages {
		if len(differing.HostOnlyFiles) == 0 {
			continue
		}
		files := make(map[string]bool, len(differing.HostOnlyFiles))
		for _, file := range differing.HostOnlyFiles {
			files[file] = true
		}
		hostOnlyFiles[identifier] = files
	}
	resolved := judging.resolved
	for packageIndex, record := range resolved.packages {
		hostOnly := judging.difference.HostOnlyPackages[record.identifier]
		files := hostOnlyFiles[record.identifier]
		if !hostOnly && files == nil {
			continue
		}
		span := resolved.packageUnits[packageIndex]
		for index := span[0]; index < span[1]; index++ {
			if hostOnly || files[resolved.files.names[resolved.unitFile[index]]] {
				judging.seed(int(index))
			}
		}
	}
}

// seedMethodSets is rule 2: a method in a file only the host compiles, or in one only the build compiles, on a type the
// host declares, moves that type's method set, which sets its name bit. A build-only file's receiver is found by its
// base name among the host package's type names, as its record holds them (packageRecord.typeNames), since the file
// was parsed for its syntax alone; a package-level type's local name is its name.
func (judging *judgment) seedMethodSets() {
	for index, receiver := range judging.resolved.unitReceiver {
		if judging.seedUnit[index] && receiver >= 0 {
			judging.seedBits = append(judging.seedBits, bit(receiver, false))
		}
	}
	for identifier, differing := range judging.difference.Packages {
		packageIndex, isHeld := judging.resolved.packageIndex[identifier]
		if !isHeld {
			continue
		}
		record := judging.resolved.packages[packageIndex]
		for _, file := range differing.BuildOnlyFiles {
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction || function.Recv == nil || len(function.Recv.List) == 0 {
					continue
				}
				name := receiverBaseName(function.Recv.List[0].Type)
				if !slices.Contains(record.typeNames, name) {
					continue
				}
				key := nodeKey{packageIndex: int32(packageIndex), local: name}
				if node, isNode := judging.resolved.nodes[key]; isNode {
					judging.seedBits = append(judging.seedBits, bit(node, false))
				}
			}
		}
	}
}

// seedShadowed seeds, beyond the briefed rules, every unit of a package that uses a name of the universe one of the
// package's build-only files declares at package level (min, any, error), since that name means another object there.
// It never fired in the counter, in any build of either tree, and costs a bit test per unit.
func (judging *judgment) seedShadowed() {
	for identifier, differing := range judging.difference.Packages {
		packageIndex, isHeld := judging.resolved.packageIndex[identifier]
		if !isHeld {
			continue
		}
		var shadowed uint64
		for _, file := range differing.BuildOnlyFiles {
			shadowed |= declaredUniverseNames(file)
		}
		if shadowed == 0 {
			continue
		}
		span := judging.resolved.packageUnits[packageIndex]
		for index := span[0]; index < span[1]; index++ {
			if !judging.seedUnit[index] && judging.resolved.unitUniverseNames[index]&shadowed != 0 {
				judging.seed(int(index))
			}
		}
	}
}

// declaredUniverseNames are the bits of the universe's names file declares at package level (PackageLevelNames).
func declaredUniverseNames(file *ast.File) uint64 {
	var declared uint64
	for _, name := range PackageLevelNames(file) {
		declared |= universeNameBit(name)
	}
	return declared
}

// PackageLevelNames are the names file declares at package level: a function without a receiver, a type or a value.
// The body reach reads them of a build-only file too, for every name it shadows, a dot import's as well as the
// universe's (#w72zxtq).
func PackageLevelNames(file *ast.File) []string {
	var names []string
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
				names = append(names, declaration.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					names = append(names, spec.Name.Name)
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						names = append(names, name.Name)
					}
				}
			}
		}
	}
	return names
}

// receiverBaseName is T in `func (t *T[K]) m()`.
func receiverBaseName(expression ast.Expr) string {
	inner := expression
	for {
		switch current := inner.(type) {
		case *ast.StarExpr:
			inner = current.X
		case *ast.ParenExpr:
			inner = current.X
		case *ast.IndexExpr:
			inner = current.X
		case *ast.IndexListExpr:
			inner = current.X
		case *ast.Ident:
			return current.Name
		default:
			return ""
		}
	}
}

// answer is whether the bit of an external object, packed (externalObjectBit), is set in the build, by the build's
// DependentObject, asked once per object; through is what its read reaches through the object's type, the same way.
// An object a decoded record names has no live object, and is answered by its name (answerByName). A build with no
// DependentObject says no such object is dependent.
func (judging *judgment) answer(packed uint32, through []uint32) bool {
	if judging.difference.DependentObject == nil {
		return false
	}
	index := packed >> 1
	object := judging.resolved.externalObjects[index].object
	if object == nil {
		return judging.answerByName(packed, through)
	}
	answered := judging.answers[index]
	if answered&objectAsked == 0 {
		name, underlying := judging.difference.DependentObject(object)
		answered |= objectAsked | flagWhen(name, objectNameSet) | flagWhen(underlying, objectUnderlyingSet)
		judging.answers[index] = answered
	}
	if packed&1 != 0 {
		return answered&objectUnderlyingSet != 0
	}
	return answered&objectNameSet != 0
}

// answerObject is whether object's bit, its underlying bit with underlying set, is set in the build, for a live object
// of a package the graph does not hold, as answer says, asked once per object whether a unit reads it or ObjectBits
// meets it.
func (judging *judgment) answerObject(object types.Object, underlying bool) bool {
	if judging.difference.DependentObject == nil {
		return false
	}
	if index, isExternal := judging.resolved.externalObjectIndex[object]; isExternal {
		return judging.answer(externalObjectBit(index, underlying), nil)
	}
	answered, isAsked := judging.objectAnswers[object]
	if !isAsked {
		answered.Name, answered.Underlying = judging.difference.DependentObject(object)
		if judging.objectAnswers == nil {
			judging.objectAnswers = map[types.Object]Bits{}
		}
		judging.objectAnswers[object] = answered
	}
	if underlying {
		return answered.Underlying
	}
	return answered.Name
}

// answerByName is whether the bit packed of an object a decoded record names is set in the build, by the build's
// DependentName: the object's own bit when a unit of the answering graph declares it, else any bit of what reading it
// reaches through its type, as through holds that, set, as the answering graph reads an object no unit declares
// through its type, both bits at once (Dependence.ObjectBits). A build with no DependentName fails it wide.
func (judging *judgment) answerByName(packed uint32, through []uint32) bool {
	if judging.difference.DependentName == nil {
		return true
	}
	answered := judging.nameAnswer(packed >> 1)
	if answered&nameDeclared != 0 {
		if packed&1 != 0 {
			return answered&nameUnderlyingSet != 0
		}
		return answered&nameNameSet != 0
	}
	if answered&(nameNameSet|nameUnderlyingSet) != 0 {
		return true
	}
	for _, reached := range through {
		reachedAnswer := judging.nameAnswer(reached >> 1)
		if reachedAnswer&nameNameSet != 0 && reached&1 == 0 || reachedAnswer&nameUnderlyingSet != 0 && reached&1 != 0 {
			return true
		}
	}
	return false
}

// nameAnswer is DependentName's answer for the external object at index, asked once per name: a decoded name is one
// external object.
func (judging *judgment) nameAnswer(index uint32) uint8 {
	answered := judging.answers[index]
	if answered&nameAsked != 0 {
		return answered
	}
	object := judging.resolved.externalObjects[index]
	name, underlying, declared := judging.difference.DependentName(object.path, object.local)
	answered |= nameAsked | flagWhen(name, nameNameSet) | flagWhen(underlying, nameUnderlyingSet) |
		flagWhen(declared, nameDeclared)
	judging.answers[index] = answered
	return answered
}

// unitReaches is whether the unit at index, no seed, is dependent: an object it declares has either bit set, or its
// body reads a set bit.
func (judging *judgment) unitReaches(index int, bits []bool) bool {
	for _, node := range judging.resolved.declares.row(index) {
		if bits[bit(node, false)] || bits[bit(node, true)] {
			return true
		}
	}
	for _, read := range judging.resolved.bits.row(index) {
		if bits[read] {
			return true
		}
	}
	external := &judging.resolved.external
	for position := external.offsets[index]; position < external.offsets[index+1]; position++ {
		if judging.answer(external.entries[position], judging.resolved.externalThrough.row(int(position))) {
			return true
		}
	}
	return false
}

// count is the build's counts: units and nodes, dependent and in all, by scope; the packages a package-wide rule
// would have to redo, those with a dependent unit or a unit only the build compiles, and their nodes; and the units
// only the build compiles, which are not the host's, counted apart as work the build alone must check.
func (judging *judgment) count(unitDependent []bool) Counts {
	var counts Counts
	resolved := judging.resolved
	scopeOf := func(workspace bool) *ScopeCounts {
		if workspace {
			return &counts.Workspace
		}
		return &counts.Dependencies
	}
	buildOnlyUnits := make([]int, len(resolved.packages))
	for identifier, differing := range judging.difference.Packages {
		packageIndex, isHeld := resolved.packageIndex[identifier]
		var scope *ScopeCounts
		switch {
		case isHeld:
			scope = scopeOf(resolved.packages[packageIndex].workspace)
		case judging.difference.BuildOnlyPackages[identifier]:
			scope = scopeOf(differing.Workspace)
		default:
			continue
		}
		for _, file := range differing.BuildOnlyFiles {
			units, nodes := fileUnits(file)
			scope.BuildOnlyUnits += units
			scope.BuildOnlyNodes += nodes
			if isHeld {
				buildOnlyUnits[packageIndex] += units
			}
		}
	}
	for packageIndex, record := range resolved.packages {
		scope := scopeOf(record.workspace)
		packageNodes, dependentUnits := 0, 0
		span := resolved.packageUnits[packageIndex]
		for index := span[0]; index < span[1]; index++ {
			nodes := int(resolved.unitNodes[index])
			scope.Units++
			scope.Nodes += nodes
			packageNodes += nodes
			if unitDependent[index] {
				scope.DependentUnits++
				scope.DependentNodes += nodes
				dependentUnits++
			}
		}
		if packageNodes == 0 {
			continue
		}
		scope.Packages++
		if dependentUnits > 0 || buildOnlyUnits[packageIndex] > 0 {
			scope.DependentPackages++
			scope.DependentPackageNodes += packageNodes
		}
	}
	return counts
}

// fileUnits are the units of a file only the build compiles, and their nodes, counted by its syntax.
func fileUnits(file *ast.File) (int, int) {
	units, nodes := 0, 0
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			units++
			nodes += countNodes(declaration)
		case *ast.GenDecl:
			if declaration.Tok == token.IMPORT {
				continue
			}
			for _, spec := range declaration.Specs {
				units++
				nodes += countNodes(spec)
			}
		}
	}
	return units, nodes
}
