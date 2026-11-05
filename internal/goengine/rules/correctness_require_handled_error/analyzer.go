// Package correctness_require_handled_error is the Go engine's rule that an error a call returns is handled, or
// dropped with a reason.
//
// An error dropped without a word is a failure nobody will see: the file that never closed, the write that never
// landed, the number that never parsed, and the code runs on as if each succeeded. Sometimes dropping it is right,
// a best-effort cleanup whose failure changes nothing, and then the reason is what the next reader needs, so a
// dropped error says why in a comment where a reader looks.
package correctness_require_handled_error

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-handled-error"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "correctness_require_handled_error",
	Doc: "Flags a call that returns an error and drops it, as a statement of its own, as the operand of defer or " +
		"go, or with the error assigned to _, unless a // Ignored: <reason> comment directly above the statement or " +
		"at the end of its line says why. Writes whose error is documented never to be non-nil, such as those to a " +
		"*strings.Builder, or to be kept for a later checked call, a *bufio.Writer's Flush, are exempt by type.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var unhandledErrorMessage = policy.MessageOf(Name, "unhandledError")

var unhandledErrorWithoutReasonMessage = policy.MessageOf(Name, "unhandledErrorWithoutReason")

// ignoredMarker opens the comment that excuses a dropped error.
const ignoredMarker = "Ignored:"

// The callees whose error need not be checked at the call: either it is documented never to be non-nil, so handling
// it would be ceremony around a value that cannot be anything but nil, or it is kept and handed back at a later call
// that is itself checked. Each is matched by its package, type and method, never by how the call is spelled.
var (
	// exemptWriteMethods are the write methods of the exemptWriters, as their documentation gives them:
	//
	//	strings.Builder.Write        "Write always returns len(p), nil."
	//	strings.Builder.WriteByte    "The returned error is always nil."
	//	strings.Builder.WriteRune    "It returns the number of bytes written and a nil error."
	//	strings.Builder.WriteString  "It returns the length of s and a nil error."
	//	bytes.Buffer.Write           "The return value n is the length of p; err is always nil."
	//	bytes.Buffer.WriteByte       "The returned error is always nil, but is included to match bufio.Writer's
	//	                             WriteByte."
	//	bytes.Buffer.WriteRune       "The nil error is included to match bufio.Writer's WriteRune."
	//	bytes.Buffer.WriteString     "The return value n is the length of s; err is always nil."
	//	bufio.Writer, every write    "If an error occurs writing to a Writer, no more data will be accepted and all
	//	                             subsequent writes, and Flush, will return the error."
	//
	// A bufio.Writer's error is sticky, so a dropped write loses nothing as long as its Flush is checked, which is
	// why Flush is absent and stays judged: it is where the error comes back. ReadFrom and WriteTo are absent on
	// purpose too: they read or write another io.Reader or io.Writer, and its error is theirs.
	exemptWriteMethods = map[string]bool{"Write": true, "WriteByte": true, "WriteRune": true, "WriteString": true}
	// exemptWriters are the types the write methods above belong to, by package path and type name, each through a
	// pointer.
	exemptWriters = []struct{ packagePath, typeName string }{
		{"strings", "Builder"},
		{"bytes", "Buffer"},
		{"bufio", "Writer"},
	}
	// exemptPrintFunctions are fmt's printers to a writer: "It returns the number of bytes written and any write
	// error encountered." Formatting itself never fails, so writing to one of the exemptWriters, they return what its
	// write would. Println and Printf write to standard output, which can fail, so they are not here.
	exemptPrintFunctions = map[string]bool{"Fprint": true, "Fprintf": true, "Fprintln": true}
)

// hashPackagePath holds hash.Hash, whose Write the interface itself documents: "Write (via the embedded io.Writer
// interface) adds more data to the running hash. It never returns an error." Every implementation is held to that,
// so the Write of any type implementing hash.Hash is exempt.
const hashPackagePath = "hash"

// excuse is what the comments at a statement say about why its error is dropped.
type excuse int

const (
	excuseNone excuse = iota
	// excuseWithoutReason is a marker with nothing after its colon, which tells the next reader nothing.
	excuseWithoutReason
	excuseWithReason
)

// checker holds what every judgment in one package reads.
type checker struct {
	pass *analysis.Pass
	// errorInterface is the predeclared error's, which a result implements to count as an error.
	errorInterface *types.Interface
	// hashInterface is hash.Hash's, or nil when no package this one imports, directly or not, is hash. A type that
	// implements hash.Hash without the package ever seeing hash makes no promise about its Write, so it is judged.
	hashInterface *types.Interface
}

