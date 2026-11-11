// Package copy_lock is vet's copylock pass, a lock passed or copied by value. A value whose pointer is a
// sync.Locker and which is not one itself, such as sync.Mutex, sync.WaitGroup or a struct holding one, works only
// through a pointer: a copy has its own state, so both the copy and the original may malfunction. The pass reports
// such a value where it is copied: assigned, declared, placed in a composite literal, returned, passed to a call,
// taken by a function as a receiver or parameter, bound by a range clause, or, from Go 1.22, copied into a fresh
// loop variable on every iteration of a three-clause for loop. A composite literal or a call on the right is a new
// value, not a copy, so it is not judged. It sits on the engine's syntax dispatcher, hearing the nine kinds it judges
// on the walk the dispatcher makes once for every syntax rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/copylock/copylock.go, whose
// logic it keeps and whose messages it reports word for word, at the same position. Its walk of a type for a
// lock, lockPath, with typeparams.StructuralTerms and its term lists, is translated into type_predicates/locks,
// which the restore from the host shares (#b2t9xmf), and versions.FileVersion and versions.AtLeast over go/version
// below. The registry runs the port in the original's place, and the original stays the oracle: the parity tests
// beside this file run both over the same packages.
//
// It differs in one place. In code that does not type-check, an assignment may hold more values than operands,
// `first = 1, mutex` or `for index := 0, mutex; ...`, and the type checker types every value all the same. The
// original reads the operand at a lock's index, past the end of the operands, and panics, which stops every
// analyzer in the run. The port reports nothing for a value with no operand to report it against, since the type
// checker already reports the mismatch. The original cannot run beside the port over such code, so the differences
// test runs the port alone there.
package copy_lock

