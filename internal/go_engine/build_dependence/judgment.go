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

// receiverResolution is what a build-only method's receiver resolves to (receiverTypeName).
type receiverResolution uint8

// The resolutions of a build-only method's receiver: a type name of the host's package, a type a build-only file
// defines, or nothing the syntax can follow.
const (
	receiverHostType receiverResolution = iota
	receiverDefinedByBuild
	receiverUnresolved
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
// A unit is known by the file a reader sees, past any //line directive, which in a file only the host compiles can name
// a file no build lists, so in a package with a host-only file every unit is a seed unless that file is one both
// builds compile (PackageDifference.SharedFiles): a unit under a directive in a shared file is seeded with them. A
// directive in a file the builds do not share can name that file too, and a build-only file that could not be read or
// parsed declares what no one knows, so a package marked for either (PackageDifference.EveryUnitDependent) has every
// unit seeded, its shared files left unread.
//
// So is every unit that hangs on the C side (unitRecord.fromCgo), one in a cgo tool's output or in any file of a
// package checked as written that has a cgo file, wherever the build's package differs at all: a file only one side
// compiles, or the host's cgo tool or the build's translated the package (PackageDifference.KeepsCgo). What the build
// compiles in its place is its own cgo tool's, under its own tags and flags, a `!cgo` file's, or nothing of the cgo
// tool's at all, whatever the file's name says, and a body or a value the host's load emptied the build checks for the
// first time (cgo_files.go).
func (judging *judgment) seedFiles() {
	sharedFiles := map[string]map[string]bool{}
	for identifier, differing := range judging.difference.Packages {
		switch {
		case differing.EveryUnitDependent:
			// No file is shared for the seeding, so every unit's file reads as not shared.
			sharedFiles[identifier] = nil
		case len(differing.HostOnlyFiles) > 0:
			files := make(map[string]bool, len(differing.SharedFiles))
			for _, file := range differing.SharedFiles {
				files[file] = true
			}
			sharedFiles[identifier] = files
		}
	}
	resolved := judging.resolved
	for packageIndex, record := range resolved.packages {
		hostOnly := judging.difference.HostOnlyPackages[record.identifier]
		files, seedsByFile := sharedFiles[record.identifier]
		differing := judging.difference.Packages[record.identifier]
		seedsCgo := len(differing.HostOnlyFiles) > 0 || len(differing.BuildOnlyFiles) > 0 || differing.KeepsCgo
		if !hostOnly && !seedsByFile && !seedsCgo {
			continue
		}
		span := resolved.packageUnits[packageIndex]
		for index := span[0]; index < span[1]; index++ {
			// A package seeded for its cgo alone has no shared files to read: only its marked units are seeds.
			if hostOnly || seedsByFile && !files[resolved.files.names[resolved.unitFile[index]]] ||
				seedsCgo && resolved.unitFromCgo[index] {
				judging.seed(int(index))
			}
		}
	}
}

// seedWithheld is rule 1's other half: every unit the host's load withheld from, its body or its initial value given
// to the analyzers without what it held (unitRecord.withheld), is a seed in every build, whatever the build's
// difference says. The host never checked what such a unit held, and a build may check it for the first time with no
// file of the unit's package differing and no edge of the unit's to say so: the load withholds wherever an expression
// is invalid, one typed from another faked package among them (load's holdsInvalid), so a build that types that
// package's declaration another way, under a tag that moves it out of a cgo file, types the unit another way too.
func (judging *judgment) seedWithheld() {
	for index, withheld := range judging.resolved.unitWithheld {
		if withheld && !judging.seedUnit[index] {
			judging.seed(index)
		}
	}
}

// seedMethodSets is rule 2: a method in a file only the host compiles, or in one only the build compiles, on a type the
// host declares, moves that type's method set, which sets its name bit. A build-only file's receiver is resolved by its
// base name, since the file was parsed for its syntax alone (receiverTypeName): an alias a build-only file declares is
// followed to the name it stands for, a type a build-only file defines moves no method set the host has, and the rest
// is found among the host package's type names, as its record holds them (packageRecord.typeNames), a package-level
// type's local name being its name. The name found may be a host alias's, whose method set is its target's, so the
// target's name bit is seeded too (seedAliasTargets). A receiver that resolves to none of these seeds the name bit of
// every type name of the package, failing wide.
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
		var buildOnlyTypes map[string]*ast.TypeSpec
		var receivers map[int32]bool
		for _, file := range differing.BuildOnlyFiles {
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction || function.Recv == nil || len(function.Recv.List) == 0 {
					continue
				}
				if buildOnlyTypes == nil {
					buildOnlyTypes = typeSpecs(differing.BuildOnlyFiles)
				}
				name, resolution := receiverTypeName(receiverBaseName(function.Recv.List[0].Type), buildOnlyTypes,
					record.typeNames)
				switch resolution {
				case receiverDefinedByBuild:
					continue
				case receiverUnresolved:
					judging.seedTypeNames(packageIndex)
					continue
				}
				key := nodeKey{packageIndex: int32(packageIndex), local: name}
				if node, isNode := judging.resolved.nodes[key]; isNode {
					judging.seedBits = append(judging.seedBits, bit(node, false))
					if receivers == nil {
						receivers = judging.packageReceivers(packageIndex)
					}
					judging.seedAliasTargets(packageIndex, node, receivers)
				}
			}
		}
	}
}

