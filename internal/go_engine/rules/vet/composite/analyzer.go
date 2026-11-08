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
// both over the same packages. The two agree everywhere, so there is no differences fixture. The port sits on the
// engine's syntax dispatcher, hearing each composite literal on the walk the dispatcher makes once for every syntax
// rule.
package composite

import (
	"fmt"
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "composites"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = `check for unkeyed composite literals

This analyzer reports a diagnostic for composite literals of struct
types imported from another package that do not use the field-keyed
syntax. Such literals are fragile because the addition of a new field
(even if unexported) to the struct will cause compilation to fail.

As an example,

	err = &net.DNSConfigError{err}

should be replaced by:

	err = &net.DNSConfigError{Err: err}
`

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/composite"

// allowListFlag is the option turning the allow list on, under the original's name, since settings may set it.
const allowListFlag = "whitelist"

// Listener is what the port hears of each package from the dispatcher: every composite literal, in preorder, as the
// original's Preorder visits them.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CompositeLit)(nil)},
	Node:  checkLiteral,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, its running
// despite errors, and its whitelist option, which the listener reads from the analyzer running it, so a test can run
// the port with the allow list off, on a dispatcher of its own, beside the registry's.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	// A literal is judged by its own type alone, so a package with errors elsewhere is still judged, as the original
	// judges it.
	analyzer.RunDespiteErrors = true
	// The option keeps the original's usage, word for word, and its default.
	analyzer.Flags.Bool(allowListFlag, true, "use composite white list; for testing only")
	return analyzer
}

// packageState is what the port reads once over a package: its whitelist option.
type packageState struct {
	// allowListRead is whether usesAllowList has been read from the option.
	allowListRead bool
	// usesAllowList is whether the allow list leaves its types' literals alone.
	usesAllowList bool
}

// checkLiteral reports node when it is an unkeyed literal of an imported struct, with the allow list when the
// whitelist option is set.
func checkLiteral(context *rule.Context, node ast.Node, _ []ast.Node) {
	literal, isLiteral := node.(*ast.CompositeLit)
	if !isLiteral {
		return
	}
	state := rule.State[packageState](context)
	if !state.allowListRead {
		state.allowListRead = true
		// newAnalyzer declares the option as a bool flag on every analyzer the port is built as, whose String is
		// strconv.FormatBool of its value, so the option is always there and reads exactly true or false.
		state.usesAllowList = context.Option(allowListFlag).String() == "true"
	}
	// An empty literal names no value to key, and one with any key, partly or fully keyed, is left to the writer.
	if len(literal.Elts) == 0 || slices.ContainsFunc(literal.Elts, isKeyValue) {
		return
	}
	// A literal the checker could not type is left alone.
	literalType := context.TypesInfo.Types[literal].Type
	if literalType == nil {
		return
	}
	// The struct type the literal builds. A type parameter's literal is judged by the first term of its term set, an
	// arbitrary one, as the original chooses it, and a type set that is invalid or empty is left alone.
	terms, termsError := normalTerms(literalType)
	if termsError != nil || len(terms) == 0 {
		return
	}
	chosen := terms[0].Type()
	// An elided `&T` in a literal of `[]*T` is typed `*T`, so a pointer is looked through to its struct.
	structure, isStruct := dereference(chosen).Underlying().(*types.Struct)
	if !isStruct {
		return
	}
	if isSamePackageType(context, chosen) {
		return
	}
	// The allow list is keyed by the literal's type as it prints, so an alias or a pointer of a listed type is not on
	// it, as in the original.
	typeName := literalType.String()
	if state.usesAllowList && unkeyedLiteral[typeName] {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos:            literal.Pos(),
		End:            literal.End(),
		Message:        fmt.Sprintf("%s struct literal uses unkeyed fields", typeName),
		SuggestedFixes: fieldNameFixes(literal, structure),
	})
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

// isSamePackageType is whether candidate belongs to the package context judges: an anonymous struct, which is always
// local, a pointer to such a type, or a named type or type parameter of the package, where a package's external test,
// `foo_test`, counts as `foo`. x/tools' composite isSamePackageType.
func isSamePackageType(context *rule.Context, candidate types.Type) bool {
	switch unaliased := types.Unalias(candidate).(type) {
	case *types.Struct:
		return true
	case *types.Pointer:
		return isSamePackageType(context, unaliased.Elem())
	// *types.Named or *types.TypeParam, since Unalias has already looked through an alias.
	case interface{ Obj() *types.TypeName }:
		return unaliased.Obj().Pkg() != nil &&
			strings.TrimSuffix(unaliased.Obj().Pkg().Path(), "_test") ==
				strings.TrimSuffix(context.Package.Path(), "_test")
	}
	return false
}

// isKeyValue is whether element is keyed, `Field: value`. x/tools' composite is[*ast.KeyValueExpr].
func isKeyValue(element ast.Expr) bool {
	_, isKeyed := element.(*ast.KeyValueExpr)
	return isKeyed
}
