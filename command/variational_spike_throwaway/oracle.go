package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"
)

// oracleLoadMode type-checks every package of the graph from source with go/types, as the platform census loads a
// build, so the oracle's answers owe nothing to export data or to the fork.
const oracleLoadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
	packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule

// firstErrorsKept is how many errors a load or a check spells out.
const firstErrorsKept = 10

// expressionKey names an expression across two parses of one file: its file, its byte offsets, and its syntax node's
// kind, since two nodes could in principle span the same bytes.
type expressionKey struct {
	file  string
	start int
	end   int
	kind  string
}

// keyOf is expression's key in fileSet. A constraint's implicit interface, which the checker makes around a `~T` or
// `A|B` constraint and gives no position, is keyed by the constraint it wraps.
func keyOf(fileSet *token.FileSet, expression ast.Expr) expressionKey {
	if wrapped := implicitConstraint(expression); wrapped != nil {
		key := keyOf(fileSet, wrapped)
		key.kind = "implicit interface of " + key.kind
		return key
	}
	tokenFile := fileSet.File(expression.Pos())
	if tokenFile == nil {
		return expressionKey{kind: reflect.TypeOf(expression).String()}
	}
	return expressionKey{
		file:  tokenFile.Name(),
		start: tokenFile.Offset(expression.Pos()),
		end:   tokenFile.Offset(expression.End()),
		kind:  reflect.TypeOf(expression).String(),
	}
}

// implicitConstraint is the constraint expression a positionless implicit interface wraps, or nil for any other
// expression. go/types' Checker.bound wraps the constraint as the interface's one embedded element.
func implicitConstraint(expression ast.Expr) ast.Expr {
	iface, isInterface := expression.(*ast.InterfaceType)
	if !isInterface || iface.Pos().IsValid() || iface.Methods == nil || len(iface.Methods.List) != 1 {
		return nil
	}
	element := iface.Methods.List[0]
	if len(element.Names) != 0 || element.Type == nil || !element.Type.Pos().IsValid() {
		return nil
	}
	return element.Type
}

// keyable reports whether expression has a key of its own: a position, or a constraint it wraps.
func keyable(expression ast.Expr) bool {
	return expression.Pos().IsValid() || implicitConstraint(expression) != nil
}

// oracleBuild is one build's plain answers for our packages' expressions, from go/packages.
type oracleBuild struct {
	answers map[expressionKey]string
	// duplicates counts expressions whose key another expression of the build already held.
	duplicates int
	// noPosition counts expressions with no position, left out of the comparison.
	noPosition    int
	packageErrors int
	firstErrors   []string
	load          phaseTiming
	// info is the build's every other Info map, spelled.
	info infoAnswers
}

// loadOracle loads patterns in root under build with go/packages, every package checked from source, and keeps the
// answers of the expressions of ours, the packages places names, and every other Info map's answers in them, spelled
// as places says of where each object is declared.
func loadOracle(
	root string,
	patterns []string,
	build configuration,
	workFile string,
	places declarationPlaces,
) (*oracleBuild, error) {
	oracle := &oracleBuild{answers: map[expressionKey]string{}, info: newInfoAnswers()}
	buildFlags := []string{"-buildvcs=false"}
	if len(build.tags) != 0 {
		buildFlags = append(buildFlags, "-tags="+strings.Join(build.tags, ","))
	}
	fileSet := token.NewFileSet()
	var roots []*packages.Package
	var loadError error
	oracle.load = measure(func() {
		roots, loadError = packages.Load(&packages.Config{
			Mode:       oracleLoadMode,
			Dir:        root,
			Env:        build.environment(workFile),
			BuildFlags: buildFlags,
			Fset:       fileSet,
		}, patterns...)
	})
	if loadError != nil {
		return nil, fmt.Errorf("oracle load: %w", loadError)
	}
	packages.Visit(roots, nil, func(visited *packages.Package) {
		for _, packageError := range visited.Errors {
			oracle.packageErrors++
			if len(oracle.firstErrors) < firstErrorsKept {
				oracle.firstErrors = append(oracle.firstErrors, visited.ID+": "+packageError.Error())
			}
		}
		if _, isOurs := places.ours[visited.PkgPath]; !isOurs || visited.TypesInfo == nil {
			return
		}
		for expression, typeAndValue := range visited.TypesInfo.Types {
			// A node the checker made for itself with nothing to key it by would share one key with every other:
			// counted apart, never compared. A constraint's implicit interface is keyed by its constraint (keyOf).
			if !keyable(expression) {
				oracle.noPosition++
				continue
			}
			key := keyOf(fileSet, expression)
			if _, held := oracle.answers[key]; held {
				oracle.duplicates++
				continue
			}
			oracle.answers[key] = plainAnswer(typeAndValue)
		}
		spelling := plainInfoSpelling{fileSet: fileSet, places: places}
		spellInto(oracle.info[infoMapDefs], fileSet, visited.TypesInfo.Defs, spelling.object)
		spellInto(oracle.info[infoMapUses], fileSet, visited.TypesInfo.Uses, spelling.object)
		spellInto(oracle.info[infoMapImplicits], fileSet, visited.TypesInfo.Implicits, spelling.object)
		spellInto(oracle.info[infoMapSelections], fileSet, visited.TypesInfo.Selections, spelling.selection)
		spellInto(oracle.info[infoMapInstances], fileSet, visited.TypesInfo.Instances, spelling.instance)
	})
	return oracle, nil
}