// packageReceivers are the nodes of the package at packageIndex that a unit of it has for its receiver, each a defined
// type, since a method's receiver is found through its named type and never through an alias (collectFunction).
func (judging *judgment) packageReceivers(packageIndex int) map[int32]bool {
	receivers := map[int32]bool{}
	span := judging.resolved.packageUnits[packageIndex]
	for index := span[0]; index < span[1]; index++ {
		if receiver := judging.resolved.unitReceiver[index]; receiver >= 0 {
			receivers[receiver] = true
		}
	}
	return receivers
}

// seedAliasTargets seeds the name bit of what a build-only method's receiver, at node, may alias: `type A = T` makes a
// method declared on A one of T's, which moves T's method set, and A's bits depend on T's, never T's on A's. A record
// keeps no mark of an alias, only its kind edges, which resolution turns into the bits that depend on each bit
// (resolvedGraph.dependents), and a live record and a decoded one must judge alike, so the alias is not read from the
// live types. Each type of the package whose name bit the receiver's name bit depends on directly is seeded, and so on
// through each of those in turn, for a chain of aliases: an alias's target, and for a defined type the types it embeds
// or constrains its parameters by, a larger set, which a doubt takes. A type some unit of the host has for its receiver
// is defined, no alias (packageReceivers), so nothing is seeded through it. A receiver must be of the method's own
// package, so no other package's type is looked at.
func (judging *judgment) seedAliasTargets(packageIndex int, node int32, receivers map[int32]bool) {
	seen := map[int32]bool{node: true}
	pending := []int32{node}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if receivers[current] {
			continue
		}
		for _, name := range judging.resolved.packages[packageIndex].typeNames {
			target, isNode := judging.resolved.nodes[nodeKey{packageIndex: int32(packageIndex), local: name}]
			if !isNode || seen[target] ||
				!slices.Contains(judging.resolved.dependents.row(int(bit(target, false))), bit(current, false)) {
				continue
			}
			seen[target] = true
			judging.seedBits = append(judging.seedBits, bit(target, false))
			pending = append(pending, target)
		}
	}
}

// receiverTypeName resolves name, a build-only method's receiver's base name, by syntax alone: a name a build-only file
// declares is the build's, an alias followed through its right-hand side's base name and a defined type the build's
// own, and any other is a type name of the host's package when typeNames holds it. A build-only file declares nothing
// a file both builds compile does, so a name both declare is a host-only file's, which the build does not compile. An
// alias of a name nothing declares, of another package's type, or of one no base name names, and a chain of aliases
// that comes back on itself, resolve to nothing, as a receiver the type checker refuses does.
func receiverTypeName(
	name string,
	buildOnlyTypes map[string]*ast.TypeSpec,
	typeNames []string,
) (string, receiverResolution) {
	followed := map[string]bool{}
	current := name
	for current != "" && !followed[current] {
		followed[current] = true
		spec, isBuildOnly := buildOnlyTypes[current]
		switch {
		case !isBuildOnly && slices.Contains(typeNames, current):
			return current, receiverHostType
		case !isBuildOnly:
			return "", receiverUnresolved
		case !spec.Assign.IsValid():
			return "", receiverDefinedByBuild
		}
		current = receiverBaseName(spec.Type)
	}
	return "", receiverUnresolved
}

// typeSpecs are the type specs files declare at package level, by name.
func typeSpecs(files []*ast.File) map[string]*ast.TypeSpec {
	specs := map[string]*ast.TypeSpec{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			general, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				if typeSpec, isTypeSpec := spec.(*ast.TypeSpec); isTypeSpec {
					specs[typeSpec.Name.Name] = typeSpec
				}
			}
		}
	}
	return specs
}

// seedTypeNames seeds the name bit of every type name of the package at packageIndex, as its record holds them
// (packageRecord.typeNames), for a build-only method whose receiver resolves to none of them: any of their method sets
// may be the one that moves.
func (judging *judgment) seedTypeNames(packageIndex int) {
	for _, name := range judging.resolved.packages[packageIndex].typeNames {
		if node, isNode := judging.resolved.nodes[nodeKey{packageIndex: int32(packageIndex), local: name}]; isNode {
			judging.seedBits = append(judging.seedBits, bit(node, false))
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

// declaredUniverseNames are the bits of the universe's names file declares at package level: a function without a
// receiver, a type or a value.
func declaredUniverseNames(file *ast.File) uint64 {
	var declared uint64
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
				declared |= universeNameBit(declaration.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					declared |= universeNameBit(spec.Name.Name)
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						declared |= universeNameBit(name.Name)
					}
				}
			}
		}
	}
	return declared
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
