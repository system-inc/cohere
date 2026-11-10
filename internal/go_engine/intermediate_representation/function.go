// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This package defines a high-level intermediate representation for
// Go programs using static single-assignment (SSA) form.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"iter"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unsafe"
)

type Id int

// A Member is a member of a Go package, implemented by *NamedConstant,
// *Global, *Function, or *Type; they are created by package-level
// const, var, func and type declarations respectively.
type Member interface {
	Name() string                         // declared name of the package member
	String() string                       // package-qualified name of the package member
	RelativeString(*types.Package) string // like String, but relative refs are unqualified
	Object() types.Object                 // typechecker's object for this member, if any
	Type() types.Type                     // type of the package member
	Token() token.Token                   // `token.VAR`, `token.FUNC`, `token.CONST` or `token.TYPE`
}

// A Value is an IR value that can be referenced by an instruction.
type Value interface {
	// Name returns the name of this value, and determines how
	// this Value appears when used as an operand of an
	// Instruction.
	//
	// This is the same as the source name for Parameters,
	// Builtins, Functions, FreeVariables, Globals.
	// For constants, it is a representation of the constant's value
	// and type.  For all other Values this is the name of the
	// virtual register defined by the instruction.
	//
	// The name of an IR Value is not semantically significant,
	// and may not even be unique within a function.
	Name() string

	// Type returns the type of this value.  Many instructions
	// (e.g. IndexAddress) change their behaviour depending on the
	// types of their operands.
	Type() types.Type

	// Parent returns the function to which this Value belongs.
	// It returns nil for named Functions, Builtin and Global.
	Parent() *Function

	// Referrers returns the list of instructions that have this
	// value as one of their operands; it may contain duplicates
	// if an instruction has a repeated operand.
	//
	// Referrers actually returns a pointer through which the
	// caller may perform mutations to the object's state.
	//
	// Referrers is currently only defined if Parent()!=nil,
	// i.e. for the function-local values FreeVariable, Parameter,
	// Functions (iff anonymous) and all value-defining instructions.
	// It returns nil for named Functions, Builtin and Global.
	//
	// Instruction.Operands contains the inverse of this relation.
	Referrers() *[]Instruction

	Operands(rands []*Value) []*Value // nil for non-Instructions

	// Source returns the AST node responsible for creating this
	// value. A single AST node may be responsible for more than one
	// value, and not all values have an associated AST node.
	//
	// Do not use this method to find a Value given an ast.Expr; use
	// ValueForExpr instead.
	Source() ast.Node

	// Pos returns Source().Pos() if Source is not nil, else it
	// returns token.NoPos.
	Pos() token.Pos
}

// An Instruction is an IR instruction that computes a new Value or
// has some effect.
//
// An Instruction that defines a value (e.g. BinaryOperation) also implements
// the Value interface; an Instruction that only has an effect (e.g. Store)
// does not.
type Instruction interface {
	setSource(ast.Node)
	setId(Id)

	Comment() string

	// Id returns the Id of this instruction. IDs are unique within a single
	// function and are densely numbered, but may contain gaps.
	// Globally, instructions are identified by their addresses. However,
	// IDs exist to facilitate efficient storage of mappings between
	// instructions and data when analysing functions.
	//
	// NB: IDs are allocated late in the IR construction process and
	// are not available to early stages of said process.
	Id() Id

	// Parent returns the function to which this instruction
	// belongs.
	Parent() *Function

	// Block returns the basic block to which this instruction
	// belongs.
	Block() *BasicBlock

	// setBlock sets the basic block to which this instruction belongs.
	setBlock(*BasicBlock)

	// Operands returns the operands of this instruction: the
	// set of Values it references.
	//
	// Specifically, it appends their addresses to rands, a
	// user-provided slice, and returns the resulting slice,
	// permitting avoidance of memory allocation.
	//
	// The operands are appended in undefined order, but the order
	// is consistent for a given Instruction; the addresses are
	// always non-nil but may point to a nil Value.  Clients may
	// store through the pointers, e.g. to effect a value
	// renaming.
	//
	// Value.Referrers is a subset of the inverse of this
	// relation.  (Referrers are not tracked for all types of
	// Values.)
	Operands(rands []*Value) []*Value

	Referrers() *[]Instruction // nil for non-Values

	// Source returns the AST node responsible for creating this
	// instruction. A single AST node may be responsible for more than
	// one instruction, and not all instructions have an associated
	// AST node.
	Source() ast.Node

	// Pos returns Source().Pos() if Source is not nil, else it
	// returns token.NoPos.
	Pos() token.Pos
}

