// Package consistency_no_property_alias is the Go engine's rule that a local does not exist only to rename a field.
//
//	name := server.name
//	return name
//
// The reach `server.name` carries where the value came from and reads the same in any window; the local makes a
// reader scroll up to learn it. A naked local should mean this scope made the value. The repair writes the reach
// where the local is read and deletes the declaration. It ports `nexus/consistency-no-property-alias` and its
// Swift twin, and in Go the Swift twin's exactness is the one that holds.
//
// A Go field is never a getter, so the TypeScript rule's worry about computed properties is gone, but a field read
// through a pointer can change under any call, any write and any channel operation, and a struct copied into a local
// is a value the field no longer shares. Nothing in the syntax tells a snapshot taken on purpose from a rename, so
// what is flagged is the case where the reach reads the same field the local did: the local is read exactly once,
// and nothing that could run code or write anything happens between its declaration and that read. Then the reach
// is evaluated at most once, at a moment nothing could have changed the field. A nil root is the one difference:
// the local panics at its declaration, and the reach at the read, or never, when a return lies between. Each item
// below is silent on purpose:
//
//   - A declaration other than one name bound to one field read with no type: `name, port := ...` binds two, and
//     `var name any = server.name` converts.
//   - A chain that is anything but names and field reads: a call (`current().name`), an index (`items[0].name`),
//     a dereference (`(*pointer).name`), a type assertion, a method value (`run := server.run`), or a qualified
//     identifier (`ErrClosed := net.ErrClosed`), which names another package's variable rather than a field.
//   - A local read more than once, which may be a snapshot of state another call or goroutine writes.
//   - A read inside a function literal, which runs on its own schedule and may be capturing the value on purpose,
//     or inside the repeated part of a loop the declaration is outside of, where the reach would run every turn.
//   - A read that writes to or through the local, or takes its address, directly or through a field, an index or a
//     dereference: `settings.port = 3`, `&name`, a pointer method on a value (`buffer.WriteString`), or a slice of an
//     array (`values[:]`). The local is a copy, and the write lands on it rather than on the field.
//   - Code that could change the field between the declaration and the read: a call (a conversion and the builtins
//     that only measure or allocate excepted), an assignment, an increment, a send, a receive, a select, or a range
//     that assigns, receives or calls. Go leaves the order of a variable's read and a call in one expression
//     unspecified, so a call anywhere in the read's own expression counts too, before or after it.
//   - A function holding a goto, which can run the read again without the declaration.
//   - A read where the chain's first name names something else, because a declaration in between shadows it.
//
// Known misses, every one a finding not made and never one invented: an alias read more than once with nothing
// between, an alias read once after a harmless call (`fmt.Println`), and a local named for its own root
// (`node := node.node`), whose reach the scope check cannot tell from a shadowed one.
package consistency_no_property_alias

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/astutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-property-alias"

// listener hears every assignment and declaration statement of the syntax dispatcher's walk.
var listener = rule.Listener{Nodes: []ast.Node{(*ast.AssignStmt)(nil), (*ast.DeclStmt)(nil)}, Node: judgeStatement}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_property_alias", "Flags a local inside a function that "+
	"binds one name to a field read of the same name, name := server.name, and is read exactly once with nothing that "+
	"could run code or write between, so the reach written at the read reads the same field, and a nil root panics "+
	"there rather than at the declaration. A local read again, in a closure or a loop, written through, or read after "+
	"a call, may be a snapshot, so it is not judged.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var noPropertyAliasMessage = policy.MessageOf(Name, "noPropertyAlias")

// measuringBuiltins are the builtins that read or allocate and write nothing, so a call to one between the
// declaration and the read cannot change the field. copy, clear and append write into a backing array a pointer
// in the chain may point into, and the rest close, delete, panic, print or recover, so each of them counts.
var measuringBuiltins = map[string]bool{
	"len": true, "cap": true, "min": true, "max": true, "real": true, "imag": true, "complex": true, "new": true,
	"make": true,
}

