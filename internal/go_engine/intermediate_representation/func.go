// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file implements the Function type.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"strings"
)

// Like ObjectOf, but panics instead of returning nil.
// Only valid during f's create and build phases.
func (function *Function) objectOf(id *ast.Ident) types.Object {
	if object := function.info.ObjectOf(id); object != nil {
		return object
	}
	panic(fmt.Sprintf("no types.Object for ast.Ident %s @ %s",
		id.Name, function.Program.FileSet.Position(id.Pos())))
}

// instrs returns an iterator that returns each reachable instruction of the SSA function.
func (function *Function) instructions() iter.Seq[Instruction] {
	return func(yield func(instruction Instruction) bool) {
		for _, block := range function.Blocks {
			for _, instr := range block.Instructions {
				if !yield(instr) {
					return
				}
			}
		}
	}
}

// Like TypeOf, but panics instead of returning nil.
// Only valid during f's create and build phases.
func (function *Function) typeOf(expr ast.Expr) types.Type {
	if T := function.info.TypeOf(expr); T != nil {
		return function.typ(T)
	}
	panic(fmt.Sprintf("no type for %T @ %s", expr, function.Program.FileSet.Position(expr.Pos())))
}

// typ is the locally instantiated type of T. go/ir substitutes type arguments here when it builds an
// instantiated body (its InstantiateGenerics mode); ours builds every generic body in generic form, so
// f.typ(T)==T always.
func (function *Function) typ(T types.Type) types.Type {
	return T
}

// If id is an Instance, returns info.Instances[id].Type.
// Otherwise returns f.typeOf(id).
func (function *Function) instanceType(id *ast.Ident) types.Type {
	if instance, ok := function.info.Instances[id]; ok {
		return instance.Type
	}
	return function.typeOf(id)
}

// selection returns a *selection corresponding to f.info.Selections[selector]
// with potential updates for type substitution.
func (function *Function) selection(selector *ast.SelectorExpr) *selection {
	sel := function.info.Selections[selector]
	if sel == nil {
		return nil
	}

	switch sel.Kind() {
	case types.MethodExpr, types.MethodVal:
		if recv := function.typ(sel.Recv()); recv != sel.Recv() {
			// recv changed during type substitution.
			goPackage := function.declaredPackage().Types
			object, index, indirect := types.LookupFieldOrMethod(recv, true, goPackage, sel.Obj().Name())

			// sig replaces sel.Type(). See (types.Selection).Typ() for details.
			sig := object.Type().(*types.Signature)
			sig = changeRecv(sig, newVariable(sig.Recv().Name(), recv))
			if sel.Kind() == types.MethodExpr {
				sig = recvAsFirstArgument(sig)
			}
			return &selection{
				kind:     sel.Kind(),
				recv:     recv,
				typ:      sig,
				object:   object,
				index:    index,
				indirect: indirect,
			}
		}
	}
	return toSelection(sel)
}

// Destinations associated with unlabelled for/switch/select stmts.
// We push/pop one of these as we enter/leave each construct and for
// each BranchStmt we scan for the innermost target of the right type.
type targets struct {
	tail         *targets // rest of stack
	_break       *BasicBlock
	_continue    *BasicBlock
	_fallthrough *BasicBlock
}

// Destinations associated with a labelled block.
// We populate these as labels are encountered in forward gotos or
// labelled statements.
// Forward gotos are resolved once it is known which statement they
// are associated with inside the Function.
type lblock struct {
	label     *types.Label // Label targeted by the blocks.
	resolved  bool         // _goto block encountered (back jump or resolved fwd jump)
	_goto     *BasicBlock
	_break    *BasicBlock
	_continue *BasicBlock
}

// label returns the symbol denoted by a label identifier.
//
// label should be a non-blank identifier (label.Name != "_").
func (function *Function) label(label *ast.Ident) *types.Label {
	return function.objectOf(label).(*types.Label)
}

// lblockOf returns the branch target associated with the
// specified label, creating it if needed.
func (function *Function) lblockOf(label *types.Label) *lblock {
	lb := function.lblocks[label]
	if lb == nil {
		lb = &lblock{
			label: label,
			_goto: function.newBasicBlock(label.Name()),
		}
		if function.lblocks == nil {
			function.lblocks = make(map[*types.Label]*lblock)
		}
		function.lblocks[label] = lb
	}
	return lb
}

