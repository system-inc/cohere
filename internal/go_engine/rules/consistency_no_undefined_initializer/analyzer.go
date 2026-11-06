// Package consistency_no_undefined_initializer is the Go engine's rule that a typed var does not restate its zero
// value.
//
// It is the Go form of ESLint core's no-undef-init. Go gives every variable its type's zero value, so `var count int`
// already holds 0, and `var count int = 0` spells out what the language guarantees. The initializer adds nothing, and
// it costs the reader a moment deciding whether the author meant something by it. Go's own idiom agrees: `var x T`
// for a variable that starts at its zero value, an initializer for one that does not.
//
// Only a var spec with both a type and an initializer is judged, and only an initializer the reader can see is the
// zero: a literal whose constant value is the zero of a basic type (`0`, `0.0`, `0x0`, `""`), the predeclared
// `false`, or the predeclared `nil`. Without a type, `var count = 0` names its type through the value, and the value
// cannot go. A composite literal, `Options{}`, is a value written out and is never read as a restated zero: for a
// slice or a map it is not even the zero, since `[]int{}` is empty and not nil. A named constant, a conversion, a
// negation and a constant expression are left alone too, since each says something a bare zero does not, and the
// rule would rather miss one than argue with a writer's choice of spelling.
package consistency_no_undefined_initializer

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-undefined-initializer"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_undefined_initializer",
	Doc: "Flags a var with a type whose initializer is that type's zero value written as a literal, false or nil " +
		"(var count int = 0, var readError error = nil), and offers to drop the initializer, since Go already gives " +
		"the variable that value. A var with no type, a composite literal, and a named constant are not judged.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads type information for the declared type and the
// initializer's constant value, and to know that a false or nil is the predeclared one.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var restatedZeroValueMessage = policy.MessageOf(Name, "restatedZeroValue")

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			declaration, isDeclaration := node.(*ast.GenDecl)
			if !isDeclaration || declaration.Tok != token.VAR {
				return true
			}
			for _, spec := range declaration.Specs {
				if value, isValue := spec.(*ast.ValueSpec); isValue && restatesZeroValue(pass, value) {
					report(pass, file, value)
				}
			}
			// A var's initializer can hold a function literal with var declarations of its own, so the walk goes on.
			return true
		})
	}
	return nil, nil
}

// restatesZeroValue is whether spec declares a type and initializes every name to that type's zero value. A spec
// with one value per name is the only shape judged: `var first, second int = pair()` initializes from a call, and a
// spec where only some values are zero cannot drop its initializer without being split, which is a rewrite rather
// than a removal.
func restatesZeroValue(pass *analysis.Pass, spec *ast.ValueSpec) bool {
	if spec.Type == nil || len(spec.Values) == 0 || len(spec.Values) != len(spec.Names) {
		return false
	}
	declared := pass.TypesInfo.TypeOf(spec.Type)
	if declared == nil {
		return false
	}
	for _, value := range spec.Values {
		if !isZeroValueOf(pass, ast.Unparen(value), declared) {
			return false
		}
	}
	return true
}

// isZeroValueOf is whether value, already unwrapped from its parentheses, is the zero of declared written so a reader
// sees it is: the predeclared nil, or a literal or the predeclared false whose constant value is the zero of a basic
// type.
func isZeroValueOf(pass *analysis.Pass, value ast.Expr, declared types.Type) bool {
	typeAndValue, recorded := pass.TypesInfo.Types[value]
	if !recorded {
		return false
	}
	// IsNil is true only for the predeclared nil, so a local named nil is never mistaken for it. Go assigns nil only
	// to a type whose zero is nil, a pointer, slice, map, channel, function, interface, unsafe.Pointer or a type
	// parameter whose every type is one of those, so the type checker has already proven the rest.
	if typeAndValue.IsNil() {
		return true
	}
	// The zero of an interface is nil, so `var boxed any = 0` holds an int and is no restated zero. Only a basic
	// type, named or not, has a constant for its zero. A type parameter's underlying type is its constraint, an
	// interface, so `var value T = 0` is left alone as well: its zero is 0 only through every type in the
	// constraint's type set, which the rule does not read.
	if _, isBasic := declared.Underlying().(*types.Basic); !isBasic || typeAndValue.Value == nil {
		return false
	}
	switch written := value.(type) {
	case *ast.BasicLit:
	case *ast.Ident:
		// A package may declare its own false, and one that holds false, `const false = 1 == 2`, is a named constant
		// like any other, so the identifier counts only when it resolves to the universe's. A local variable named
		// false holds no constant and was already turned away above.
		if pass.TypesInfo.Uses[written] != types.Universe.Lookup("false") {
			return false
		}
	default:
		return false
	}
	switch typeAndValue.Value.Kind() {
	case constant.Bool:
		return !constant.BoolVal(typeAndValue.Value)
	case constant.String:
		return constant.StringVal(typeAndValue.Value) == ""
	case constant.Int, constant.Float, constant.Complex:
		// Sign is zero for a complex value only when both its parts are.
		return constant.Sign(typeAndValue.Value) == 0
	case constant.Unknown:
	}
	return false
}

// report flags spec's initializer and offers to drop it, from the end of the type to the end of the last value, so
// the equals sign and the space around it go with the values. A comment inside that range would go with them, so a
// spec holding one there is reported without a fix, and the writer moves the comment and drops the rest by hand. A
// comment after the last value is outside the range and stays.
func report(pass *analysis.Pass, file *ast.File, spec *ast.ValueSpec) {
	removalStart := spec.Type.End()
	removalEnd := spec.Values[len(spec.Values)-1].End()
	names := make([]string, 0, len(spec.Names))
	for _, name := range spec.Names {
		names = append(names, name.Name)
	}
	values := make([]string, 0, len(spec.Values))
	for _, value := range spec.Values {
		values = append(values, types.ExprString(value))
	}
	initializer := strings.Join(values, ", ")
	diagnostic := analysis.Diagnostic{
		Pos:      spec.Values[0].Pos(),
		End:      removalEnd,
		Category: restatedZeroValueMessage.Id,
		Message: restatedZeroValueMessage.Render(map[string]string{
			"names":  strings.Join(names, ", "),
			"type":   types.ExprString(spec.Type),
			"values": initializer,
		}),
	}
	if !holdsComment(file, removalStart, removalEnd) {
		diagnostic.SuggestedFixes = []analysis.SuggestedFix{{
			Message:   "Drop = " + initializer,
			TextEdits: []analysis.TextEdit{{Pos: removalStart, End: removalEnd}},
		}}
	}
	pass.Report(diagnostic)
}

// holdsComment is whether any of file's comments overlaps the range from start to end.
func holdsComment(file *ast.File, start token.Pos, end token.Pos) bool {
	for _, group := range file.Comments {
		if group.Pos() < end && group.End() > start {
			return true
		}
	}
	return false
}
