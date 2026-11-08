package printf

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
	"golang.org/x/tools/refactor/satisfy"
)

// wrapper is a function that may wrap a print or printf function. It is a types.Object rather than a *types.Func, so
// a function literal assigned to a variable, `printf := func(format string, args ...any) {...}`, is one too, as the
// variable.
type wrapper struct {
	// object is the *types.Func, or the *types.Var a function literal is assigned to.
	object types.Object

	// body is the function's *ast.BlockStmt, and not valid for an interface method, which has none.
	body inspector.Cursor

	// format is the "format string" parameter, nil for a print-like candidate, and arguments the final
	// "args ...any".
	format    *types.Var
	arguments *types.Var

	// callers are the candidates that forward to this one.
	callers []printfCaller
}

// printfCaller is a call by which caller forwards to another candidate: the call, or nil for an interface method's
// implicit call of a method that implements it.
type printfCaller struct {
	caller *wrapper
	call   *ast.CallExpr
}

// wrapperSearch is one package's search for wrappers: x/tools' printf findPrintLike, whose closures are its methods.
type wrapperSearch struct {
	pass  *analysis.Pass
	kinds wrapperKinds

	candidates []*wrapper
	byObject   map[types.Object]*wrapper
}

// findPrintLike finds every print or printf wrapper of the package, kept in kinds and exported as facts.
func findPrintLike(pass *analysis.Pass, kinds wrapperKinds) {
	walker, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		return
	}
	search := &wrapperSearch{pass: pass, kinds: kinds, byObject: map[types.Object]*wrapper{}}
	// First gather the candidates.
	for cursor := range walker.Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil), (*ast.InterfaceType)(nil)) {
		search.gather(cursor)
	}
	// An interface method behaves as if its body called each method that implements it. Here Logger.Logf is
	// implemented by myLogger.Logf, so when myLogger.Logf is a printf wrapper Logger.Logf is one too:
	//
	//	type Logger interface {
	//		Logf(format string, args ...any)
	//	}
	//	type myLogger struct{ ... }
	//	func (myLogger) Logf(format string, args ...any) {...}
	//	var _ Logger = myLogger{}
	implementations := methodImplementations(pass)
	// Then scan each candidate's body for the calls it forwards.
	for _, candidate := range search.candidates {
		if candidate.body.Valid() {
			search.scanBody(candidate)
			continue
		}
		method, isMethod := candidate.object.(*types.Func)
		if !isMethod {
			continue
		}
		for implementation := range implementations[method] {
			search.forward(candidate, implementation, nil)
		}
	}
}

// gather adds the candidate cursor declares, if any: a declared function or method with a body, a function literal
// assigned to a variable, a field or an imported variable, or each method of an interface in a file of go1.26 or
// later.
func (search *wrapperSearch) gather(cursor inspector.Cursor) {
	info := search.pass.TypesInfo
	switch node := cursor.Node().(type) {
	case *ast.FuncDecl:
		// func wrapf(format string, args ...any) {...}
		if node.Body == nil {
			return
		}
		if function, isFunction := info.Defs[node.Name].(*types.Func); isFunction {
			search.add(function, function.Signature(), cursor.ChildAt(edge.FuncDecl_Body, -1))
		}
	case *ast.FuncLit:
		// var wrapf = func(format string, args ...any) {...}, wrapf := func..., or wrapf = func..., where wrapf may
		// also be a field, x.wrapf, or an imported variable, pkg.Wrapf.
		variable := assignedVariable(info, cursor)
		if variable == nil {
			return
		}
		if signature, isSignature := info.TypeOf(node).(*types.Signature); isSignature {
			search.add(variable, signature, cursor.ChildAt(edge.FuncLit_Body, -1))
		}
	case *ast.InterfaceType:
		// Wrappers found through interface methods are gated as if they were a go1.26 feature, so go test's vet
		// suite growing stricter surprises no one.
		if !fileUsesGoVersion(search.pass, enclosingFile(cursor), go126) {
			return
		}
		declared, isInterface := info.TypeOf(node).(*types.Interface)
		if !isInterface {
			return
		}
		for method := range declared.Methods() {
			search.add(method, method.Signature(), inspector.Cursor{})
		}
	}
}