// labelledBlock searches `function` for the block of the specified label.
//
// If `function` is a yield function, it additionally searches ancestor Functions
// corresponding to enclosing range-over-func statements within the
// same source function, so the returned block may belong to a different Function.
func labelledBlock(function *Function, label *types.Label, tok token.Token) *BasicBlock {
	if lb := function.lblocks[label]; lb != nil {
		var block *BasicBlock
		switch tok {
		case token.BREAK:
			block = lb._break
		case token.CONTINUE:
			block = lb._continue
		case token.GOTO:
			block = lb._goto
		}
		if block != nil {
			return block
		}
	}
	// Search ancestors if this is a yield function.
	if function.jump != nil {
		return labelledBlock(function.parent, label, tok)
	}
	return nil
}

// targetedBlock looks for the nearest block in f.targets
// (and `function`'s ancestors) that matches tok's type, and returns
// the block and function it was found in.
func targetedBlock(function *Function, tok token.Token) *BasicBlock {
	if function == nil {
		return nil
	}
	for targets := function.targets; targets != nil; targets = targets.tail {
		var block *BasicBlock
		switch tok {
		case token.BREAK:
			block = targets._break
		case token.CONTINUE:
			block = targets._continue
		case token.FALLTHROUGH:
			block = targets._fallthrough
		}
		if block != nil {
			return block
		}
	}
	// Search f's ancestors (in case f is a yield function).
	return targetedBlock(function.parent, tok)
}

// addResultVariable adds a result for a variable `variable` to f.results and `variable` to f.returnVars.
func (function *Function) addResultVariable(variable *types.Var, source ast.Node) {
	name := variable.Name()
	if name == "" {
		name = fmt.Sprintf("res.%d", len(function.results))
	}
	result := emitLocalVariable(function, variable, source)
	result.comment = name
	function.results = append(function.results, result)
	function.returnVariables = append(function.returnVariables, variable)
}

// addParamVariable adds a parameter to f.Parameters.
func (function *Function) addParamVariable(variable *types.Var, source ast.Node) *Parameter {
	name := variable.Name()
	if name == "" {
		name = fmt.Sprintf("arg%d", len(function.Parameters))
	}
	parameter := &Parameter{name: name, typ: function.typ(variable.Type()), parent: function}
	parameter.setSource(source)
	parameter.object = variable
	function.Parameters = append(function.Parameters, parameter)
	return parameter
}

// addSpilledParameter declares a parameter that is pre-spilled to the
// stack; the function body will load/store the spilled location.
// Subsequent lifting will eliminate spills where possible.
func (function *Function) addSpilledParameter(object *types.Var, source ast.Node) {
	parameter := function.addParamVariable(object, source)
	spill := emitLocalVariable(function, object, source)
	emitStore(function, spill, parameter, source)
}

// startBody initializes the function prior to generating IR code for its body.
// Precondition: f.Type() already set.
func (function *Function) startBody() {
	entry := function.newBasicBlock("entry")
	function.currentBlock = entry
	function.vars = make(map[*types.Var]Value) // needed for some synthetics, e.g. init
}

func (function *Function) blockset(index int) *BlockSet {
	bs := &function.blocksets[index]
	if len(bs.values) != len(function.Blocks) {
		if cap(bs.values) >= len(function.Blocks) {
			bs.values = bs.values[:len(function.Blocks)]
			bs.Clear()
		} else {
			bs.values = make([]bool, len(function.Blocks))
		}
	} else {
		bs.Clear()
	}
	return bs
}

// createSyntacticParameters populates f.Parameters and generates code (spills
// and named result locals) for all the parameters declared in the
// syntax.  In addition it populates the f.objects mapping.
//
// Preconditions:
// f.startBody() was called. f.info != nil.
// Postcondition:
// len(f.Parameters) == len(f.Signature.Params) + (f.Signature.Recv() ? 1 : 0)
func (function *Function) createSyntacticParameters(recv *ast.FieldList, functype *ast.FuncType) {
	// Receiver (at most one inner iteration).
	if recv != nil {
		for _, field := range recv.List {
			for _, name := range field.Names {
				function.addSpilledParameter(identVariable(function, name), name)
			}
			// Anonymous receiver?  No need to spill.
			if field.Names == nil {
				function.addParamVariable(function.Signature.Recv(), field)
			}
		}
	}

	// Parameters.
	if functype.Params != nil {
		n := len(function.Parameters) // 1 if has recv, 0 otherwise
		for _, field := range functype.Params.List {
			for _, name := range field.Names {
				function.addSpilledParameter(identVariable(function, name), name)
			}
			// Anonymous parameter?  No need to spill.
			if field.Names == nil {
				function.addParamVariable(function.Signature.Params().At(len(function.Parameters)-n), field)
			}
		}
	}

	// Results.
	if functype.Results != nil {
		for _, field := range functype.Results.List {
			// Implicit "var" decl of locals for named results.
			for _, name := range field.Names {
				variable := identVariable(function, name)
				function.addResultVariable(variable, name)
			}
			// Implicit "var" decl of local for an unnamed result.
			if field.Names == nil {
				variable := function.Signature.Results().At(len(function.results))
				function.addResultVariable(variable, field.Type)
			}
		}
	}
}