func run(pass *analysis.Pass) (any, error) {
	errorInterface, isInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !isInterface {
		return nil, fmt.Errorf("reading the predeclared error as an interface")
	}
	checker := checker{pass: pass, errorInterface: errorInterface, hashInterface: findHashInterface(pass.Pkg)}
	for _, file := range pass.Files {
		// A comment excuses a statement from directly above it, the group ending on the line before, or from the end
		// of its line, a group starting there after the statement ends.
		commentEndingOnLine := map[int]*ast.CommentGroup{}
		commentsStartingOnLine := map[int][]*ast.CommentGroup{}
		for _, group := range file.Comments {
			commentEndingOnLine[pass.Fset.PositionFor(group.End(), false).Line] = group
			startLine := pass.Fset.PositionFor(group.Pos(), false).Line
			commentsStartingOnLine[startLine] = append(commentsStartingOnLine[startLine], group)
		}
		excuseAt := func(statement ast.Node) excuse {
			given := excuseOf(commentEndingOnLine[pass.Fset.PositionFor(statement.Pos(), false).Line-1])
			for _, group := range commentsStartingOnLine[pass.Fset.PositionFor(statement.End(), false).Line] {
				if group.Pos() >= statement.End() {
					given = max(given, excuseOf(group))
				}
			}
			return given
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch statement := node.(type) {
			case *ast.ExprStmt:
				if call, isCall := ast.Unparen(statement.X).(*ast.CallExpr); isCall && checker.returnsError(call) {
					checker.report(call, excuseAt(statement))
				}
			case *ast.DeferStmt:
				// A deferred or started call's results go nowhere, whatever they are.
				if checker.returnsError(statement.Call) {
					checker.report(statement.Call, excuseAt(statement))
				}
			case *ast.GoStmt:
				if checker.returnsError(statement.Call) {
					checker.report(statement.Call, excuseAt(statement))
				}
			case *ast.AssignStmt:
				for _, call := range checker.callsDroppingError(statement.Lhs, statement.Rhs) {
					checker.report(call, excuseAt(statement))
				}
			case *ast.ValueSpec:
				// `var value, _ = strconv.Atoi(text)` drops the error as an assignment does.
				names := make([]ast.Expr, 0, len(statement.Names))
				for _, name := range statement.Names {
					names = append(names, name)
				}
				for _, call := range checker.callsDroppingError(names, statement.Values) {
					checker.report(call, excuseAt(statement))
				}
			}
			return true
		})
	}
	return nil, nil
}

// returnsError is whether call has a result that is an error, in any position, and is not one of the exempt callees
// whose error need not be checked at the call.
func (checker checker) returnsError(call *ast.CallExpr) bool {
	if !checker.isFunctionCall(call) {
		return false
	}
	switch result := checker.pass.TypesInfo.TypeOf(call).(type) {
	case *types.Tuple:
		for index := range result.Len() {
			if checker.isError(result.At(index).Type()) {
				return !checker.isExempt(call)
			}
		}
		return false
	default:
		return checker.isError(result) && !checker.isExempt(call)
	}
}

// callsDroppingError is every call on the right of an assignment or var declaration whose error lands in _. A call
// that fills the whole left side, `value, _ := strconv.Atoi(text)`, is judged by the result each _ takes; with one
// value per name, each call stands alone.
func (checker checker) callsDroppingError(left []ast.Expr, right []ast.Expr) []*ast.CallExpr {
	calls := []*ast.CallExpr{}
	if len(right) == 1 && len(left) > 1 {
		call, isCall := ast.Unparen(right[0]).(*ast.CallExpr)
		if !isCall || !checker.isFunctionCall(call) {
			return calls
		}
		// A map index, a channel receive or a type assertion fills two names too, and is no call.
		results, isTuple := checker.pass.TypesInfo.TypeOf(call).(*types.Tuple)
		if !isTuple {
			return calls
		}
		for index, name := range left {
			if index < results.Len() && isBlank(name) && checker.isError(results.At(index).Type()) {
				if !checker.isExempt(call) {
					calls = append(calls, call)
				}
				return calls
			}
		}
		return calls
	}
	if len(left) != len(right) {
		return calls
	}
	for index, name := range left {
		call, isCall := ast.Unparen(right[index]).(*ast.CallExpr)
		if !isCall || !isBlank(name) || !checker.isFunctionCall(call) {
			continue
		}
		if checker.isError(checker.pass.TypesInfo.TypeOf(call)) && !checker.isExempt(call) {
			calls = append(calls, call)
		}
	}
	return calls
}

// isFunctionCall is whether call calls a function, rather than converting to a type or calling a builtin, neither
// of which returns an error of its own.
func (checker checker) isFunctionCall(call *ast.CallExpr) bool {
	callee, known := checker.pass.TypesInfo.Types[call.Fun]
	return known && !callee.IsType() && !callee.IsBuiltin()
}

// isError is whether a result of type candidate is an error: the predeclared error, or any type implementing it.
func (checker checker) isError(candidate types.Type) bool {
	return candidate != nil && types.Implements(candidate, checker.errorInterface)
}

// isExempt is whether call is one of the callees above whose error need not be checked at the call.
func (checker checker) isExempt(call *ast.CallExpr) bool {
	function, isFunc := typeutil.Callee(checker.pass.TypesInfo, call).(*types.Func)
	if !isFunc {
		return false
	}
	if receiver := function.Signature().Recv(); receiver != nil {
		// The method's own receiver, so a Builder embedded in another struct and written through it is the same
		// Builder.WriteString.
		if exemptWriteMethods[function.Name()] && isExemptWriterType(receiver.Type()) {
			return true
		}
		return function.Name() == "Write" && checker.isHashReceiver(call)
	}
	if function.Pkg() == nil || function.Pkg().Path() != "fmt" || !exemptPrintFunctions[function.Name()] {
		return false
	}
	if len(call.Args) == 0 {
		return false
	}
	// The writer's static type: an io.Writer that happens to hold a Builder promises nothing at the call.
	writer := checker.pass.TypesInfo.TypeOf(call.Args[0])
	if isExemptWriterType(writer) {
		return true
	}
	return checker.hashInterface != nil && types.Implements(writer, checker.hashInterface)
}