// A Node is a node in the IR value graph.  Every concrete type that
// implements Node is also either a Value, an Instruction, or both.
//
// Node contains the methods common to Value and Instruction, plus the
// Operands and Referrers methods generalized to return nil for
// non-Instructions and non-Values, respectively.
//
// Node is provided to simplify IR graph algorithms.  Clients should
// use the more specific and informative Value or Instruction
// interfaces where appropriate.
type Node interface {
	// Common methods:
	Source() ast.Node
	Pos() token.Pos
	Parent() *Function

	// Partial methods:
	Operands(rands []*Value) []*Value // nil for non-Instructions
	Referrers() *[]Instruction        // nil for non-Values
}

// Function represents the parameters, results, and code of a function
// or method.
//
// If Blocks is nil, this indicates an external function for which no
// Go source code is available.  In this case, FreeVariables, Locals, and
// Parameters are nil too.  Clients performing whole-program analysis must
// handle external functions specially.
//
// Blocks contains the function's control-flow graph (CFG).
// Blocks[0] is the function entry point; block order is not otherwise
// semantically significant, though it may affect the readability of
// the disassembly.
// To iterate over the blocks in dominance order, use DominatorPreorder().
//
// Recover is an optional second entry point to which control resumes
// after a recovered panic.  The Recover block may contain only a return
// statement, preceded by a load of the function's named return
// parameters, if any.
//
// A nested function (Parent()!=nil) that refers to one or more
// lexically enclosing local variables ("free variables") has FreeVariables.
// Such functions cannot be called directly but require a
// value created by MakeClosure which, via its Bindings, supplies
// values for these parameters.
//
// If the function is a method (Signature.Recv() != nil) then the first
// element of Parameters is the receiver parameter.
//
// A Go package may declare many functions called "init".
// For each one, Object().Name() returns "init" but Name() returns
// "init#1", etc, in declaration order.
//
// Pos() returns the declaring ast.FuncLit.Type.Func or the position
// of the ast.FuncDecl.Name, if the function was explicit in the
// source. Synthetic wrappers, for which Synthetic != "", may share
// the same position as the function they wrap.
// Syntax.Pos() always returns the position of the declaring "func" token.
//
// When the operand of a range statement is an iterator function,
// the loop body is transformed into a synthetic anonymous function
// that is passed as the yield argument in a call to the iterator.
// In that case, Function.Source() is the ast.RangeStmt.
// and Function.Syntax is the ast.RangeStmt.
//
// Synthetic functions, for which Synthetic != "", are functions
// that do not appear in the source AST. These include:
//   - method wrappers,
//   - thunks,
//   - bound functions,
//   - empty functions built from loaded type information,
//   - yield functions created from range-over-func loops,
//   - package init functions, and
//   - instantiations of generic functions.
//
// Synthetic wrapper functions may share the same position
// as the function they wrap.
//
// Type() returns the function's Signature.
//
// A generic function is a function or method that has uninstantiated type
// parameters (TypeParameters() != nil). Consider a hypothetical generic
// method, (*Map[K,V]).Get. It may be instantiated with all
// non-parameterized types as (*Map[string,int]).Get or with
// parameterized types as (*Map[string,U]).Get, where U is a type parameter.
// In both instantiations, Origin() refers to the instantiated generic
// method, (*Map[K,V]).Get, TypeParameters() refers to the parameters [K,V] of
// the generic method. TypeArguments() refers to [string,U] or [string,int],
// respectively, and is nil in the generic method.
type Function struct {
	name      string
	object    *types.Func // symbol for declared function (nil for FuncLit or synthetic init)
	method    *selection  // info about provenance of synthetic methods; thunk => non-nil
	Signature *types.Signature
	position  token.Pos
	syntax    ast.Node

	// source information
	Synthetic string      // provenance of synthetic function; "" for true source functions
	info      *types.Info // type annotations (if syntax != nil)
	goversion string      // Go version of syntax (NB: init is special)

	parent  *Function // enclosing function if anon; nil if global
	Package *Package  // enclosing package; nil for shared funcs (wrappers and error.Error)
	Program *Program  // enclosing program

	buildshared *task // wait for a shared function to be done building (may be nil if <=1 builder ever needs to wait)

	// buildOnce runs the function's build exactly once, whichever builder or caller of Build reaches it first; the
	// others wait for it (function_build.go).
	buildOnce sync.Once

	// expressionValues is the expression map ValueForExpression reads, sorted by position, recorded only in a package
	// built with SetDebugMode.
	expressionValues []expressionValue

	// These fields are populated only when the function body is built:

	Parameters         []*Parameter    // function parameters; for methods, includes receiver
	FreeVariables      []*FreeVariable // free variables whose values must be supplied by closure
	Locals             []*Allocation   // frame-allocated variables of this function
	Blocks             []*BasicBlock   // basic blocks of the function; nil => external
	Recover            *BasicBlock     // optional; control transfers here after recovered panic
	AnonymousFunctions []*Function     // anonymous functions (from FuncLit, RangeStmt) directly beneath this one
	referrers          []Instruction   // referring instructions (iff Parent() != nil)
	// position of a nested function in parent's AnonymousFunctions.
	// fn.Parent()!=nil => fn.Parent().AnonymousFunctions[fn.anonIndex] == fn.
	anonIndex int32

	Sccs []*Scc // strongly connected components, a directed acyclic graph in reverse topological order

	// receiver type parameters of this function.
	// recvtypeparams.Len() > 0 => method on generic or instance of generic type
	recvtypeparams *types.TypeParamList
	// type arguments that instantiated recvtypeparams.
	// len(recvtypeargs) > 0 => method on instance of generic type
	recvtypeargs []types.Type
	// type parameters of this function.
	// typeparams.Len() > 0 => generic or instance of generic function or method
	typeparams *types.TypeParamList
	// type arguments that instantiated typeparams.
	// len(typeargs) > 0 => instance of generic function or method
	typeargs []types.Type
	// the origin function if this is an instance of a source function. nil if Parent()!=nil.
	topLevelOrigin *Function
	generic        *generic // instances of this function, if generic

	// The following fields are cleared after building.
	build        buildFunc            // algorithm to build function body (nil => built)
	currentBlock *BasicBlock          // where to emit code
	vars         map[*types.Var]Value // addresses of local variables
	results      []*Allocation        // result allocations of the current function
	// variables for a return statement. Either results or for range-over-func a parent's results
	returnVariables []*types.Var
	targets         *targets                 // linked stack of branch targets
	lblocks         map[*types.Label]*lblock // labelled blocks
	jump            *types.Var               // synthetic variable for the yield state (non-nil => range-over-func)
	deferstack      *types.Var               // synthetic variable holding enclosing ssa:deferstack()
	source          *Function                // nearest enclosing source function
	exits           []*exit                  // exits of the function that need to be resolved
	uniq            int64                    // source of unique ints within the source tree while building
}

