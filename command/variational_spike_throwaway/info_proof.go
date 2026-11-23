package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"strings"

	"github.com/system-inc/go-types/types"
)

// The Info maps the proof compares beside Types, in the order it reports them.
const (
	infoMapDefs       = "Defs"
	infoMapUses       = "Uses"
	infoMapImplicits  = "Implicits"
	infoMapSelections = "Selections"
	infoMapInstances  = "Instances"
)

// infoMapNames are the Info maps in the order the proof reports them.
var infoMapNames = []string{infoMapDefs, infoMapUses, infoMapImplicits, infoMapSelections, infoMapInstances}

// spelledNil is the spelling of no object, a package clause's name's Defs answer.
const spelledNil = "nil"

// infoAnswers are one build's answers of every Info map but Types, each map's keyed by node and spelled.
type infoAnswers map[string]map[expressionKey]string

// newInfoAnswers holds every map, empty.
func newInfoAnswers() infoAnswers {
	answers := infoAnswers{}
	for _, name := range infoMapNames {
		answers[name] = map[expressionKey]string{}
	}
	return answers
}

// spelledObject is an object as the proof compares it: its kind, package, name, type and where it is declared, each
// spelled by its side's speller.
func spelledObject(kind string, packagePath string, name string, spelledType string, place string) string {
	return fmt.Sprintf("%s %s.%s %s at %s", kind, packagePath, name, spelledType, place)
}

// spelledSelection is a selection as the proof compares it: its kind, index, indirection and spelled object.
func spelledSelection(kind int, index []int, indirect bool, spelledObject string) string {
	return fmt.Sprintf("kind %d index %v indirect %t %s", kind, index, indirect, spelledObject)
}

// spelledInstance is an instance as the proof compares it: its type arguments and its type, each spelled.
func spelledInstance(arguments []string, spelledType string) string {
	return "[" + strings.Join(arguments, ", ") + "] " + spelledType
}

// nodeKeyOf is node's key in fileSet, as an expression of Types is keyed: its file, its offsets and its kind.
func nodeKeyOf(fileSet *token.FileSet, node ast.Node) expressionKey {
	tokenFile := fileSet.File(node.Pos())
	if tokenFile == nil {
		return expressionKey{kind: reflect.TypeOf(node).String()}
	}
	return expressionKey{
		file:  tokenFile.Name(),
		start: tokenFile.Offset(node.Pos()),
		end:   tokenFile.Offset(node.End()),
		kind:  reflect.TypeOf(node).String(),
	}
}

// spellInto spells each of answers into into, keyed by its node.
//
// Mutates: into gets an answer for each node.
func spellInto[Key interface {
	comparable
	ast.Node
}, Value any](into map[expressionKey]string, fileSet *token.FileSet, answers map[Key]Value, spell func(Value) string) {
	for node, value := range answers {
		into[nodeKeyOf(fileSet, node)] = spell(value)
	}
}

// overlaid is plain, the host's answers of one map, with a build's overlay laid over it: the build's own answers.
func overlaid[Key comparable, Value any](plain map[Key]Value, overlay types.Overlay[Key, Value]) map[Key]Value {
	projected := make(map[Key]Value, len(plain)+len(overlay.Set))
	for key, value := range plain {
		if !overlay.Removed[key] {
			projected[key] = value
		}
	}
	for key, value := range overlay.Set {
		projected[key] = value
	}
	return projected
}

// projectInfo is checked's Info answers in build, every map but Types: the host's answers with overlayBuild's overlay
// laid over them, which is build's own but when --plant-wrong-overlay lays another's, and each object's type in build.
func projectInfo(
	fileSet *token.FileSet,
	checked []*checkedPackage,
	build int,
	overlayBuild int,
	places declarationPlaces,
) infoAnswers {
	projected := newInfoAnswers()
	spelling := forkInfoSpelling{fileSet: fileSet, places: places, build: types.Mask(1) << build}
	for _, each := range checked {
		overlays := types.BuildOverlays{}
		if overlayBuild > 0 && overlayBuild < len(each.variation.Overlays) {
			overlays = each.variation.Overlays[overlayBuild]
		}
		spellInto(projected[infoMapDefs], fileSet, overlaid(each.info.Defs, overlays.Defs), spelling.object)
		spellInto(projected[infoMapUses], fileSet, overlaid(each.info.Uses, overlays.Uses), spelling.object)
		spellInto(projected[infoMapImplicits], fileSet, overlaid(each.info.Implicits, overlays.Implicits),
			spelling.object)
		spellInto(projected[infoMapSelections], fileSet, overlaid(each.info.Selections, overlays.Selections),
			spelling.selection)
		spellInto(projected[infoMapInstances], fileSet, overlaid(each.info.Instances, overlays.Instances),
			spelling.instance)
	}
	return projected
}

// overlaySizes is, for each map, how many keys build's overlays over checked answer differently from the host's.
func overlaySizes(checked []*checkedPackage, build int) map[string]int {
	sizes := map[string]int{}
	for _, each := range checked {
		if build == 0 || build >= len(each.variation.Overlays) {
			continue
		}
		overlays := each.variation.Overlays[build]
		sizes[infoMapDefs] += overlays.Defs.Len()
		sizes[infoMapUses] += overlays.Uses.Len()
		sizes[infoMapImplicits] += overlays.Implicits.Len()
		sizes[infoMapSelections] += overlays.Selections.Len()
		sizes[infoMapInstances] += overlays.Instances.Len()
	}
	return sizes
}

// proveInfo compares each map of projected with the oracle's plain answers, key for key.
func proveInfo(projected infoAnswers, plain infoAnswers, overlay map[string]int, sources *sourceCache) []mapProof {
	proofs := make([]mapProof, 0, len(infoMapNames))
	for _, name := range infoMapNames {
		proof := mapProof{Map: name, Overlay: overlay[name]}
		var differences []difference
		for key, projectedAnswer := range projected[name] {
			plainAnswer, inOracle := plain[name][key]
			switch {
			case !inOracle:
				proof.OnlyProjected++
				differences = append(
					differences,
					sources.differenceAt(differenceOnlyProjected, key, projectedAnswer, ""),
				)
			case plainAnswer == projectedAnswer:
				proof.Compared++
				proof.Matched++
			default:
				proof.Compared++
				proof.Mismatched++
				differences = append(differences,
					sources.differenceAt(differenceMismatched, key, projectedAnswer, plainAnswer))
			}
		}
		for key, plainAnswer := range plain[name] {
			if _, projectedToo := projected[name][key]; !projectedToo {
				proof.OnlyPlain++
				differences = append(differences, sources.differenceAt(differenceOnlyPlain, key, "", plainAnswer))
			}
		}
		proof.Green = proof.Mismatched == 0 && proof.OnlyProjected == 0 && proof.OnlyPlain == 0
		proof.FirstDifferences = firstDifferences(differences)
		proofs = append(proofs, proof)
	}
	return proofs
}

// mapsGreen says whether every map of a build's proof matched.
func mapsGreen(proofs []mapProof) bool {
	for _, proof := range proofs {
		if !proof.Green {
			return false
		}
	}
	return true
}