// createDeferStack initializes fn.deferstack to a local variable
// initialized to a ssa:deferstack() call.
func (function *Function) createDeferStack() {
	// Each syntactic function makes a call to ssa:deferstack,
	// which is spilled to a local. Unused ones are later removed.
	function.deferstack = newVariable("defer$stack", tDeferStack)
	call := &Call{Call: CallCommon{Value: vDeferStack}}
	call.setType(tDeferStack)
	deferstack := function.emit(call, nil)
	spill := emitLocalVariable(function, function.deferstack, nil)
	emitStore(function, spill, deferstack, nil)
}

func numberNodes(function *Function) {
	var base Id
	for _, block := range function.Blocks {
		for _, instr := range block.Instructions {
			if instr == nil {
				continue
			}
			base++
			instr.setId(base)
		}
	}
}

func updateOperandsReferrers(instr Instruction, ops []*Value) {
	for _, op := range ops {
		if value := *op; value != nil {
			if refs := (*op).Referrers(); refs != nil {
				if len(*refs) == 0 {
					// per median, each value has two referrers, so we can avoid one call into growslice
					//
					// Note: we experimented with allocating
					// sequential scratch space, but we
					// couldn't find a value that gave better
					// performance than making many individual
					// allocations
					*refs = make([]Instruction, 1, 2)
					(*refs)[0] = instr
				} else {
					*refs = append(*refs, instr)
				}
			}
		}
	}
}

// buildReferrers populates the def/use information in all non-nil
// Value.Referrers slice.
// Precondition: all such slices are initially empty.
func buildReferrers(function *Function) {
	var rands []*Value

	for _, block := range function.Blocks {
		for _, instr := range block.Instructions {
			rands = instr.Operands(rands[:0]) // recycle storage
			updateOperandsReferrers(instr, rands)
		}
	}
}

// finishBody() finalizes the contents of the function after IR code generation of its body.
//
// The function is not done being built until done() is called.
func (function *Function) finishBody() {
	function.currentBlock = nil
	function.lblocks = nil
	function.returnVariables = nil
	function.jump = nil
	function.source = nil
	function.exits = nil

	// Remove from f.Locals any Allocations that escape to the heap.
	j := 0
	for _, local := range function.Locals {
		if !local.Heap {
			function.Locals[j] = local
			j++
		}
	}
	// Nil out f.Locals[j:] to aid GC.
	for localIndex := j; localIndex < len(function.Locals); localIndex++ {
		function.Locals[localIndex] = nil
	}
	function.Locals = function.Locals[:j]

	optimizeBlocks(function)
	buildReferrers(function)
	buildDominatorTree(function)
	buildSccs(function)

	// Lifting goes here (#ee33njz). go/ir lifts in this spot, then simplifies constant composite values, unless
	// its NaiveForm mode is set. Ours is always NaiveForm until then: locals stay Allocation, Load and Store, and
	// what readers see matches go/ir built with NaiveForm. The expression map go/ir builds next from its
	// debug references comes with lifting too.

	// clear remaining builder state
	function.results = nil    // (used by lifting)
	function.deferstack = nil // (used by lifting)
	function.vars = nil       // (used by lifting)

	numberNodes(function)

	numberNodes(function)
}

// done marks the building of f's SSA body complete,
// along with any nested functions, and optionally prints them.
func (function *Function) done() {
	assert(function.parent == nil, "done called on an anonymous function")

	var visit func(*Function)
	visit = func(function *Function) {
		for _, anon := range function.AnonymousFunctions {
			visit(anon) // anon is done building before f.
		}

		function.uniq = 0    // done with uniq
		function.build = nil // function is built
	}
	visit(function)
}

