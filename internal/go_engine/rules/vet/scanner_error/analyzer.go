// Package scanner_error is vet's scannererr pass, a bufio.Scanner made by bufio.NewScanner, held in a local variable
// and called through Scan in a loop, whose Err method is never called, so an I/O error ends the loop as quietly as the
// end of the input does. A scanner that may escape, used anywhere but as the receiver of a method call, is left
// alone, as is one that reads from a strings.Reader, a bytes.Reader or a bytes.Buffer, which cannot fail to read.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/scannererr/scannererr.go, whose
// logic it keeps and whose message it reports word for word, at the same position. The original finds the calls of
// NewScanner, and the uses of the variable each one is assigned to, through x/tools' internal type index, which
// cohere cannot import. The port requires inspect alone and finds both by walking the syntax in preorder, the order
// the index hands them over in: each call whose typeutil.Callee is NewScanner, and each identifier whose use is the
// variable. The registry runs the port in the original's place, and the original stays the oracle: the parity tests
// beside this file run both over the same packages. The two agree everywhere, so there is no differences fixture.
//
// The port sits on the engine's syntax dispatcher, hearing each call on the walk the dispatcher makes once for every
// syntax rule, with the call's cursor, which it reads where the call sits in its parent. Once the walk has covered the
// package, it walks the identifiers for the scanners' uses on the dispatcher's own inspector, only in a package that
// holds a scanner to judge, as before.
package scanner_error

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore` naming
// it read the same as before the port took the original's place.
const passName = "scannererr"

// passDoc is the original's documentation byte for byte, so the rule's documentation reads the same.
const passDoc = `scannererr: report failure to check bufio.Scanner.Err

This analyzer reports uses of bufio.Scanner in which the result of
NewScanner is assigned to a local variable that is then used in a loop
that calls Scanner.Scan, but lacks a final check of Scanner.Err,
which is how I/O errors are reported.

For example:

	sc := bufio.NewScanner(os.Stdin) // error: "bufio.Scanner sc is used in Scan loop without final check of sc.Err()"
	for sc.Scan() {
		line := sc.Text()
		use(line)
	}
	/* ...no use of sc.Err()... */

To avoid false positives, the analyzer is silent if the scanner is
passed into or out of the function or assigned somewhere other than a
local variable.

It is not this analyzer's goal to ensure proper handling of errors in
all cases, but merely the simple mistakes where the user may have been
oblivious to the existence of the Scanner.Err method.