import (
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"go/version"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/type_predicates/locks"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "copylocks"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check for locks erroneously passed by value\n\n" +
	"Inadvertently copying a value containing a lock, such as sync.Mutex or\n" +
	"sync.WaitGroup, may cause both copies to malfunction. Generally such\n" +
	"values should be referred to through a pointer."

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/copylock"

// Listener is what the port hears of each package from the dispatcher: every node of the nine kinds the original's
// WithStack visits, in the same preorder, each file before anything inside it, and each with its ancestors, whose
// last is the parent the original reads from its stack.
var Listener = rule.Listener{
	Nodes: []ast.Node{
		(*ast.AssignStmt)(nil),
		(*ast.CallExpr)(nil),
		(*ast.CompositeLit)(nil),
		(*ast.File)(nil),
		(*ast.FuncDecl)(nil),
		(*ast.FuncLit)(nil),
		(*ast.GenDecl)(nil),
		(*ast.RangeStmt)(nil),
		(*ast.ReturnStmt)(nil),
	},
	Node: checkNode,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener. It runs despite type errors, as the original does.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, and its
// running despite type errors, which RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	analyzer.RunDespiteErrors = true
	return analyzer
}

// loopVariablesPerIteration is the Go version from which each iteration of a for loop declares its variables afresh,
// copying their values from the previous iteration's.
const loopVariablesPerIteration = "go1.22"

// walkState is what the port keeps over a package's walk, in the context's state.
type walkState struct {
	// currentFileVersion is the effective Go version of the file being walked, "" when it is unknown. The walk meets
	// each file before anything inside it, as the original's WithStack does.
	currentFileVersion string
}

// checkNode judges node by its kind, as the original's WithStack callback does. stack is node's ancestors, so its
// last is node's parent.
func checkNode(context *rule.Context, node ast.Node, stack []ast.Node) {
	switch node := node.(type) {
	case *ast.File:
		rule.State[walkState](context).currentFileVersion = fileVersion(context.TypesInfo, node)
	case *ast.RangeStmt:
		checkRange(context, node)
	case *ast.FuncDecl:
		checkFunction(context, node.Name.Name, node.Recv, node.Type)
	case *ast.FuncLit:
		checkFunction(context, "func", nil, node.Type)
	case *ast.CallExpr:
		checkCall(context, node)
	case *ast.AssignStmt:
		// An assignment sits inside a file, so its ancestors hold the file at least, and their last is its parent.
		checkAssignment(context, node, rule.State[walkState](context).currentFileVersion, stack[len(stack)-1])
	case *ast.GenDecl:
		checkVariableDeclaration(context, node)
	case *ast.CompositeLit:
		checkCompositeLiteral(context, node)
	case *ast.ReturnStmt:
		checkReturn(context, node)
	}
}

// checkAssignment reports a value an assignment copies a lock from, and, in a file at Go 1.22 or later, a variable
// a for loop's `:=` init declares holding a lock, since each iteration copies it into the next one's variable. Once
// any value is reported, no loop variable is, so one statement is not reported twice. x/tools'
// checkCopyLocksAssign.
func checkAssignment(context *rule.Context, assignment *ast.AssignStmt, currentFileVersion string, parent ast.Node) {
	reportedValue := false
	for index, value := range assignment.Rhs {
		path := lockPathOfValue(context, value)
		// A value past the last operand does not type-check, and has no name to be reported against (see the
		// package doc).
		if path == nil || index >= len(assignment.Lhs) {
			continue
		}
		context.ReportRangef(
			value, "assignment copies lock value to %v: %v", formatted(context.FileSet, assignment.Lhs[index]), path,
		)
		reportedValue = true
	}
	if reportedValue || assignment.Tok != token.DEFINE || !atLeast(currentFileVersion, loopVariablesPerIteration) {
		return
	}
	loop, isLoop := parent.(*ast.ForStmt)
	if !isLoop || loop.Init != assignment {
		return
	}
	for _, operand := range assignment.Lhs {
		identifier, isIdentifier := operand.(*ast.Ident)
		if !isIdentifier || identifier.Name == "_" {
			continue
		}
		// Defs holds nothing for a name `:=` redeclares rather than declares, which a for init cannot do, and the
		// object's type is missing only in code that does not type-check.
		object := context.TypesInfo.Defs[identifier]
		if object == nil || object.Type() == nil {
			continue
		}
		if path := locks.Of(object.Type()); path != nil {
			context.ReportRangef(
				operand, "for loop iteration copies lock value to %v: %v", formatted(context.FileSet, operand), path,
			)
		}
	}
}

// checkVariableDeclaration reports a value a var declaration copies a lock from. x/tools' checkCopyLocksGenDecl.
func checkVariableDeclaration(context *rule.Context, declaration *ast.GenDecl) {
	if declaration.Tok != token.VAR {
		return
	}
	for _, specification := range declaration.Specs {
		// The parser makes every specification of a var declaration a value specification.
		valueSpecification, isValueSpecification := specification.(*ast.ValueSpec)
		if !isValueSpecification {
			continue
		}
		for index, value := range valueSpecification.Values {
			path := lockPathOfValue(context, value)
			// A value past the last name does not type-check, and the type checker leaves it untyped, so its path is
			// nil and the original never indexes past the names here. The bound keeps an index past them from
			// panicking should that change.
			if path == nil || index >= len(valueSpecification.Names) {
				continue
			}
			context.ReportRangef(
				value, "variable declaration copies lock value to %v: %v", valueSpecification.Names[index].Name, path,
			)
		}
	}
}

// checkCompositeLiteral reports an element of a composite literal, or the value of a keyed one, that copies a lock.
// A key is not judged, as the original does not judge it. x/tools' checkCopyLocksCompositeLit.
func checkCompositeLiteral(context *rule.Context, literal *ast.CompositeLit) {
	for _, element := range literal.Elts {
		value := element
		if keyed, isKeyed := element.(*ast.KeyValueExpr); isKeyed {
			value = keyed.Value
		}
		if path := lockPathOfValue(context, value); path != nil {
			context.ReportRangef(
				value,
				"literal copies lock value from %v: %v",
				formatted(context.FileSet, value),
				path,
			)
		}
	}
}

// checkReturn reports a result a return statement copies a lock from. x/tools' checkCopyLocksReturnStmt.
func checkReturn(context *rule.Context, statement *ast.ReturnStmt) {
	for _, result := range statement.Results {
		if path := lockPathOfValue(context, result); path != nil {
			context.ReportRangef(result, "return copies lock value: %v", path)
		}
	}
}

// checkCall reports an argument a call copies a lock from, except to a builtin that reads only its argument's type
// or copies nothing. x/tools' checkCopyLocksCallExpr.
func checkCall(context *rule.Context, call *ast.CallExpr) {
	// The identifier naming the function called, nil when the call names none, as a call of a function literal or of
	// an indexed function does. Uses holds nothing for nil.
	var called *ast.Ident
	switch function := call.Fun.(type) {
	case *ast.Ident:
		called = function
	case *ast.SelectorExpr:
		called = function.Sel
	}
	if builtin, isBuiltin := context.TypesInfo.Uses[called].(*types.Builtin); isBuiltin {
		switch builtin.Name() {
		case "len", "cap", "Sizeof", "Offsetof", "Alignof":
			// The argument of this operation is used only for its type, `len(array)`, or the operation does not
			// copy a lock, `len(slice)`.
			return
		}
	}
	for _, argument := range call.Args {
		if path := lockPathOfValue(context, argument); path != nil {
			context.ReportRangef(
				argument,
				"call of %s copies lock value: %v",
				formatted(context.FileSet, call.Fun),
				path,
			)
		}
	}
}

// checkFunction reports a receiver or a parameter, once for each field however many names it declares, whose type
// holds a lock, so every call copies one. Results are not judged: `return T{}` returns a zero value, and copying
// one is left to checkReturn. x/tools' checkCopyLocksFunc.
func checkFunction(context *rule.Context, name string, receiver *ast.FieldList, signature *ast.FuncType) {
	if receiver != nil && len(receiver.List) > 0 {
		receiverType := receiver.List[0].Type
		if path := locks.Of(context.TypesInfo.Types[receiverType].Type); path != nil {
			context.ReportRangef(receiverType, "%s passes lock by value: %v", name, path)
		}
	}
	if signature.Params == nil {
		return
	}
	for _, field := range signature.Params.List {
		if path := locks.Of(context.TypesInfo.Types[field.Type].Type); path != nil {
			context.ReportRangef(field.Type, "%s passes lock by value: %v", name, path)
		}
	}
}

// checkRange reports a range clause's key or value that copies a lock out of what it ranges over. x/tools'
// checkCopyLocksRange.
func checkRange(context *rule.Context, statement *ast.RangeStmt) {
	checkRangeVariable(context, statement.Tok, statement.Key)
	checkRangeVariable(context, statement.Tok, statement.Value)
}

// checkRangeVariable reports one of a range clause's key or value, absent when variable is nil, that copies a lock.
// A variable `:=` declares is judged by the object it defines, and one `=` assigns by the type of the expression.
// x/tools' checkCopyLocksRangeVar.
func checkRangeVariable(context *rule.Context, operator token.Token, variable ast.Expr) {
	if variable == nil {
		return
	}
	identifier, isIdentifier := variable.(*ast.Ident)
	if isIdentifier && identifier.Name == "_" {
		return
	}
	var variableType types.Type
	if operator == token.DEFINE {
		// The parser accepts any expression before `:=`, and only an identifier declares a variable.
		if !isIdentifier {
			return
		}
		object := context.TypesInfo.Defs[identifier]
		if object == nil {
			return
		}
		variableType = object.Type()
	} else {
		variableType = context.TypesInfo.Types[variable].Type
	}
	if variableType == nil {
		return
	}
	if path := locks.Of(variableType); path != nil {
		context.Reportf(variable.Pos(), "range var %s copies lock: %v", formatted(context.FileSet, variable), path)
	}
}

// formatted is node printed as Go source, as the messages name an operand. x/tools' astutil.Format.
func formatted(files *token.FileSet, node ast.Node) string {
	var builder strings.Builder
	// Ignored: the original ignores it too. A strings.Builder never fails a write, and the printer fails otherwise
	// only on a node it cannot print, which an expression the parser built is not.
	_ = printer.Fprint(&builder, files, node)
	return builder.String()
}

// fileVersion is the Go version file is checked at, its //go:build line's or its module's, as the type checker
// recorded it, and "" when the version is unknown, which atLeast takes as later than every release. x/tools'
// versions.FileVersion.
func fileVersion(information *types.Info, file *ast.File) string {
	if recorded := information.FileVersions[file]; version.IsValid(recorded) {
		return recorded
	}
	return ""
}

// atLeast reports whether the language version of fileVersion is release's or later. An unknown version, "", is a
// future one, later than every release. Language versions are compared, so go1.22rc1 and go1.22.3 are both at
// go1.22. x/tools' versions.AtLeast.
func atLeast(fileVersion string, release string) bool {
	if fileVersion == "" {
		return true
	}
	return version.Compare(version.Lang(fileVersion), version.Lang(release)) >= 0
}