// judgeStatement judges a declaration of one local in the body of the function or function literal that most
// closely encloses it, among its ancestors in stack, which head with its file. A function literal declares its own
// locals, so a declaration inside one is judged against the literal's body and never its enclosing function's.
func judgeStatement(context *rule.Context, node ast.Node, stack []ast.Node) {
	if len(stack) == 0 {
		return
	}
	file, isFile := stack[0].(*ast.File)
	if !isFile {
		return
	}
	body := enclosingBody(stack)
	if body == nil {
		return
	}
	switch statement := node.(type) {
	case *ast.AssignStmt:
		if statement.Tok == token.DEFINE && len(statement.Lhs) == 1 && len(statement.Rhs) == 1 {
			if local, isIdentifier := statement.Lhs[0].(*ast.Ident); isIdentifier {
				judge(context, file, body, statement, local, statement.Rhs[0])
			}
		}
	case *ast.DeclStmt:
		declaration, isGeneral := statement.Decl.(*ast.GenDecl)
		if !isGeneral || declaration.Tok != token.VAR || len(declaration.Specs) != 1 {
			return
		}
		// A declared type may convert, to an interface or from an untyped value, so the local is not the field.
		specification, isValue := declaration.Specs[0].(*ast.ValueSpec)
		if isValue && specification.Type == nil && len(specification.Names) == 1 && len(specification.Values) == 1 {
			judge(context, file, body, statement, specification.Names[0], specification.Values[0])
		}
	}
}

// enclosingBody is the body of the innermost function declaration or function literal among stack, or nil when
// there is none.
func enclosingBody(stack []ast.Node) *ast.BlockStmt {
	for index := len(stack) - 1; index >= 0; index-- {
		switch function := stack[index].(type) {
		case *ast.FuncLit:
			return function.Body
		case *ast.FuncDecl:
			return function.Body
		}
	}
	return nil
}

// judge reports the declaration of local from value when it is a pure alias of a field: the same name, a chain of
// names and field reads, and one read of the local that the reach could stand in for unchanged.
func judge(
	context *rule.Context,
	file *ast.File,
	body *ast.BlockStmt,
	statement ast.Stmt,
	local *ast.Ident,
	value ast.Expr,
) {
	selector, isSelector := ast.Unparen(value).(*ast.SelectorExpr)
	if !isSelector || selector.Sel.Name != local.Name || local.Name == "_" {
		return
	}
	localObject, isVariable := context.TypesInfo.Defs[local].(*types.Var)
	if !isVariable {
		return
	}
	root := chainRoot(context, selector)
	if root == nil {
		return
	}
	rootObject, rootIsVariable := context.TypesInfo.Uses[root].(*types.Var)
	if !rootIsVariable {
		return
	}
	read := onlyRead(context, body, localObject)
	if read == nil || containsGoto(body) {
		return
	}
	path, _ := astutil.PathEnclosingInterval(file, read.Pos(), read.End())
	if len(path) == 0 || path[0] != read {
		return
	}
	if runsApartFromTheDeclaration(path, statement) || writesThroughTheRead(context, path) {
		return
	}
	// The reach written at the read must name the same object the declaration read, so a declaration between them
	// that shadows the chain's first name leaves the alias alone.
	scope := context.Package.Scope().Innermost(read.Pos())
	if scope == nil {
		return
	}
	if _, found := scope.LookupParent(root.Name, read.Pos()); found != rootObject {
		return
	}
	if codeRunsBetween(context, body, statement, read, evaluationUnit(path)) {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos:      local.Pos(),
		End:      value.End(),
		Category: noPropertyAliasMessage.Id,
		Message: noPropertyAliasMessage.Render(map[string]string{
			"name":  local.Name,
			"reach": types.ExprString(ast.Unparen(value)),
		}),
	})
}