type Scc struct {
	Index                    int
	Blocks                   []*BasicBlock
	Predecessors, Successors []*Scc
	reachable                big.Int
}

// Pure values ----------------------------------------

// Value-defining instructions  ----------------------------------------

// SelectState is a helper for Select.
// It represents one goal state and its corresponding communication.
type SelectState struct {
	Direction types.ChanDir // direction of case (SendOnly or RecvOnly)
	Channel   Value         // channel to use (for send or receive)
	Send      Value         // value to send (for send)
	Position  token.Pos     // position of `token.ARROW`
}

// Instructions executed for effect.  They do not yield a value. --------------------

// Embeddable mix-ins and helpers for common parts of other structs. -----------

// The CallInstruction interface, implemented by *Go, *Defer and *Call,
// exposes the common parts of function-calling instructions,
// yet provides a way back to the Value defined by *Call alone.
type CallInstruction interface {
	Instruction
	Common() *CallCommon // returns the common parts of the call
	Value() *Call
}

func (function *Function) Name() string       { return function.name }
func (function *Function) Type() types.Type   { return function.Signature }
func (function *Function) Token() token.Token { return token.FUNC }
func (function *Function) Object() types.Object {
	if function.object != nil {
		return types.Object(function.object)
	}
	return nil
}
func (function *Function) String() string    { return function.RelativeString(nil) }
func (function *Function) Parent() *Function { return function.parent }
func (function *Function) Referrers() *[]Instruction {
	if function.parent != nil {
		return &function.referrers
	}
	return nil
}

