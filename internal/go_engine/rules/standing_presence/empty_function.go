package standing_presence

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// emptyFunctionNamed names, with emptyfunction's reason, each method the package's bodies use under the name of a
// marker interface's one method: the rule leaves an empty method of that name alone only while no selection in the
// package names a method of it whose receiver implements the interface (unusedMarkerInterfaces,
// rules/consistency_no_empty_function/analyzer.go:105-157), so each body that selects one must be present. Any
// receiver, of any package, since a selection the rule reads is judged by whether its receiver implements the
// interface, which the build cannot judge of a body it never holds.
//
// A marker is as the rule reads one: an interface type the package defines, at any level, with exactly one explicit
// method, the method unexported, and nothing embedded (analyzer.go:107-126). The build reads every one its own syntax
// declares, and recorded are the names of those the host's record says a body declares, which the build's syntax
// holds no longer once the body is stripped.
func emptyFunctionNamed(
	pass *analysis.Pass,
	used []fact_reach.UsedObject,
	find func(identity build_reads.Identity) (types.Object, bool),
	recorded []string,
) []fact_reach.Named {
	markers := markerNames(pass)
	for _, name := range recorded {
		markers[name] = true
	}
	if len(markers) == 0 {
		return nil
	}
	return methodsNamed(used, markers, find, EmptyFunction.Reason())
}

// markerNames are the names of the one method of each marker interface the pass's files declare, at any level.
func markerNames(pass *analysis.Pass) map[string]bool {
	names := map[string]bool{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if specification, isTypeSpecification := node.(*ast.TypeSpec); isTypeSpecification {
				if name, isMarker := markerName(pass.TypesInfo, specification); isMarker {
					names[name] = true
				}
			}
			return true
		})
	}
	return names
}

// markerName is the name of the one method of the interface specification declares, when it is a marker.
func markerName(information *types.Info, specification *ast.TypeSpec) (string, bool) {
	typeName, isTypeName := information.Defs[specification.Name].(*types.TypeName)
	if !isTypeName {
		return "", false
	}
	declared, isInterface := typeName.Type().Underlying().(*types.Interface)
	if !isInterface || declared.NumExplicitMethods() != 1 || declared.NumEmbeddeds() != 0 {
		return "", false
	}
	method := declared.ExplicitMethod(0)
	if method.Exported() {
		return "", false
	}
	return method.Name(), true
}

// methodsNamed names, with reason, each used object that is a method of one of names, on any receiver: a method's
// identity says its name (build_reads.IdentityMethod), as the universe's says error's Error. A package-level object,
// an import and a file's ordinal are no method. An object named by any other objectpath, an interface literal's method
// or a field, say, is judged by the build's own object where the build finds one, and named all the same where it
// does not, as is an identity of a kind this cannot read: wider is the safe side.
func methodsNamed(
	used []fact_reach.UsedObject,
	names map[string]bool,
	find func(identity build_reads.Identity) (types.Object, bool),
	reason fact_reach.Reason,
) []fact_reach.Named {
	var named []fact_reach.Named
	for _, object := range used {
		isNamed := true
		switch object.Identity.Kind {
		case build_reads.IdentityMethod:
			isNamed = names[object.Identity.Method]
		case build_reads.IdentityUniverse:
			_, method, isMethod := strings.Cut(string(object.Identity.Path), ".")
			isNamed = isMethod && names[method]
		case build_reads.IdentityImport, build_reads.IdentityFileOrdinal:
			isNamed = false
		case build_reads.IdentityObject:
			if token.IsIdentifier(string(object.Identity.Path)) {
				isNamed = false
				break
			}
			if found, isFound := find(object.Identity); isFound {
				function, isFunction := found.(*types.Func)
				isNamed = isFunction && function.Signature().Recv() != nil && names[function.Name()]
			}
		}
		if isNamed {
			named = append(named, fact_reach.Named{Object: object.Object, Reason: reason})
		}
	}
	return named
}