// isHashReceiver is whether the Write call is on a value whose type implements hash.Hash. It reads the receiver
// expression's type rather than the method's, since a hash.Hash's Write is io.Writer's, declared on io.Writer.
func (checker checker) isHashReceiver(call *ast.CallExpr) bool {
	if checker.hashInterface == nil {
		return false
	}
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	// A method value, hasher.Write(data), or a method expression, hash.Hash.Write(hasher, data); either way the
	// selection's receiver is the type written to.
	selection, isSelection := checker.pass.TypesInfo.Selections[selector]
	if !isSelection {
		return false
	}
	receiver := selection.Recv()
	if types.Implements(receiver, checker.hashInterface) {
		return true
	}
	// An addressable value whose pointer implements hash.Hash, a maphash.Hash variable, calls the pointer's Write.
	if _, isPointer := receiver.Underlying().(*types.Pointer); isPointer || types.IsInterface(receiver) {
		return false
	}
	return types.Implements(types.NewPointer(receiver), checker.hashInterface)
}

// isExemptWriterType is whether candidate is a pointer to one of exemptWriters.
func isExemptWriterType(candidate types.Type) bool {
	pointer, isPointer := types.Unalias(candidate).(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(pointer.Elem()).(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil {
		return false
	}
	for _, writer := range exemptWriters {
		if named.Obj().Pkg().Path() == writer.packagePath && named.Obj().Name() == writer.typeName {
			return true
		}
	}
	return false
}

// findHashInterface is hash.Hash's interface from the import graph under analyzed, analyzed included, or nil when
// hash is not in it.
func findHashInterface(analyzed *types.Package) *types.Interface {
	seen := map[*types.Package]bool{}
	pending := []*types.Package{analyzed}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[current] {
			continue
		}
		seen[current] = true
		if current.Path() == hashPackagePath {
			typeName, isTypeName := current.Scope().Lookup("Hash").(*types.TypeName)
			if !isTypeName {
				return nil
			}
			hashInterface, isInterface := typeName.Type().Underlying().(*types.Interface)
			if !isInterface {
				return nil
			}
			return hashInterface
		}
		pending = append(pending, current.Imports()...)
	}
	return nil
}

// isBlank is whether name is the blank identifier, which keeps nothing it is given.
func isBlank(name ast.Expr) bool {
	identifier, isIdentifier := name.(*ast.Ident)
	return isIdentifier && identifier.Name == "_"
}

// calleeName is how the message names what call calls: a function or method by its package and receiver type,
// with this package's own names left bare, and anything else, a function value or literal, as written.
func (checker checker) calleeName(call *ast.CallExpr) string {
	function, isFunc := typeutil.Callee(checker.pass.TypesInfo, call).(*types.Func)
	if !isFunc {
		return types.ExprString(call.Fun)
	}
	qualifier := func(other *types.Package) string {
		if other == checker.pass.Pkg {
			return ""
		}
		return other.Name()
	}
	if receiver := function.Signature().Recv(); receiver != nil {
		return "(" + types.TypeString(receiver.Type(), qualifier) + ")." + function.Name()
	}
	if function.Pkg() == nil || function.Pkg() == checker.pass.Pkg {
		return function.Name()
	}
	return function.Pkg().Name() + "." + function.Name()
}

// excuseOf reads a comment group for the marker: opening any line of any comment in it, with the */ that closes a
// block comment no part of the reason. A nil group says nothing.
func excuseOf(group *ast.CommentGroup) excuse {
	if group == nil {
		return excuseNone
	}
	found := excuseNone
	for _, comment := range group.List {
		for _, line := range strings.Split(comment.Text, "\n") {
			// The marker opens the line, after the comment's own slashes, stars and spaces, so a sentence that only
			// mentions it, "this was Ignored: once", excuses nothing.
			opening := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "/*"))
			reason, hasMarker := strings.CutPrefix(opening, ignoredMarker)
			if !hasMarker {
				continue
			}
			if strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(reason), "*/")) != "" {
				return excuseWithReason
			}
			found = excuseWithoutReason
		}
	}
	return found
}

// report flags call unless its excuse gives a reason, with the message that names what is missing.
func (checker checker) report(call *ast.CallExpr, given excuse) {
	message := unhandledErrorMessage
	switch given {
	case excuseWithReason:
		return
	case excuseWithoutReason:
		message = unhandledErrorWithoutReasonMessage
	case excuseNone:
	}
	checker.pass.Report(analysis.Diagnostic{
		Pos:      call.Pos(),
		End:      call.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{"callee": checker.calleeName(call)}),
	})
}