// removeNilBlocks eliminates nils from f.Blocks and updates each
// BasicBlock.Index.  Use this after any pass that may delete blocks.
func (function *Function) removeNilBlocks() {
	j := 0
	for _, block := range function.Blocks {
		if block != nil {
			block.Index = j
			function.Blocks[j] = block
			j++
		}
	}
	// Nil out f.Blocks[j:] to aid GC.
	for blockIndex := j; blockIndex < len(function.Blocks); blockIndex++ {
		function.Blocks[blockIndex] = nil
	}
	function.Blocks = function.Blocks[:j]
}

// lookup returns the address of the named variable identified by obj
// that is local to function f or one of its enclosing functions.
// If escaping, the reference comes from a potentially escaping pointer
// expression and the referent must be heap-allocated.
// We assume the referent is a *Allocation or *Phi.
// (The only Phis at this stage are those created directly by go1.22 "for" loops.)
func (function *Function) lookup(object *types.Var, escaping bool) Value {
	if value, ok := function.vars[object]; ok {
		if escaping {
			switch typed := value.(type) {
			case *Allocation:
				typed.Heap = true
			case *Phi:
				for _, edge := range typed.Edges {
					if alloc, ok := edge.(*Allocation); ok {
						alloc.Heap = true
					}
				}
			}
		}
		return value // function-local var (address)
	}

	// Definition must be in an enclosing function;
	// plumb it through intervening closures.
	if function.parent == nil {
		panic("no ir.Value for " + object.String())
	}
	outer := function.parent.lookup(object, true) // escaping
	variable := &FreeVariable{
		name:   object.Name(),
		typ:    outer.Type(),
		outer:  outer,
		parent: function,
	}
	function.vars[object] = variable
	function.FreeVariables = append(function.FreeVariables, variable)
	return variable
}

// emit emits the specified instruction to function f.
func (function *Function) emit(instr Instruction, source ast.Node) Value {
	return function.currentBlock.emit(instr, source)
}

// RelativeString returns the full name of this function, qualified by
// package name, receiver type, etc.
//
// The specific formatting rules are not guaranteed and may change.
//
// Examples:
//
//	"math.IsNaN"                  // a package-level function
//	"(*bytes.Buffer).Bytes"       // a declared method or a wrapper
//	"(*bytes.Buffer).Bytes$thunk" // thunk (func wrapping method; receiver is param 0)
//	"(*bytes.Buffer).Bytes$bound" // bound (func wrapping method; receiver supplied by closure)
//	"main.main$1"                 // an anonymous function in main
//	"main.init#1"                 // a declared init function
//	"main.init"                   // the synthesized package initializer
//
// When these functions are referred to from within the same package
// (i.e. from == f.Package.Types), they are rendered without the package path.
// For example: "IsNaN", "(*Buffer).Bytes", etc.
//
// All non-synthetic functions have distinct package-qualified names.
// (But two methods may have the same name "(T).f" if one is a synthetic
// wrapper promoting a non-exported method "f" from another package; in
// that case, the strings are equal but the identifiers "f" are distinct.)
func (function *Function) RelativeString(from *types.Package) string {
	// Anonymous?
	if function.parent != nil {
		// An anonymous function's Name() looks like "parentName$1",
		// but its String() should include the type/package/etc.
		parent := function.parent.RelativeString(from)
		for index, anon := range function.parent.AnonymousFunctions {
			if anon == function {
				return fmt.Sprintf("%s$%d", parent, 1+index)
			}
		}

		return function.name // should never happen
	}

	// Method (declared or wrapper)?
	if recv := function.Signature.Recv(); recv != nil {
		return function.relativeMethod(from, recv.Type())
	}

	// Thunk?
	if function.method != nil {
		return function.relativeMethod(from, function.method.recv)
	}

	// Bound?
	if len(function.FreeVariables) == 1 && strings.HasSuffix(function.name, "$bound") {
		return function.relativeMethod(from, function.FreeVariables[0].Type())
	}

	// Package-level function?
	// Prefix with package name for cross-package references only.
	if typesPackage := function.relativePackage(); typesPackage != nil && typesPackage != from {
		return fmt.Sprintf("%s.%s", typesPackage.Path(), function.name)
	}

	// Unknown.
	return function.name
}

func (function *Function) relativeMethod(from *types.Package, recv types.Type) string {
	return fmt.Sprintf("(%s).%s", relativeType(recv, from), function.name)
}

// declaredPackage returns the package fn is declared in or nil if the
// function is not declared in a package.
func (function *Function) declaredPackage() *Package {
	switch {
	case function.Package != nil:
		return function.Package // non-generic function  (does that follow??)
	case function.topLevelOrigin != nil:
		return function.topLevelOrigin.Package // instance of a named generic function
	case function.parent != nil:
		return function.parent.declaredPackage() // instance of an anonymous [generic] function
	default:
		return nil // function is not declared in a package, e.g. a wrapper.
	}
}

