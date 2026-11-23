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

// keyOf is expression's key in fileSet.
func keyOf(fileSet *token.FileSet, expression ast.Expr) expressionKey {
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

// oracleBuild is one build's plain answers for our packages' expressions, from go/packages.
type oracleBuild struct {
	answers map[expressionKey]string
	// duplicates counts expressions whose key another expression of the build already held.
	duplicates    int
	packageErrors int
	firstErrors   []string
	load          phaseTiming
}

// loadOracle loads patterns in root under build with go/packages, every package checked from source, and keeps the
// answers of the expressions of ours.
func loadOracle(
	root string,
	patterns []string,
	build configuration,
	workFile string,
	ours map[string]*ourPackage,
) (*oracleBuild, error) {
	oracle := &oracleBuild{answers: map[expressionKey]string{}}
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
		if _, isOurs := ours[visited.PkgPath]; !isOurs || visited.TypesInfo == nil {
			return
		}
		for expression, typeAndValue := range visited.TypesInfo.Types {
			key := keyOf(fileSet, expression)
			if _, held := oracle.answers[key]; held {
				oracle.duplicates++
				continue
			}
			oracle.answers[key] = plainAnswer(typeAndValue)
		}
	})
	return oracle, nil
}