// assignedVariable is the variable, field or imported variable the function literal at cursor is assigned to as a
// whole, in a var declaration or an assignment, or nil.
func assignedVariable(info *types.Info, cursor inspector.Cursor) *types.Var {
	var assigned ast.Expr
	switch parentEdge, index := cursor.ParentEdge(); parentEdge {
	case edge.ValueSpec_Values:
		// A name is always an identifier, and asserted as one so a failed assertion leaves assigned nil rather than
		// an identifier that is nil.
		if name, isName := cursor.Parent().ChildAt(edge.ValueSpec_Names, index).Node().(*ast.Ident); isName {
			assigned = name
		}
	case edge.AssignStmt_Rhs:
		assigned, _ = cursor.Parent().ChildAt(edge.AssignStmt_Lhs, index).Node().(ast.Expr)
	}
	switch target := assigned.(type) {
	case *ast.Ident:
		// A variable: wrapf = func(...).
		variable, _ := info.ObjectOf(target).(*types.Var)
		return variable
	case *ast.SelectorExpr:
		if selection, isSelection := info.Selections[target]; isSelection {
			// A field: x.wrapf = func(...).
			field, _ := selection.Obj().(*types.Var)
			return field
		}
		// An imported variable: pkg.Wrapf = func(...).
		imported, _ := info.Uses[target.Sel].(*types.Var)
		return imported
	}
	return nil
}

// add records object as a candidate when its signature has the shape of a print or printf function. Which
// candidates are wrappers is decided later, from the graph of their calls and their bodies.
func (search *wrapperSearch) add(object types.Object, signature *types.Signature, body inspector.Cursor) {
	format, arguments := formatArgumentsParameters(signature)
	if arguments == nil {
		return
	}
	candidate := &wrapper{object: object, body: body, format: format, arguments: arguments}
	search.byObject[object] = candidate
	search.candidates = append(search.candidates, candidate)
}

// scanBody looks at every call in candidate's body that passes its arguments last. It stops at the first statement
// that may change the format or the arguments, an assignment to either or its address taken, since a candidate that
// does so is no simple wrapper. Only a change before the call is seen (issue 26555).
func (search *wrapperSearch) scanBody(candidate *wrapper) {
	info := search.pass.TypesInfo
	for cursor := range candidate.body.Preorder((*ast.AssignStmt)(nil), (*ast.UnaryExpr)(nil), (*ast.CallExpr)(nil)) {
		switch node := cursor.Node().(type) {
		case *ast.AssignStmt:
			for _, assigned := range node.Lhs {
				if candidate.format != nil && matchesParameter(info, assigned, candidate.format) ||
					matchesParameter(info, assigned, candidate.arguments) {
					return
				}
			}
		case *ast.UnaryExpr:
			if node.Op == token.AND && (candidate.format != nil && matchesParameter(info, node.X, candidate.format) ||
				matchesParameter(info, node.X, candidate.arguments)) {
				return
			}
		case *ast.CallExpr:
			if len(node.Args) == 0 || !matchesParameter(info, node.Args[len(node.Args)-1], candidate.arguments) {
				continue
			}
			if callee := typeutil.Callee(info, node); callee != nil {
				search.forward(candidate, callee, node)
			}
		}
	}
}

// forward records that candidate calls callee, by call, nil for an interface method's implicit call. When callee is
// a candidate, the edge makes candidate a wrapper once callee is found to be one. When callee is a print or printf
// function already, known by name, by a fact or found, candidate is one of its kind.
func (search *wrapperSearch) forward(candidate *wrapper, callee types.Object, call *ast.CallExpr) {
	if calledCandidate, isCandidate := search.byObject[callee]; isCandidate {
		calledCandidate.callers = append(calledCandidate.callers, printfCaller{caller: candidate, call: call})
	}
	if kind := search.kinds.of(search.pass, callee); kind != kindNone {
		search.propagate(candidate, call, kind)
	}
}

// propagate makes candidate a wrapper of kind when call forwards well, and then each of its known callers, back
// through the graph of forwarding calls. An interface method's implicit call, nil, forwards well by construction.
func (search *wrapperSearch) propagate(candidate *wrapper, call *ast.CallExpr, kind wrapperKind) {
	if call != nil && !search.forwardsWell(candidate, call, kind) {
		return
	}
	if search.kinds[candidate.object] == kind {
		return
	}
	search.kinds[candidate.object] = kind
	// A fact can only be exported on this package's own objects, but a variable of another package is still a
	// wrapper within this one. A fact is about the generic declaration.
	if candidate.object.Pkg() == search.pass.Pkg {
		search.pass.ExportObjectFact(origin(candidate.object), &isWrapper{Kind: kind})
	}
	for _, caller := range candidate.callers {
		search.propagate(caller.caller, caller.call, kind)
	}
}