// chainRoot is the first name of a chain of field reads, through parentheses, or nil when any link is something
// else: a call, an index, a dereference, an assertion, a method, or a package's qualified identifier, which go/types
// records as no selection at all.
func chainRoot(context *rule.Context, selector *ast.SelectorExpr) *ast.Ident {
	current := selector
	for {
		selection, isSelection := context.TypesInfo.Selections[current]
		if !isSelection || selection.Kind() != types.FieldVal {
			return nil
		}
		switch object := ast.Unparen(current.X).(type) {
		case *ast.Ident:
			return object
		case *ast.SelectorExpr:
			current = object
		default:
			return nil
		}
	}
}

// onlyRead is the one identifier in body that uses local, or nil when there are none or more than one. A function
// literal's body is searched too, since a read there is still a read, and go/types records a write to the name
// among its uses, so a local assigned again is never taken for read once.
func onlyRead(context *rule.Context, body *ast.BlockStmt, local *types.Var) *ast.Ident {
	var reads []*ast.Ident
	ast.Inspect(body, func(node ast.Node) bool {
		if identifier, isIdentifier := node.(*ast.Ident); isIdentifier && context.TypesInfo.Uses[identifier] == local {
			reads = append(reads, identifier)
		}
		return true
	})
	if len(reads) != 1 {
		return nil
	}
	return reads[0]
}

// containsGoto is whether body jumps by goto anywhere, which can run a read again without its declaration.
func containsGoto(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if branch, isBranch := node.(*ast.BranchStmt); isBranch && branch.Tok == token.GOTO {
			found = true
		}
		return !found
	})
	return found
}

// runsApartFromTheDeclaration is whether the read, whose path runs from it out to the file, sits somewhere the
// declaration does not run with it: inside a function literal, or in the repeated part of a loop the declaration is
// outside of. A for loop's init and a range's expression run once, so a read there is not repeated.
func runsApartFromTheDeclaration(path []ast.Node, statement ast.Stmt) bool {
	for index := 1; index < len(path); index++ {
		node := path[index]
		// The first node holding the declaration as well is where both run together, so nothing above it counts.
		if node.Pos() <= statement.Pos() && statement.End() <= node.End() {
			return false
		}
		child := path[index-1]
		switch loop := node.(type) {
		case *ast.FuncLit:
			return true
		case *ast.ForStmt:
			if child != loop.Init {
				return true
			}
		case *ast.RangeStmt:
			if child == loop.Body {
				return true
			}
		}
	}
	return false
}

// writesThroughTheRead is whether the read writes to the local or through it, or takes an address in it, which on a
// copy lands somewhere the field never sees. The walk climbs from the read through what keeps reaching into the
// local, a field, an index, a dereference or parentheses, and asks what the outermost of those is used for.
func writesThroughTheRead(context *rule.Context, path []ast.Node) bool {
	for index := 1; index < len(path); index++ {
		child := path[index-1]
		switch parent := path[index].(type) {
		case *ast.ParenExpr, *ast.StarExpr:
			continue
		case *ast.IndexExpr:
			if parent.X == child {
				continue
			}
		case *ast.SelectorExpr:
			selection, isSelection := context.TypesInfo.Selections[parent]
			if !isSelection {
				return false
			}
			if selection.Kind() == types.FieldVal {
				continue
			}
			// A pointer method called on a value takes the value's address, and the method may write through it.
			method, isFunc := selection.Obj().(*types.Func)
			if !isFunc {
				return false
			}
			receiver := method.Signature().Recv()
			return receiver != nil && isPointer(receiver.Type()) && !isPointer(context.TypesInfo.TypeOf(parent.X))
		case *ast.SliceExpr:
			// Slicing an array takes its address, so the slice shares the copy's elements, not the field's.
			if parent.X != child {
				return false
			}
			_, isArray := context.TypesInfo.TypeOf(parent.X).Underlying().(*types.Array)
			return isArray
		case *ast.AssignStmt:
			for _, left := range parent.Lhs {
				if left == child {
					return true
				}
			}
			return false
		case *ast.IncDecStmt:
			return true
		case *ast.UnaryExpr:
			return parent.Op == token.AND
		case *ast.RangeStmt:
			return parent.Key == child || parent.Value == child
		}
		return false
	}
	return false
}

