// Package consistency_no_empty_function is the Go engine's rule that an empty function body says why it is empty.
//
// An empty body reads the same whether it is a deliberate no-op, a method an interface asks for that this type has
// nothing to do in, a callback that ignores what it is handed, or work someone meant to write and forgot. Only its
// author knows which, so the body says so in a comment inside its braces, where the next reader looks first. A doc
// comment above the function does not count: it tells a caller what the function is for, not why its body is empty,
// as the TypeScript twin reads it. An empty test is the sharpest case: it passes every run and can never fail.
//
// One empty method is no empty function at all: a sealed union's marker. Go has no sum types, so a closed union is
// spelled as an interface with one unexported method, `type InstructionValue interface { instructionValue() }`, and
// each variant declares that method with an empty body, `func (*LoadLocal) instructionValue() {}`. The method is
// membership, not behavior, the way consistency-require-one-type-per-file reads it through markertype, and the
// interface's own doc comment is where its reason lives, once, not in every variant. So an empty method is left alone
// when all of these hold:
//
//   - It has markertype's marker shape: a receiver, no parameters, no results and an empty body. A method taking
//     something or answering something is an operation, and an empty one is a null object that says why.
//   - Its name is unexported and it is the only method of an interface declared in this package, with nothing
//     embedded. An unexported name is what seals the union, since no type outside the package can declare it, and an
//     interface with a second method or an embedded one asks for behavior, so its empty method is an ordinary no-op.
//   - The receiver's type implements that interface, so the method is the variant's membership and not a method that
//     happens to share the marker's name.
//   - The method is never used: no call, method value or method expression names it, through the interface or
//     through any type that implements it. This is what tells a marker from a real no-op of the same shape, like an
//     empty `close()` of `closer{ close() }` that some caller calls: a marker is only ever read by a type switch or
//     an assertion, never invoked, and a method someone invokes does nothing they asked for, which wants a reason.
//     The name is unexported, so every use is inside this package and reading this package's uses is complete.
package consistency_no_empty_function

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/goengine/rules/markertype"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-empty-function"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_empty_function",
	Doc: "Flags a function, method or function literal whose body holds no statement and no comment with words in " +
		"it between its braces, tests included, since an empty test can never fail. A doc comment above the " +
		"function does not count. A declaration with no body, implemented in assembly or linked by name, has " +
		"nothing to judge, and neither has a sealed union's marker: the never used method of a one-method " +
		"unexported interface, which marks its variants' membership and does nothing by design.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads the syntax tree and its comments, and the type
// information that tells a sealed union's marker from any other empty method.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var emptyFunctionMessage = policy.MessageOf(Name, "emptyFunction")

func run(pass *analysis.Pass) (any, error) {
	unusedMarkers := unusedMarkerInterfaces(pass)
	for _, file := range pass.Files {
		judgeFile(pass, file, unusedMarkers)
	}
	return nil, nil
}

// unusedMarkerInterfaces gathers, once for the package, every interface that can seal a union, by its one method's
// name, keeping those whose method nothing in the package ever uses. An interface can seal a union when it is declared
// here with exactly one method, unexported, and nothing embedded. A use is any selector, a call, a method value or a
// method expression, that resolves to a method of that name whose receiver implements the interface: the interface's
// own method, a variant's, or one promoted through an embedding, all of which name the same membership.
func unusedMarkerInterfaces(pass *analysis.Pass) map[string][]*types.Interface {
	markerInterfaces := map[string][]*types.Interface{}
	for _, object := range pass.TypesInfo.Defs {
		typeName, isTypeName := object.(*types.TypeName)
		if !isTypeName {
			continue
		}
		// A type parameter's underlying type is its constraint, which declares what a type argument must have and
		// seals nothing.
		if _, isTypeParameter := typeName.Type().(*types.TypeParam); isTypeParameter {
			continue
		}
		declared, isInterface := typeName.Type().Underlying().(*types.Interface)
		if !isInterface || declared.NumExplicitMethods() != 1 || declared.NumEmbeddeds() != 0 {
			continue
		}
		method := declared.ExplicitMethod(0)
		if method.Exported() {
			continue
		}
		markerInterfaces[method.Name()] = append(markerInterfaces[method.Name()], declared)
	}
	if len(markerInterfaces) == 0 {
		return markerInterfaces
	}
	// The receiver of every method a selector names, by the method's name. A method is only ever named through a
	// selector, and Selections holds each one with the method it resolves to, the same object Uses records at the
	// selector's name, so reading Uses as well would find nothing more. A method of a generic type is read through its
	// origin, whose receiver is the type as declared rather than one instantiation of it.
	usedReceivers := map[string][]types.Type{}
	for _, selection := range pass.TypesInfo.Selections {
		method, isMethod := selection.Obj().(*types.Func)
		if !isMethod {
			continue
		}
		if _, isMarkerName := markerInterfaces[method.Name()]; !isMarkerName {
			continue
		}
		usedReceivers[method.Name()] = append(usedReceivers[method.Name()], method.Origin().Signature().Recv().Type())
	}
	unusedMarkers := map[string][]*types.Interface{}
	for name, declaredInterfaces := range markerInterfaces {
		for _, declared := range declaredInterfaces {
			isUsed := slices.ContainsFunc(usedReceivers[name], func(receiver types.Type) bool {
				return types.Implements(receiver, declared)
			})
			if !isUsed {
				unusedMarkers[name] = append(unusedMarkers[name], declared)
			}
		}
	}
	return unusedMarkers
}