// forwardsWell reports whether call, by which candidate is known to pass its arguments last, forwards them as a
// wrapper of kind does: a printf's format just before them, and the arguments with `...`. A call that leaves the
// `...` out, where adding it would compile, is reported as fmt.Printf(format, args) written for
// fmt.Printf(format, args...).
func (search *wrapperSearch) forwardsWell(candidate *wrapper, call *ast.CallExpr, kind wrapperKind) bool {
	if kind == kindPrintf || kind == kindErrorf {
		if len(call.Args) < 2 ||
			!matchesParameter(search.pass.TypesInfo, call.Args[len(call.Args)-2], candidate.format) {
			return false
		}
	}
	if call.Ellipsis.IsValid() {
		return true
	}
	signature, isSignature := search.pass.TypesInfo.Types[call.Fun].Type.(*types.Signature)
	if !isSignature {
		return false
	}
	// With more arguments than the callee's parameters, adding `...` would break the program, as in
	//
	//	func foo(arg1 string, arg2 ...interface{}) {
	//		fmt.Printf("%s %v", arg1, arg2)
	//	}
	if len(call.Args) > signature.Params().Len() {
		return false
	}
	search.pass.ReportRangef(call, "missing ... in args forwarded to %s-like function", kind)
	return false
}

// formatArgumentsParameters are the "format string" and final "args ...any" parameters of a function that may be a
// print or printf wrapper, the format nil for a print-like one, and both nil for a signature of neither shape.
func formatArgumentsParameters(signature *types.Signature) (*types.Var, *types.Var) {
	if !signature.Variadic() {
		return nil, nil
	}
	parameters := signature.Params()
	// A variadic signature has a parameter.
	parameterCount := parameters.Len()
	var format *types.Var
	if parameterCount >= 2 {
		if parameter := parameters.At(parameterCount - 2); parameter.Type() == types.Typ[types.String] {
			format = parameter
		}
	}
	// The variadic parameter is a slice, of the empty interface for a wrapper.
	arguments := parameters.At(parameterCount - 1)
	variadic, isSlice := arguments.Type().(*types.Slice)
	if !isSlice {
		return nil, nil
	}
	element, isInterface := types.Unalias(variadic.Elem()).(*types.Interface)
	if !isInterface || !element.Empty() {
		return nil, nil
	}
	return format, arguments
}

// methodImplementations maps each interface method declared in the package that has a print or printf shape to the
// methods that implement it, maybe interface methods too, as the package's assignments to interface types show. The
// assignments are found by x/tools' satisfy, as the original finds them (proposal #70638 would give a better way).
func methodImplementations(pass *analysis.Pass) map[*types.Func]map[*types.Func]bool {
	implementations := map[*types.Func]map[*types.Func]bool{}
	var finder satisfy.Finder
	finder.Find(pass.TypesInfo, pass.Files)
	for assignment := range finder.Result {
		// The assignment is LHS = RHS, where LHS is an interface type.
		assigned, isInterface := assignment.LHS.Underlying().(*types.Interface)
		if !isInterface {
			continue
		}
		for method := range assigned.Methods() {
			if method.Pkg() != pass.Pkg {
				continue // Only the package's own interface methods.
			}
			if _, arguments := formatArgumentsParameters(method.Signature()); arguments == nil {
				continue
			}
			// The lookup cannot fail, since RHS satisfies LHS.
			found, _, _ := types.LookupFieldOrMethod(assignment.RHS, false, pass.Pkg, method.Name())
			implementation, isFunction := found.(*types.Func)
			if !isFunction {
				continue
			}
			if implementations[method] == nil {
				implementations[method] = map[*types.Func]bool{}
			}
			implementations[method][implementation] = true
		}
	}
	return implementations
}

// matchesParameter reports whether expression is an identifier naming parameter.
func matchesParameter(info *types.Info, expression ast.Expr, parameter *types.Var) bool {
	identifier, isIdentifier := expression.(*ast.Ident)
	return isIdentifier && info.ObjectOf(identifier) == parameter
}

// enclosingFile is the file cursor is in: x/tools' astutil.EnclosingFile.
func enclosingFile(cursor inspector.Cursor) *ast.File {
	for enclosing := range cursor.Enclosing((*ast.File)(nil)) {
		file, _ := enclosing.Node().(*ast.File)
		return file
	}
	return nil
}
