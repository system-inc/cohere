package standing_presence

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// hostPortNamed names, with hostport's reason, each object the package's bodies use that the build finds as a
// package-level variable of the pass's own package and whose own declaration hostport would report as an address: the
// rule judges such a variable by every dial it is passed to, anywhere in the package, so each body that names it must
// be present. A variable of another package is never one, since the rule reads only its own package's declarations.
//
// The predicate mirrors rules/vet/host_port/analyzer.go, which exposes none of it: the package judged at all
// (isJudged, analyzer.go:128-140), the variable's sole declaring value (soleDeclaredValues, analyzer.go:258-266), and
// that value a three-argument fmt.Sprintf with a constant format "%s:%d" or "%s:%s" (reportAddress,
// analyzer.go:183-196, by isFunctionNamed, analyzer.go:273-281). A variable passed to no dial is named all the same:
// the build cannot see the dials a stripped body would hold, so a candidate is judged by its declaration alone.
func hostPortNamed(
	pass *analysis.Pass,
	used []fact_reach.UsedObject,
	find func(identity build_reads.Identity) (types.Object, bool),
) []fact_reach.Named {
	if !judgesHostPort(pass.Pkg) {
		return nil
	}
	// values holds each package-level variable's sole declaring value, read on the first variable found, since most
	// packages' bodies use none.
	var values map[*types.Var]ast.Expr
	var named []fact_reach.Named
	for _, object := range used {
		found, isFound := find(object.Identity)
		variable, isVariable := found.(*types.Var)
		if !isFound || !isVariable || variable.Pkg() != pass.Pkg {
			continue
		}
		if values == nil {
			values = soleDeclaredValues(pass)
		}
		if formatsAddress(pass.TypesInfo, values[variable]) {
			named = append(named, fact_reach.Named{Object: object.Object, Reason: HostPort.Reason()})
		}
	}
	return named
}

// judgesHostPort is whether hostport judges judged at all: a package that imports fmt, and is not net, whose own dial
// functions the original never finds (rules/vet/host_port/analyzer.go:128-140).
func judgesHostPort(judged *types.Package) bool {
	if judged.Path() == "net" {
		return false
	}
	return slices.ContainsFunc(judged.Imports(), func(imported *types.Package) bool {
		return imported.Path() == "fmt"
	})
}

// soleDeclaredValues maps each package-level variable declared by `var name = value` to that value when the
// specification holds just one, whatever the number of names, as the rule reads its value specifications
// (rules/vet/host_port/analyzer.go:258-266). The rule reads `:=` and a function's own specifications too, which
// declare no package-level variable.
func soleDeclaredValues(pass *analysis.Pass) map[*types.Var]ast.Expr {
	declared := map[*types.Var]ast.Expr{}
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			general, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.VAR {
				continue
			}
			for _, specification := range general.Specs {
				value, isValue := specification.(*ast.ValueSpec)
				if !isValue || len(value.Values) != 1 {
					continue
				}
				for _, name := range value.Names {
					if variable, isVariable := pass.TypesInfo.Defs[name].(*types.Var); isVariable {
						declared[variable] = value.Values[0]
					}
				}
			}
		}
	}
	return declared
}

// formatsAddress is whether hostport would report value as an address: a call fmt.Sprintf("%s:%d", host, port) or
// fmt.Sprintf("%s:%s", host, port) with a constant format (rules/vet/host_port/analyzer.go:183-196). No value at all,
// a variable declared without one, is none.
func formatsAddress(information *types.Info, value ast.Expr) bool {
	call, isCall := value.(*ast.CallExpr)
	if !isCall || len(call.Args) != 3 {
		return false
	}
	// The rule's isFunctionNamed (rules/vet/host_port/analyzer.go:273-281), for fmt.Sprintf.
	callee, isFunction := typeutil.Callee(information, call).(*types.Func)
	if !isFunction || callee.Pkg() == nil || callee.Parent() != callee.Pkg().Scope() ||
		callee.Pkg().Path() != "fmt" || callee.Signature().Recv() != nil || callee.Name() != "Sprintf" {
		return false
	}
	// Sprintf's format is a string, so a constant one is a string constant; the kind is read all the same, since
	// StringVal panics on any other.
	format := information.Types[call.Args[0]].Value
	if format == nil || format.Kind() != constant.String {
		return false
	}
	return constant.StringVal(format) == "%s:%d" || constant.StringVal(format) == "%s:%s"
}
