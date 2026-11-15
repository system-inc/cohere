package go_engine

import (
	"go/types"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// byDeclaringUnit is the census's view of the standard library's graph, as one build judged it, for the facts of
// packages named by records (#01chv64): each unit's file and start, by its index in units, read once the first fact
// asks.
type byDeclaringUnit struct {
	standardLibrary *build_dependence.Dependence
	units           []build_dependence.Unit

	// answerPaths are the paths of a restored package's answers of one kind, by the unit declaring each, read once
	// the first answer of the package and kind asks (answerOf).
	answerPaths map[answerKind]map[int][]objectpath.Path
}

// answerKind is one package's answers of one kind.
type answerKind struct {
	packageIdentifier string
	analyzer          string
}

// unitKey is key moved to the unit at index: its file and start in place of its object's position, beside its name,
// empty for a fact the host holds by its object's path alone (nameless).
func (declaring *byDeclaringUnit) unitKey(key factKey, index int) factKey {
	if declaring.units == nil {
		declaring.units = declaring.standardLibrary.Units()
	}
	key.file, key.offset, key.local = declaring.units[index].File, declaring.units[index].Start, ""
	return key
}

// withholdNameless is hosted and built, the host's and the build's facts by unit, with every fact the host holds by
// a path that is no name left out, and how many facts it left out (#01chv64). A package-level object's path is its
// name, which factKeyByName keyed it by; any other path, a method's or a field's, indexes into a set another build's
// files can change, so nothing pairs it with the build's fact about the same object. The census never guesses: such a
// host fact is left out, and so is every build fact of its kind in its unit that no named fact of the host's claims,
// which would otherwise count as differing.
func withholdNameless(
	hosted map[factKey]check.ExportedFact,
	built map[factKey]check.ExportedFact,
) (map[factKey]check.ExportedFact, map[factKey]check.ExportedFact, int) {
	keptHosted := make(map[factKey]check.ExportedFact, len(hosted))
	withheld := map[factKey]bool{}
	left := 0
	for key, fact := range hosted {
		if key.name == "" {
			withheld[key] = true
			left++
			continue
		}
		keptHosted[key] = fact
	}
	keptBuilt := make(map[factKey]check.ExportedFact, len(built))
	for key, fact := range built {
		inUnit := key
		inUnit.name = ""
		if _, isClaimed := keptHosted[key]; !isClaimed && withheld[inUnit] {
			left++
			continue
		}
		keptBuilt[key] = fact
	}
	return keptHosted, keptBuilt, left
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

// answerOf is the host's answer, among answers, the answers it restored of builtFact's package, about the function a
// build's answer, builtFact, is about (#01chv64), whether it holds one, and whether the function is placed at all. The
// host holds each answer by its function's objectpath, which names a method by its index in its type's method set, and
// another build's files can change that set, so the build's own objectpath may name another method. The function is
// found instead by its position, in the unit of the standard library's graph holding it, and the host's answer by the
// unit declaring its path: a function declaration's unit declares one function, so the two meet exactly. A function
// with no position in the graph, and an interface's method, which a type's unit declares beside others, are not
// placed, and the census never guesses at them.
func (declaring *byDeclaringUnit) answerOf(
	answers *fact_resolver.Answers,
	builtFact check.ExportedFact,
) ([]byte, bool, bool) {
	function, isFunction := builtFact.Object.(*types.Func)
	if !isFunction || declaring.standardLibrary == nil {
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
	kind := answerKind{packageIdentifier: builtFact.Package.ID, analyzer: builtFact.Analyzer}
	if declaring.answerPaths == nil {
		declaring.answerPaths = map[answerKind]map[int][]objectpath.Path{}
	}
	byUnit, isRead := declaring.answerPaths[kind]
	if !isRead {
		byUnit = map[int][]objectpath.Path{}
		for path := range answers.Values[builtFact.Analyzer] {
			if index, isDeclared := declaring.standardLibrary.UnitDeclaringName(builtFact.Package.PkgPath,
				string(path)); isDeclared {
				byUnit[index] = append(byUnit[index], path)
			}
		}
		declaring.answerPaths[kind] = byUnit
	}
	switch len(byUnit[unit]) {
	case 0:
		return nil, false, true
	case 1:
		return answers.Values[builtFact.Analyzer][byUnit[unit][0]], true, true
	}
	return nil, false, false
}