// isUnusedMarker is whether function is a sealed union's marker, as the package comment reads one: markertype's
// marker shape, on a receiver that implements an interface in unusedMarkers under the method's own name.
func isUnusedMarker(pass *analysis.Pass, function *ast.FuncDecl, unusedMarkers map[string][]*types.Interface) bool {
	if !markertype.IsMarkerMethod(function) {
		return false
	}
	method, isMethod := pass.TypesInfo.Defs[function.Name].(*types.Func)
	if !isMethod {
		return false
	}
	return slices.ContainsFunc(unusedMarkers[method.Name()], func(declared *types.Interface) bool {
		return types.Implements(method.Signature().Recv().Type(), declared)
	})
}

// judgeFile reports every empty function body in file that holds no comment saying why and is no sealed union's
// marker.
func judgeFile(pass *analysis.Pass, file *ast.File, unusedMarkers map[string][]*types.Interface) {
	// Each comment that has words in it, by where it opens. The parser gathers file.Comments in source order, so the
	// slice is sorted and a body's comments are found by a binary search for its opening brace.
	commentOpenings := []token.Pos{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if hasWords(comment) {
				commentOpenings = append(commentOpenings, comment.Slash)
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		var body *ast.BlockStmt
		switch function := node.(type) {
		case *ast.FuncDecl:
			if isUnusedMarker(pass, function, unusedMarkers) {
				return true
			}
			body = function.Body
		case *ast.FuncLit:
			body = function.Body
		default:
			return true
		}
		// A declaration without a body is implemented elsewhere, in assembly or through a linkname directive.
		if body == nil || !holdsNoStatement(body) {
			return true
		}
		// The first comment opening after the brace is inside the body when it opens before the closing brace.
		index, _ := slices.BinarySearch(commentOpenings, body.Lbrace+1)
		if index < len(commentOpenings) && commentOpenings[index] < body.Rbrace {
			return true
		}
		pass.Report(analysis.Diagnostic{
			Pos:      body.Lbrace,
			End:      body.Rbrace + 1,
			Category: emptyFunctionMessage.Id,
			Message:  emptyFunctionMessage.Render(map[string]string{}),
		})
		return true
	})
}

// holdsNoStatement is whether body has nothing in it to run. A semicolon written alone parses as an empty statement,
// which runs nothing and which gofmt drops, so `{ ; }` is as empty as `{}`.
func holdsNoStatement(body *ast.BlockStmt) bool {
	for _, statement := range body.List {
		if _, isEmpty := statement.(*ast.EmptyStmt); !isEmpty {
			return false
		}
	}
	return true
}

// hasWords is whether comment says anything once its slashes, stars and spaces are set aside, so `//` or `/**/`
// alone inside a body tells the next reader nothing and excuses nothing.
func hasWords(comment *ast.Comment) bool {
	return strings.TrimFunc(comment.Text, func(character rune) bool {
		return character == '/' || character == '*' || unicode.IsSpace(character)
	}) != ""
}