The analyzer ignores calls to bufio.NewScanner whose argument is an
infallible memory-backed io.Reader such as strings.Reader or bytes.Buffer.
(In such cases, Scan may yet fail if a token or line is too long for the
scanner's internal buffer, but this is rare.)

If you know that errors are impossible for a given scanner, you can
suppress the diagnostic thus:

	_ = sc.Err() // ignore error; neither reading nor scanning can fail

`

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/scannererr"

// Listener is what the port hears of each package from the dispatcher: every call, in preorder, with its cursor, as
// the original's index hands the calls over, and, once the walk is over, the package, whose scanners it judges.
var Listener = rule.Listener{
	Nodes:       []ast.Node{(*ast.CallExpr)(nil)},
	Node:        gatherCandidate,
	PackageExit: judgeCandidates,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	return analyzer
}

// candidate is one call of bufio.NewScanner whose result is assigned to a variable, the scanner, waiting to be judged
// by the scanner's uses.
type candidate struct {
	call    *ast.CallExpr
	scanner *types.Var
}

// packageState is what the port gathers over one package's walk: the candidates, in the order their calls were
// heard, and the inspector the dispatcher walks with, whose identifiers judgeCandidates walks for the scanners' uses.
type packageState struct {
	candidates []candidate
	inspected  *inspector.Inspector
}

// gatherCandidate keeps node as a candidate when it is a call of bufio.NewScanner, over a reader that can fail, whose
// result is assigned to a variable. The calls are heard in the order the original's index hands them over in, the
// order of their functions' identifiers: a call comes before every call inside it, and its function before its
// arguments. The judgment reads where each call sits in its parent, so it reads the call's cursor, as the original's
// does.
func gatherCandidate(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	if !isNewScanner(context, typeutil.Callee(context.TypesInfo, call)) {
		return
	}
	// A reader in memory never fails to read, so a scanner over one is disregarded. Scanning can still fail on a
	// token or line longer than the scanner's buffer, which is much rarer than an I/O error. NewScanner takes one
	// argument, and a pass runs only over a package that type-checks, so Args holds it.
	reader := context.TypesInfo.TypeOf(call.Args[0])
	if isPointerToNamed(reader, "bytes", "Buffer", "Reader") || isPointerToNamed(reader, "strings", "Reader") {
		return
	}
	cursor := context.Cursor()
	assigned := assignedIdentifier(cursor)
	if assigned == nil {
		return
	}
	scanner, isVariable := context.TypesInfo.ObjectOf(assigned).(*types.Var)
	if !isVariable {
		return
	}
	state := rule.State[packageState](context)
	state.candidates = append(state.candidates, candidate{call: call, scanner: scanner})
	// Every cursor of the walk is the same inspector's, the one the dispatcher walks the package with.
	state.inspected = cursor.Inspector()
}

// judgeCandidates reports each candidate whose scanner is used in a Scan loop with no check of its Err, once the walk
// has covered the package, and walks nothing in a package with no candidate.
func judgeCandidates(context *rule.Context) {
	state := rule.State[packageState](context)
	if len(state.candidates) == 0 {
		return
	}
	uses := usesOf(context.TypesInfo, state.inspected, state.candidates)
	for _, judged := range state.candidates {
		scanPosition := scanInLoop(judged.scanner, uses[judged.scanner])
		if !scanPosition.IsValid() {
			continue
		}
		context.Report(analysis.Diagnostic{
			Pos: judged.call.Pos(),
			End: judged.call.End(),
			Message: fmt.Sprintf(
				"bufio.Scanner %q is used in Scan loop at line %d without final check of %s.Err()",
				judged.scanner.Name(), context.FileSet.Position(scanPosition).Line, judged.scanner.Name(),
			),
		})
	}
}

// isNewScanner reports whether callee is bufio's package-level NewScanner, the object the original's index looks up
// as `index.Object("bufio", "NewScanner")`. The lookup asks for no kind of object, so neither does this; in the
// standard library's bufio, the only package at that path, it is a function.
// The index holds the packages the pass refers to, never the pass's own, so a call inside bufio itself is never
// judged. No file of bufio calls NewScanner, so no input reaches that case; it is kept so the port judges what the
// original judges.
func isNewScanner(context *rule.Context, callee types.Object) bool {
	return callee != nil &&
		isPackageLevel(callee) &&
		callee.Pkg() != context.Package &&
		callee.Pkg().Path() == "bufio" &&
		callee.Name() == "NewScanner"
}

// assignedIdentifier is the identifier the call at cursor is assigned to, `var scanner = bufio.NewScanner(reader)`
// or `scanner := bufio.NewScanner(reader)` (or `=`), and nil when the call is not a value of a declaration or the
// right of an assignment, or is assigned to something other than an identifier. A parenthesized call sits in its
// parentheses, not in the assignment, so it is nil too, as it is in the original.
func assignedIdentifier(cursor inspector.Cursor) *ast.Ident {
	// NewScanner returns one value, so in a declaration or assignment that type-checks, the name or left side at the
	// call's index is the one it is assigned to.
	switch kind, index := cursor.ParentEdge(); kind {
	case edge.ValueSpec_Values:
		specification, isSpecification := cursor.Parent().Node().(*ast.ValueSpec)
		if !isSpecification {
			// Invariant: a node on a ValueSpec's Values edge has a ValueSpec for its parent.
			panic("scanner_error: a declared value's parent is no value specification")
		}
		return specification.Names[index]
	case edge.AssignStmt_Rhs:
		assignment, isAssignment := cursor.Parent().Node().(*ast.AssignStmt)
		if !isAssignment {
			// Invariant: a node on an AssignStmt's Rhs edge has an AssignStmt for its parent.
			panic("scanner_error: an assigned value's parent is no assignment")
		}
		identifier, isIdentifier := assignment.Lhs[index].(*ast.Ident)
		if !isIdentifier {
			return nil
		}
		return identifier
	}
	return nil
}

// usesOf is each candidate's scanner's uses, the cursor of every identifier in the package whose use is the scanner,
// in preorder: the order the original's index hands them over in, and the one scanInLoop's last Scan depends on.
// Uses never records a variable's declaring identifier, and does record the left side of an `=` assignment.
func usesOf(
	information *types.Info, inspected *inspector.Inspector, candidates []candidate,
) map[*types.Var][]inspector.Cursor {
	uses := map[*types.Var][]inspector.Cursor{}
	for _, judged := range candidates {
		uses[judged.scanner] = []inspector.Cursor{}
	}
	for cursor := range inspected.Root().Preorder((*ast.Ident)(nil)) {
		identifier, isIdentifier := cursor.Node().(*ast.Ident)
		if !isIdentifier {
			// Invariant: Preorder filtered by *ast.Ident visits nothing else.
			panic("scanner_error: an identifier's cursor holds no identifier")
		}
		used, isVariable := information.Uses[identifier].(*types.Var)
		if !isVariable {
			continue
		}
		if found, isScanner := uses[used]; isScanner {
			uses[used] = append(found, cursor)
		}
	}
	return uses
}

// scanInLoop is the position of the last use, in uses' order, that calls scanner.Scan inside a loop beginning after
// scanner's declaration, and token.NoPos when there is none. It is token.NoPos as well when any use is other than the
// receiver of a method call, since the scanner may then escape, or is a call of scanner.Err, the check the pass asks
// for. The loop is the innermost enclosing the use, through function literals too.
func scanInLoop(scanner *types.Var, uses []inspector.Cursor) token.Pos {
	scanPosition := token.NoPos
	for _, use := range uses {
		if use.ParentEdgeKind() != edge.SelectorExpr_X || use.Parent().ParentEdgeKind() != edge.CallExpr_Fun {
			return token.NoPos
		}
		selector, isSelector := use.Parent().Node().(*ast.SelectorExpr)
		if !isSelector {
			// Invariant: a node on a SelectorExpr's X edge has a SelectorExpr for its parent.
			panic("scanner_error: a selected value's parent is no selector")
		}
		switch selector.Sel.Name {
		case "Err":
			return token.NoPos
		case "Scan":
			loop, inLoop := innermostLoop(use)
			if inLoop && loop.Node().Pos() > scanner.Pos() {
				scanPosition = use.Node().Pos()
			}
		}
	}
	return scanPosition
}

// innermostLoop is the innermost for or range statement enclosing cursor, and false when there is none. x/tools'
// moreiters.First over Cursor.Enclosing, which yields the enclosing nodes innermost first.
func innermostLoop(cursor inspector.Cursor) (inspector.Cursor, bool) {
	for loop := range cursor.Enclosing((*ast.RangeStmt)(nil), (*ast.ForStmt)(nil)) {
		return loop, true
	}
	return inspector.Cursor{}, false
}

// isPointerToNamed reports whether subject is, or is an alias for, a pointer to a package-level defined type of the
// package at packagePath named one of names. x/tools' typesinternal.IsPointerToNamed, with its Unpointer folded in.
func isPointerToNamed(subject types.Type, packagePath string, names ...string) bool {
	pointer, isPointer := types.Unalias(subject).(*types.Pointer)
	return isPointer && isTypeNamed(pointer.Elem(), packagePath, names...)
}

// isTypeNamed reports whether subject is, or is an alias for, a package-level defined type of the package at
// packagePath named one of names. x/tools' typesinternal.IsTypeNamed.
func isTypeNamed(subject types.Type, packagePath string, names ...string) bool {
	named, isNamed := types.Unalias(subject).(*types.Named)
	if !isNamed {
		return false
	}
	typeName := named.Obj()
	return typeName != nil &&
		isPackageLevel(typeName) &&
		typeName.Pkg().Path() == packagePath &&
		slices.Contains(names, typeName.Name())
}

// isPackageLevel reports whether object is declared at its package's scope. x/tools' typesinternal.IsPackageLevel.
func isPackageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
}