// Returns returns an iterator over all blocks containing return statements.
func (function *Function) Returns() iter.Seq[*BasicBlock] {
	return func(yield func(*BasicBlock) bool) {
		for _, block := range function.Blocks {
			if _, ok := block.Control().(*Return); ok {
				if !yield(block) {
					return
				}
			}
		}
	}
}

// Exits returns an iterator over all blocks that have no successors.
func (function *Function) Exits() iter.Seq[*BasicBlock] {
	return func(yield func(*BasicBlock) bool) {
		for _, block := range function.Blocks {
			if len(block.Successors) == 0 {
				if !yield(block) {
					return
				}
			}
		}
	}
}

// Nodes returns an iterator over the basic block indices.
func (function *Function) Nodes() iter.Seq[int] {
	return func(yield func(int) bool) {
		for index := range function.Blocks {
			if !yield(index) {
				return
			}
		}
	}
}

// NumNodes returns the number of basic blocks in the graph.
func (function *Function) NumNodes() int { return len(function.Blocks) }

// Out returns an iterator over the successor block indices of a given node.
func (function *Function) Out(node int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for _, succ := range function.Blocks[node].Successors {
			if !yield(succ.Index) {
				return
			}
		}
	}
}

// TypeParameters are the function's type parameters if generic or the
// type parameters that were instantiated if fn is an instantiation.
//
// Specifically, the resulting list behaves like:
//
//	func        f       // []
//	func        f[P]    // [P]
//	func (T)    m       // []
//	func (T)    m[P]    // [P]
//	func (T[P]) m       // [P]
//	func (T[P]) m[Q]    // [P (index=0), Q (index=0)]
//
// Note that receiver type parameters precede other type parameters.
// Also, type parameters may have the same index if they come from
// different source type parameter lists.
func (function *Function) TypeParameters() *types.TypeParamList {
	return consTypeParamLists(function.recvtypeparams, function.typeparams)
}

func consTypeParamLists(first, second *types.TypeParamList) *types.TypeParamList {
	if first.Len() == 0 {
		return second
	}
	if second.Len() == 0 {
		return first
	}

	tpars := make([]*types.TypeParam, first.Len()+second.Len())
	for index := range first.Len() {
		tpars[index] = first.At(index)
	}
	for index := range second.Len() {
		tpars[index+first.Len()] = second.At(index)
	}
	// This logic unsafely assumes (and asserts) that the layout of the
	// TypeParamList is identical to that of a slice of TypeParams. This
	// is a hack while we work on getting a constructor for TypeParamList
	// approved (see go.dev/issue/79603).
	listType := reflect.TypeFor[types.TypeParamList]()
	if listType.NumField() != 1 {
		// Invariant: go/types defines TypeParamList as a struct whose one field is a []*TypeParam, the layout
		// the conversion below relies on.
		panic("TypeParamList has unexpected fields")
	}
	if field := listType.Field(0); field.Offset != 0 || field.Type != reflect.TypeFor[[]*types.TypeParam]() {
		// Invariant: go/types defines TypeParamList as a struct whose one field is a []*TypeParam, the layout
		// the conversion below relies on.
		panic("TypeParamList field is not []*TypeParam")
	}
	// Unsafe: the checks above prove TypeParamList is a struct whose only field, at offset zero, is a
	// []*TypeParam, so a pointer to tpars points at a value with TypeParamList's layout.
	return (*types.TypeParamList)(unsafe.Pointer(&tpars))
}

