package go_engine

import (
	"go/types"
	"strings"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// byDeclaringUnit is the census's view of the standard library's graph, as one build judged it, for the facts of
// packages named by records (#01chv64): each unit's file and start, by its index in units, read once the first fact
// asks.
type byDeclaringUnit struct {
	standardLibrary *build_dependence.Dependence
	units           []build_dependence.Unit

	// answerPaths are the paths of a restored package's answers of one kind, by where each sits (answerPlace), read
	// once the first answer of the package and kind asks (answerOf), and paths the build's objectpaths, which say a
	// parameter's or a result's place in its function's signature.
	answerPaths map[answerKind]map[answerPlace][]objectpath.Path
	paths       objectpath.Encoder
}

// answerKind is one package's answers of one kind.
type answerKind struct {
	packageIdentifier string
	analyzer          string
}

// unitKey is key moved to the unit at index: its file and start in place of its object's position, beside its name,
// empty for a fact the host holds by its object's path alone, and marked as a unit's key.
func (declaring *byDeclaringUnit) unitKey(key factKey, index int) factKey {
	if declaring.units == nil {
		declaring.units = declaring.standardLibrary.Units()
	}
	key.file, key.offset, key.local, key.byUnit = declaring.units[index].File, declaring.units[index].Start, "", true
	return key
}

// withheldReason is why the census leaves out, in one unit, the build's facts of one kind that no named fact of the
// host's claims (pairByUnit).
type withheldReason int

const (
	// notWithheld leaves the build's facts to be compared.
	notWithheld withheldReason = iota
	// withheldPathOnly is a host fact of the kind in the unit held by a member's path alone.
	withheldPathOnly
	// withheldCollision is two host facts of the kind meeting in the unit under one name.
	withheldCollision
)

// pairByUnit is the host's facts by name, hostByName, and the build's by position, builtByPosition, each moved to its
// declaring unit by hosted and built, as the census compares them, and the facts it left out, by why (#h660y7d). The
// census never guesses, and the two sides fail alike: a fact either side cannot place is left out, and so are the
// build's facts that would otherwise meet a host fact left out unpaired and count as differing: of the same package,
// kind and name as a host fact no unit declares, or of its kind in the package where that fact has no name; and of the
// kind in a unit where the host's fact is held by a member's path alone, a method's or a field's, which indexes into a
// set another build's files can change, or where two host facts of the kind meet under one name. Two build facts that
// meet under one key are left out with the host's fact there. A package-level object's path is its name, which
// factKeyByName keyed it by, so its fact is paired exactly.
func pairByUnit(
	hostByName map[factKey]check.ExportedFact,
	builtByPosition map[factKey]check.ExportedFact,
	hosted func(factKey) (factKey, bool),
	built func(factKey) (factKey, bool),
) (map[factKey]check.ExportedFact, map[factKey]check.ExportedFact, contract.UnkeyedFacts) {
	var parts contract.UnkeyedFacts
	hostGroups := map[factKey][]check.ExportedFact{}
	unplaced := map[factKey]bool{}
	for key, fact := range hostByName {
		unitKey, isPlaced := hosted(key)
		if !isPlaced {
			parts.Unplaced++
			unplaced[factKey{packageIdentifier: key.packageIdentifier, name: key.name, analyzer: key.analyzer,
				factType: key.factType}] = true
			continue
		}
		hostGroups[unitKey] = append(hostGroups[unitKey], fact)
	}
	hostFacts := make(map[factKey]check.ExportedFact, len(hostGroups))
	withheld := map[factKey]withheldReason{}
	withhold := func(unitKey factKey, reason withheldReason) {
		inUnit := unitKey
		inUnit.name = ""
		if withheld[inUnit] == notWithheld {
			withheld[inUnit] = reason
		}
	}
	for unitKey, group := range hostGroups {
		switch {
		case len(group) > 1:
			parts.Collisions += len(group)
			withhold(unitKey, withheldCollision)
		case unitKey.name == "":
			parts.PathOnly++
			withhold(unitKey, withheldPathOnly)
		default:
			hostFacts[unitKey] = group[0]
		}
	}
	builtGroups := map[factKey][]check.ExportedFact{}
	for key, fact := range builtByPosition {
		unitKey, isPlaced := built(key)
		if !isPlaced {
			parts.Unplaced++
			continue
		}
		builtGroups[unitKey] = append(builtGroups[unitKey], fact)
	}
	builtFacts := make(map[factKey]check.ExportedFact, len(builtGroups))
	for unitKey, group := range builtGroups {
		_, isClaimed := hostFacts[unitKey]
		inUnit := unitKey
		inUnit.name = ""
		inPackage := factKey{packageIdentifier: unitKey.packageIdentifier, analyzer: unitKey.analyzer,
			factType: unitKey.factType}
		named := inPackage
		named.name = unitKey.name
		switch {
		case len(group) > 1:
			parts.Collisions += len(group)
			if isClaimed {
				parts.Collisions++
				delete(hostFacts, unitKey)
			}
		case isClaimed:
			builtFacts[unitKey] = group[0]
		case withheld[inUnit] == withheldCollision:
			parts.Collisions++
		case withheld[inUnit] == withheldPathOnly:
			parts.PathOnly++
		case unplaced[named] || unplaced[inPackage]:
			parts.Unplaced++
		default:
			builtFacts[unitKey] = group[0]
		}
	}
	return hostFacts, builtFacts, parts
}

// unkeyedTotal is the facts parts left out in all, the census's Unkeyed.
func unkeyedTotal(parts contract.UnkeyedFacts) int {
	return parts.Unnamed + parts.Closures + parts.Unplaced + parts.PathOnly + parts.Collisions + parts.Answers
}

// hosted is a host's key by name (factKeyByName) moved to the unit declaring its object, and false when no unit
// of the graph declares it.
func (declaring *byDeclaringUnit) hosted(key factKey) (factKey, bool) {
	if declaring.standardLibrary == nil {
		return factKey{}, false
	}
	index, isDeclared := declaring.standardLibrary.UnitDeclaringName(key.packageIdentifier, key.local)
	if !isDeclared {
		return factKey{}, false
	}
	return declaring.unitKey(key, index), true
}

// built is a build's fact's key, by its object's position, moved to the unit holding that position, and false when no
// unit of the graph holds it.
func (declaring *byDeclaringUnit) built(key factKey) (factKey, bool) {
	if declaring.standardLibrary == nil {
		return factKey{}, false
	}
	index, isUnit := declaring.standardLibrary.UnitContaining(key.packageIdentifier, key.file, key.offset)
	if !isUnit {
		return factKey{}, false
	}
	return declaring.unitKey(key, index), true
}

// answerOf is the host's answer, among answers, the answers it restored of builtFact's package, about the function, or
// the parameter or result of one, a build's answer, builtFact, is about (#h660y7d), whether it holds one, and whether
// the object is placed at all. The host holds each answer by its object's objectpath, which names a method by its
// index in its type's method set, and another build's files can change that set, so the build's own objectpath may
// name another method; and two functions of one name in files of different builds share a path, though they are other
// code. The object is found instead by its position, in the unit of the standard library's graph holding it, and the
// host's answer by the unit declaring the function its path names, beside the object's place in that function's
// signature (answerPlaceOf): a function declaration's unit declares one function, so the two meet exactly. An object
// with no position in the graph, an interface's method or its parameter, which a type's unit declares beside others,
// and an object two of the host's answers meet at, are not placed, and the census never guesses at them.
func (declaring *byDeclaringUnit) answerOf(
	answers *fact_resolver.Answers,
	builtFact check.ExportedFact,
) ([]byte, bool, bool) {
	if declaring.standardLibrary == nil {
		return nil, false, false
	}
	function, place, isPlaced := declaring.builtFunction(builtFact)
	if !isPlaced {
		return nil, false, false
	}
	if receiver := function.Signature().Recv(); receiver != nil && types.IsInterface(receiver.Type()) {
		return nil, false, false
	}
	key, isKeyed := factKeyOf(builtFact)
	if !isKeyed {
		return nil, false, false
	}
	unit, isUnit := declaring.standardLibrary.UnitContaining(key.packageIdentifier, key.file, key.offset)
	if !isUnit {
		return nil, false, false
	}
	// A parameter or result is placed only within its own function's declaration.
	if place != "" {
		functionKey, isFunctionKeyed := factKeyAt(builtFact, function, builtFact.Package.Fset)
		functionUnit, isFunctionUnit := declaring.standardLibrary.UnitContaining(key.packageIdentifier,
			functionKey.file, functionKey.offset)
		if !isFunctionKeyed || !isFunctionUnit || functionUnit != unit {
			return nil, false, false
		}
	}
	kind := answerKind{packageIdentifier: builtFact.Package.ID, analyzer: builtFact.Analyzer}
	if declaring.answerPaths == nil {
		declaring.answerPaths = map[answerKind]map[answerPlace][]objectpath.Path{}
	}
	byPlace, isRead := declaring.answerPaths[kind]
	if !isRead {
		byPlace = map[answerPlace][]objectpath.Path{}
		for path := range answers.Values[builtFact.Analyzer] {
			if hostPlace, isHostPlaced := declaring.hostedAnswer(builtFact.Package.PkgPath, path); isHostPlaced {
				byPlace[hostPlace] = append(byPlace[hostPlace], path)
			}
		}
		declaring.answerPaths[kind] = byPlace
	}
	paths := byPlace[answerPlace{unit: unit, place: place}]
	switch len(paths) {
	case 0:
		return nil, false, true
	case 1:
		return answers.Values[builtFact.Analyzer][paths[0]], true, true
	}
	return nil, false, false
}

// builtFunction is the function a build's answer, builtFact, is about, and its object's place in that function's
// signature, empty for the function itself (answerPlaceOf), and false for an object that is neither a function nor a
// parameter or result of a function its package names.
func (declaring *byDeclaringUnit) builtFunction(builtFact check.ExportedFact) (*types.Func, string, bool) {
	switch object := builtFact.Object.(type) {
	case *types.Func:
		return object, "", true
	case *types.Var:
		if object.IsField() || builtFact.Package.Types == nil {
			return nil, "", false
		}
		path, pathError := declaring.paths.For(object)
		if pathError != nil {
			return nil, "", false
		}
		functionPath, place, isPlace := answerPlaceOf(path)
		if !isPlace {
			return nil, "", false
		}
		named, findError := objectpath.Object(builtFact.Package.Types, functionPath)
		function, isFunction := named.(*types.Func)
		if findError != nil || !isFunction {
			return nil, "", false
		}
		return function, place, true
	}
	return nil, "", false
}

// hostedAnswer is the place of the host's answer at path, of the package of packagePath: the unit declaring the object
// it names, a function's, with no place, or the unit declaring the function whose parameter or result it names, beside
// that place (answerPlaceOf), and false when no unit declares either.
func (declaring *byDeclaringUnit) hostedAnswer(packagePath string, path objectpath.Path) (answerPlace, bool) {
	if index, isDeclared := declaring.standardLibrary.UnitDeclaringName(packagePath, string(path)); isDeclared {
		return answerPlace{unit: index}, true
	}
	functionPath, place, isPlace := answerPlaceOf(path)
	if !isPlace {
		return answerPlace{}, false
	}
	index, isDeclared := declaring.standardLibrary.UnitDeclaringName(packagePath, string(functionPath))
	if !isDeclared {
		return answerPlace{}, false
	}
	return answerPlace{unit: index, place: place}, true
}

// answerPlace is where a host's answer sits: the unit declaring its function, and its object's place in that
// function's signature, empty for the function itself.
type answerPlace struct {
	unit  int
	place string
}

// answerPlaceOf splits path, a parameter's or a result's, into its function's path and its place in the function's
// signature, as objectpath's grammar writes it: the function's path, then `.`, its Type operator, then `P` or `R`, the
// signature's Params or Results, then `A` and the tuple's index. A `.` appears in a path only as the Type operator,
// since no identifier holds one, so the last one opens the final step; any other final step, a field's or one through
// another type, is no place, and false.
func answerPlaceOf(path objectpath.Path) (objectpath.Path, string, bool) {
	dot := strings.LastIndexByte(string(path), '.')
	if dot < 0 {
		return "", "", false
	}
	step := string(path[dot+1:])
	if len(step) < 3 || (step[0] != 'P' && step[0] != 'R') || step[1] != 'A' ||
		strings.Trim(step[2:], "0123456789") != "" {
		return "", "", false
	}
	return path[:dot], step, true
}