// relPkg returns types.Package fn is printed in relationship to.
func (function *Function) relativePackage() *types.Package {
	if p := function.declaredPackage(); p != nil {
		return p.Types
	}
	return nil
}

// newBasicBlock adds to f a new basic block and returns it.  It does
// not automatically become the current block for subsequent calls to emit.
// comment is an optional string for more readable debugging output.
func (function *Function) newBasicBlock(comment string) *BasicBlock {
	block := &BasicBlock{
		Index:   len(function.Blocks),
		comment: comment,
		parent:  function,
	}
	block.Successors = block.successorsSpace[:0]
	function.Blocks = append(function.Blocks, block)
	return block
}

// NewFunction returns a new synthetic Function instance belonging to
// prog, with its name and signature fields set as specified.
//
// The caller is responsible for initializing the remaining fields of
// the function object, e.g. Package, Parameters, Blocks.
//
// It is practically impossible for clients to construct well-formed
// IR functions/packages/programs directly, so we assume this is the
// job of the Builder alone.  NewFunction exists to provide clients a
// little flexibility.  For example, analysis tools may wish to
// construct fake Functions for the root of the callgraph, a fake
// "reflect" package, etc.
//
// TODO(adonovan): think harder about the API here.
func (prog *Program) NewFunction(name string, sig *types.Signature, provenance string) *Function {
	return &Function{Program: prog, name: name, Signature: sig, Synthetic: provenance}
}

//lint:ignore U1000 we may make use of this for functions loaded from export data
type extentNode [2]token.Pos

func (node extentNode) Pos() token.Pos { return node[0] }
func (node extentNode) End() token.Pos { return node[1] }

// Syntax returns the function's syntax (*ast.Func{Decl,Lit})
// if it was produced from syntax or an *ast.RangeStmt if
// it is a range-over-func yield function.
func (function *Function) Syntax() ast.Node { return function.syntax }

// identVariable returns the variable defined by id.
func identVariable(function *Function, id *ast.Ident) *types.Var {
	return function.info.Defs[id].(*types.Var)
}

// unique returns a unique positive int within the source tree of `function`.
// The source tree of `function` includes all of `function`'s ancestors by parent and all
// of the AnonymousFunctions contained within these.
func unique(function *Function) int64 {
	function.uniq++
	return function.uniq
}

// exit is a change of control flow going from a range-over-func
// yield function to an ancestor function caused by a break, continue,
// goto, or return statement.
//
// There are 3 types of exits:
// * return from the source function (from ReturnStmt),
// * jump to a block (from break and continue statements [labelled/unlabelled]),
// * go to a label (from goto statements).
//
// As the builder does one pass over the ast, it is unclear whether
// a forward goto statement will leave a range-over-func body.
// The function being exited to is unresolved until the end
// of building the range-over-func body.
type exit struct {
	id     int64     // unique value for exit within from and to
	from   *Function // the function the exit starts from
	to     *Function // the function being exited to (nil if unresolved)
	source ast.Node

	block *BasicBlock  // basic block within to being jumped to.
	label *types.Label // forward label being jumped to via goto.
	// block == nil && label == nil => return
}

// storeVariable emits to function `function` code to store a value `value` to a *types.Var x.
func storeVariable(function *Function, x *types.Var, value Value, source ast.Node) {
	emitStore(function, function.lookup(x, true), value, source)
}

// labelExit creates a new exit to a yield fn to exit the function using a label.
func labelExit(function *Function, label *types.Label, source ast.Node) *exit {
	exit := &exit{
		id:     unique(function),
		from:   function,
		to:     nil,
		source: source,
		label:  label,
	}
	function.exits = append(function.exits, exit)
	return exit
}

// blockExit creates a new exit to a yield fn that jumps to a basic block.
func blockExit(function *Function, block *BasicBlock, source ast.Node) *exit {
	exit := &exit{
		id:     unique(function),
		from:   function,
		to:     block.parent,
		source: source,
		block:  block,
	}
	function.exits = append(function.exits, exit)
	return exit
}

// returnExit creates a new exit to a yield fn that returns to the source function.
func returnExit(function *Function, source ast.Node) *exit {
	exit := &exit{
		id:     unique(function),
		from:   function,
		to:     function.source,
		source: source,
	}
	function.exits = append(function.exits, exit)
	return exit
}