// TypeArguments are the types that TypeParameters() were instantiated by to create fn
// from fn.Origin().
//
// Specifically, the resulting slice behaves like:
//
//	f                   // []
//	f[int]              // [int]
//	T.m                 // []
//	T.m[int]            // [int]
//	T[int].m            // [int]
//	T[int].m[uint]      // [int, uint]
//
// Note that receiver type arguments precede other type arguments.
func (function *Function) TypeArguments() []types.Type {
	return slices.Concat(function.recvtypeargs, function.typeargs)
}

// Origin returns the generic function from which fn was instantiated,
// or nil if fn is not an instantiation.
func (function *Function) Origin() *Function {
	if function.parent != nil && function.parent.hasTypeArguments() {
		// Nested functions are built at a different time than their instances.
		// Build declared package if not yet built. This is not an expected use
		// case, but is simple and robust.
		function.declaredPackage().Build()
	}
	return origin(function)
}

// hasTypeParameters returns whether fn has any type parameters
func (function *Function) hasTypeParameters() bool {
	return function.recvtypeparams.Len()+function.typeparams.Len() > 0
}

// hasTypeArguments returns whether fn has any type arguments
func (function *Function) hasTypeArguments() bool {
	return len(function.recvtypeargs)+len(function.typeargs) > 0
}

// subrtargs returns fn's receiver type parameters substituted with receiver type arguments
func (function *Function) subrtargs(method *types.Func) []types.Type {
	return receiverTypeArguments(method)
}

// subtargs returns fn's type parameters substituted with (possibly implied) type arguments
func (function *Function) subtargs(id *ast.Ident) []types.Type {
	return instanceArguments(function.info, id)
}

// targstr returns a comma-separated string of the types in targs
func targstr(targs []types.Type) string {
	var text strings.Builder
	if len(targs) > 0 {
		text.WriteString("[")
		for index := range targs {
			if index > 0 {
				text.WriteString(", ")
			}
			text.WriteString(targs[index].String())
		}
		text.WriteString("]")
	}
	return text.String()
}

// origin is the function that fn is an instantiation of. Returns nil if fn is
// not an instantiation.
//
// Precondition: fn and the origin function are done building.
func origin(function *Function) *Function {
	if function.parent != nil && function.parent.hasTypeArguments() {
		return origin(function.parent).AnonymousFunctions[function.anonIndex]
	}
	return function.topLevelOrigin
}

func (function *Function) Pos() token.Pos   { return function.position }
func (function *Function) Source() ast.Node { return function.syntax }

// Operands.

func (function *Function) Operands(rands []*Value) []*Value { return rands }

// This file implements the Function type.

// Like ObjectOf, but panics instead of returning nil.
// Only valid during f's create and build phases.
func (function *Function) objectOf(id *ast.Ident) types.Object {
	if object := function.info.ObjectOf(id); object != nil {
		return object
	}
	// Invariant: callers pass identifiers that declare or use an object in syntax go/types checked, and go/types
	// records the object of each such identifier in Defs or Uses.
	panic(fmt.Sprintf("no types.Object for ast.Ident %s @ %s",
		id.Name, function.Program.FileSet.Position(id.Pos())))
}

// Like TypeOf, but panics instead of returning nil.
// Only valid during f's create and build phases.
func (function *Function) typeOf(expr ast.Expr) types.Type {
	if resolvedType := function.info.TypeOf(expr); resolvedType != nil {
		return function.typ(resolvedType)
	}
	// Invariant: callers pass expressions of syntax go/types checked, and go/types records the type of each such
	// expression in Types, or for an identifier in Defs or Uses.
	panic(fmt.Sprintf("no type for %T @ %s", expr, function.Program.FileSet.Position(expr.Pos())))
}

