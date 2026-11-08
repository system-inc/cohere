// Package composite is vet's composites pass, a composite literal of a struct type imported from another package
// that lists its values without naming their fields. Such a literal breaks the day the struct gains a field, even an
// unexported one, so the pass asks for the field-keyed form. A struct of the package's own, or of the package its
// external test imports, is the writer's to change in step, and is left alone, as is a literal of a standard-library
// type on the allow list (unkeyed_literal.go), unless the whitelist option is set false. Where the literal has one
// value per field and every field is exported, the finding offers to fill in the field names.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/composite/composite.go and
// whitelist.go, whose logic it keeps and whose messages and suggested fixes it reports word for word, at the same
// position. A literal of a type parameter is judged by the first term of the parameter's normalized term set, as the
// original does through x/tools' internal typeparams, which the port cannot import, so its NormalTerms, the term set
// computation behind it, and the core type behind its Deref are translated here (normal_terms.go, term.go,
// term_list.go), keeping the order of their terms, since which term comes first decides the judgment. The registry
// runs the port in the original's place, and the original stays the oracle: the parity tests beside this file run
// both over the same packages. The two agree everywhere, so there is no differences fixture.
package composite

import (
	"fmt"
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = newAnalyzer()

// newAnalyzer builds the port with a whitelist option of its own, which its run reads, so a test can run the port
// with the allow list off beside the registry's, without setting a flag the registry's reads.
func newAnalyzer() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name: "composites",
		Doc: `check for unkeyed composite literals

This analyzer reports a diagnostic for composite literals of struct
types imported from another package that do not use the field-keyed
syntax. Such literals are fragile because the addition of a new field
(even if unexported) to the struct will cause compilation to fail.

As an example,

	err = &net.DNSConfigError{err}

should be replaced by:

	err = &net.DNSConfigError{Err: err}
`,
		URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/composite",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		// A literal is judged by its own type alone, so a package with errors elsewhere is still judged, as the
		// original judges it.
		RunDespiteErrors: true,
	}
	// The option keeps the original's name, whitelist, and its usage, word for word, since settings may set it.
	usesAllowList := true
	analyzer.Flags.BoolVar(&usesAllowList, "whitelist", usesAllowList, "use composite white list; for testing only")
	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		run(pass, usesAllowList)
		return nil, nil
	}
	return analyzer
}

// run reports every unkeyed literal of an imported struct in the package pass judges, with the allow list when
// usesAllowList is set. It has no result to give and meets no error, so it returns nothing.
func run(pass *analysis.Pass, usesAllowList bool) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("composite: inspect's result is missing")
	}
	// All visits each composite literal in preorder, as the original's Preorder does, and hands it over typed.
	for literal := range inspector.All[*ast.CompositeLit](inspected) {
		// An empty literal names no value to key, and one with any key, partly or fully keyed, is left to the writer.
		if len(literal.Elts) == 0 || slices.ContainsFunc(literal.Elts, isKeyValue) {
			continue
		}
		// A literal the checker could not type is left alone.
		literalType := pass.TypesInfo.Types[literal].Type
		if literalType == nil {
			continue
		}
		// The struct type the literal builds. A type parameter's literal is judged by the first term of its term
		// set, an arbitrary one, as the original chooses it, and a type set that is invalid or empty is left alone.
		terms, termsError := normalTerms(literalType)
		if termsError != nil || len(terms) == 0 {
			continue
		}
		chosen := terms[0].Type()
		// An elided `&T` in a literal of `[]*T` is typed `*T`, so a pointer is looked through to its struct.
		structure, isStruct := dereference(chosen).Underlying().(*types.Struct)
		if !isStruct {
			continue
		}
		if isSamePackageType(pass, chosen) {
			continue
		}
		// The allow list is keyed by the literal's type as it prints, so an alias or a pointer of a listed type is
		// not on it, as in the original.
		typeName := literalType.String()
		if usesAllowList && unkeyedLiteral[typeName] {
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:            literal.Pos(),
			End:            literal.End(),
			Message:        fmt.Sprintf("%s struct literal uses unkeyed fields", typeName),
			SuggestedFixes: fieldNameFixes(literal, structure),
		})
	}
}

// fieldNameFixes is the original's one fix, which keys each value of literal with its field's name, offered only when
// literal has one value per field of structure and every field is exported, since an unexported field of another
// package cannot be named. It is nil when no fix is offered.
func fieldNameFixes(literal *ast.CompositeLit, structure *types.Struct) []analysis.SuggestedFix {
	if len(literal.Elts) != structure.NumFields() {
		return nil
	}
	edits := []analysis.TextEdit{}
	for index, element := range literal.Elts {
		field := structure.Field(index)
		if !field.Exported() {
			return nil
		}
		edits = append(edits, analysis.TextEdit{
			Pos:     element.Pos(),
			End:     element.Pos(),
			NewText: fmt.Appendf(nil, "%s: ", field.Name()),
		})
	}
	return []analysis.SuggestedFix{{
		Message:   "Add field names to struct literal",
		TextEdits: edits,
	}}
}

// isSamePackageType is whether candidate belongs to the package pass judges: an anonymous struct, which is always
// local, a pointer to such a type, or a named type or type parameter of the package, where a package's external test,
// `foo_test`, counts as `foo`. x/tools' composite isSamePackageType.
func isSamePackageType(pass *analysis.Pass, candidate types.Type) bool {
	switch unaliased := types.Unalias(candidate).(type) {
	case *types.Struct:
		return true
	case *types.Pointer:
		return isSamePackageType(pass, unaliased.Elem())
	// *types.Named or *types.TypeParam, since Unalias has already looked through an alias.
	case interface{ Obj() *types.TypeName }:
		return unaliased.Obj().Pkg() != nil &&
			strings.TrimSuffix(unaliased.Obj().Pkg().Path(), "_test") ==
				strings.TrimSuffix(pass.Pkg.Path(), "_test")
	}
	return false
}

// isKeyValue is whether element is keyed, `Field: value`. x/tools' composite is[*ast.KeyValueExpr].
func isKeyValue(element ast.Expr) bool {
	_, isKeyed := element.(*ast.KeyValueExpr)
	return isKeyed
}