func isPointer(candidate types.Type) bool {
	if candidate == nil {
		return false
	}
	_, isPointerType := candidate.Underlying().(*types.Pointer)
	return isPointerType
}

// evaluationUnit is the outermost node the read is evaluated together with: the expression holding it, and the
// simple statement that expression belongs to, but never a block or a compound statement, whose other parts run
// at other times. Go leaves the order of a variable's read against a call in one such unit unspecified.
func evaluationUnit(path []ast.Node) ast.Node {
	unit := path[0]
	for _, node := range path[1:] {
		switch node.(type) {
		case *ast.FuncLit:
			return unit
		case ast.Expr, *ast.ExprStmt, *ast.AssignStmt, *ast.ReturnStmt, *ast.IncDecStmt, *ast.SendStmt, *ast.GoStmt,
			*ast.DeferStmt, *ast.DeclStmt, *ast.GenDecl, *ast.ValueSpec:
			unit = node
		default:
			return unit
		}
	}
	return unit
}

// codeRunsBetween is whether anything from the end of the declaration to the end of the read's evaluation unit could
// run code or write. A node that holds the read is passed over, since a call whose argument is the read runs after
// it, but its other parts are looked at like anything else. A function literal's body counts although it may never
// run there, which can only add silence.
func codeRunsBetween(
	context *rule.Context,
	body *ast.BlockStmt,
	statement ast.Stmt,
	read *ast.Ident,
	unit ast.Node,
) bool {
	runs := false
	ast.Inspect(body, func(node ast.Node) bool {
		if runs || node == nil || node.End() <= statement.End() || node.Pos() >= unit.End() {
			return false
		}
		holdsTheRead := node.Pos() <= read.Pos() && read.End() <= node.End()
		if !holdsTheRead && node.Pos() >= statement.End() && mayRunCode(context, node) {
			runs = true
		}
		return !runs
	})
	return runs
}

// mayRunCode is whether node, by itself, may run code or write: a call that is no conversion or measuring builtin, an
// assignment to anything already declared, an increment, a send, a receive, a select, or a range that assigns,
// receives from a channel, or calls an iterator.
func mayRunCode(context *rule.Context, node ast.Node) bool {
	switch expression := node.(type) {
	case *ast.CallExpr:
		if typeAndValue, recorded := context.TypesInfo.Types[expression.Fun]; recorded && typeAndValue.IsType() {
			return false
		}
		if name, isIdentifier := ast.Unparen(expression.Fun).(*ast.Ident); isIdentifier {
			_, isBuiltin := context.TypesInfo.Uses[name].(*types.Builtin)
			if isBuiltin && measuringBuiltins[name.Name] {
				return false
			}
		}
		return true
	case *ast.AssignStmt:
		// A := whose every name is new writes nothing that existed before it; one that reuses a name assigns it.
		if expression.Tok != token.DEFINE {
			return true
		}
		for _, left := range expression.Lhs {
			identifier, isIdentifier := left.(*ast.Ident)
			if !isIdentifier || (identifier.Name != "_" && context.TypesInfo.Defs[identifier] == nil) {
				return true
			}
		}
		return false
	case *ast.IncDecStmt, *ast.SendStmt, *ast.SelectStmt:
		return true
	case *ast.UnaryExpr:
		return expression.Op == token.ARROW
	case *ast.RangeStmt:
		if expression.Tok == token.ASSIGN {
			return true
		}
		switch context.TypesInfo.TypeOf(expression.X).Underlying().(type) {
		case *types.Chan, *types.Signature:
			return true
		}
		return false
	}
	return false
}