// typ is the locally instantiated type of T. go/ir substitutes type arguments here when it builds an
// instantiated body (its InstantiateGenerics mode); ours builds every generic body in generic form, so
// f.typ(T)==T always.
func (function *Function) typ(declaredType types.Type) types.Type {
	return declaredType
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
			sig, isSignature := object.Type().(*types.Signature)
			if !isSignature {
				// Invariant: the selection is a method, so looking its name up on the receiver finds a *types.Func,
				// and go/types gives every *types.Func a *types.Signature type.
				panic(fmt.Sprintf("selection: method %s has type %s, not a signature", object, object.Type()))
			}
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
	object := function.objectOf(label)
	resolvedLabel, isLabel := object.(*types.Label)
	if !isLabel {
		// Invariant: callers pass the label of a labelled or branch statement, and go/types resolves each to a
		// *types.Label.
		panic(fmt.Sprintf("label: %s denotes %s, not a label", label.Name, object))
	}
	return resolvedLabel
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
		receiverCount := len(function.Parameters) // 1 if has recv, 0 otherwise
		for _, field := range functype.Params.List {
			for _, name := range field.Names {
				function.addSpilledParameter(identVariable(function, name), name)
			}
			// Anonymous parameter?  No need to spill.
			if field.Names == nil {
				function.addParamVariable(function.Signature.Params().At(len(function.Parameters)-receiverCount), field)
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
	keptCount := 0
	for _, local := range function.Locals {
		if !local.Heap {
			function.Locals[keptCount] = local
			keptCount++
		}
	}
	// Nil out f.Locals[j:] to aid GC.
	for localIndex := keptCount; localIndex < len(function.Locals); localIndex++ {
		function.Locals[localIndex] = nil
	}
	function.Locals = function.Locals[:keptCount]

	optimizeBlocks(function)
	buildReferrers(function)
	buildDominatorTree(function)
	buildSccs(function)

	// Lifting, where go/ir lifts (#ee33njz): through the shared core, in lifting.go.
	liftFunction(function)

	// The debug references become the expression map, as go/ir turns them into its exprToValue here.
	function.recordExpressions()

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
	keptCount := 0
	for _, block := range function.Blocks {
		if block != nil {
			block.Index = keptCount
			function.Blocks[keptCount] = block
			keptCount++
		}
	}
	// Nil out f.Blocks[j:] to aid GC.
	for blockIndex := keptCount; blockIndex < len(function.Blocks); blockIndex++ {
		function.Blocks[blockIndex] = nil
	}
	function.Blocks = function.Blocks[:keptCount]
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
		// Invariant: callers look up only function-local variables that go/types resolved in scope here, so one
		// missing from this function's vars is declared in an enclosing function, and this function has a parent.
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
	if irPackage := function.declaredPackage(); irPackage != nil {
		return irPackage.Types
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

// Syntax returns the function's syntax (*ast.Func{Decl,Lit})
// if it was produced from syntax or an *ast.RangeStmt if
// it is a range-over-func yield function.
func (function *Function) Syntax() ast.Node { return function.syntax }

// identVariable returns the variable defined by id.
func identVariable(function *Function, id *ast.Ident) *types.Var {
	object := function.info.Defs[id]
	variable, isVariable := object.(*types.Var)
	if !isVariable {
		// Invariant: callers pass identifiers that a receiver, parameter, result, var spec, short variable
		// declaration or range clause defines, and go/types records a *types.Var in Defs for each of those.
		panic(fmt.Sprintf("identVariable: %s defines %v, not a variable", id.Name, object))
	}
	return variable
}

// unique returns a unique positive int within the source tree of `function`.
// The source tree of `function` includes all of `function`'s ancestors by parent and all
// of the AnonymousFunctions contained within these.
//
// Mutates: function advances its unique counter
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
//
// Mutates: function gains the exit among those to resolve
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
//
// Mutates: function gains the exit among those to resolve
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
//
// Mutates: function gains the exit among those to resolve
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
