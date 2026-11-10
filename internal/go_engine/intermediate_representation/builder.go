// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines the builder, which builds SSA-form IR for function bodies.
//
// SSA construction has two phases, "create" and "build". First, one
// or more packages are created in any order by a sequence of calls to
// CreatePackage, either from syntax or from mere type information.
// Each created package has a complete set of Members (const, var,
// type, func) that can be accessed through methods like
// Program.FunctionValue. One created from type information creates
// each member as it is first accessed.
//
// It is not necessary to call CreatePackage for all dependencies of
// each syntax package, only for its direct imports. (In future
// perhaps even this restriction may be lifted.)
//
// Second, packages created from syntax are built, by one or more
// calls to Package.Build, which may be concurrent; or by a call to
// Program.Build, which builds all packages in parallel. Building
// traverses the type-annotated syntax tree of each function body and
// creates SSA-form IR, a control-flow graph of instructions,
// populating fields such as Function.Body, .Parameters, and others.
//
// Building may create additional methods, including:
// - wrapper methods (e.g. for embedding, or implicit &recv)
// - bound method closures (e.g. for use(recv.f))
// - thunks (e.g. for use(I.f) or use(T.f))
// - instantiation wrappers (e.g. f[int], which delegates to f[T]).
// As these methods are created, they are added to the build queue,
// and then processed in turn, until a fixed point is reached,
// Since these methods might belong to packages that were not
// created (by a call to CreatePackage), their Package field is unset.
//
// Instances of generic functions are always wrapped (f[int] delegates
// to f[T]), as go/ir does without its InstantiateGenerics mode: a
// generic body is built once, in generic form.
//
// Each package has an initializer function named "init" that calls
// the initializer functions of each direct import, computes and
// assigns the initial value of each global variable, and calls each
// source-level function named "init". (These generate SSA functions
// named "init#1", "init#2", etc.)
//
// Function literals
//
// Anonymous functions must be built as soon as they are encountered,
// as it may affect locals of the enclosing function, but they are not
// marked 'built' until the end of the outermost enclosing function.
// (Among other things, this causes them to be logged in top-down order.)
//
// The Function.build fields determines the algorithm for building the
// function body. It is cleared to mark that building is complete.

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"runtime"

	"honnef.co/go/tools/go/types/typeutil"
)

var (
	// Type constants.
	tBool       = types.Typ[types.Bool]
	tByte       = types.Typ[types.Byte]
	tRune       = types.Universe.Lookup("rune").Type() // prints as "rune" (Typ[Rune] is same as Int32)
	tInt        = types.Typ[types.Int]
	tInvalid    = types.Typ[types.Invalid]
	tString     = types.Typ[types.String]
	tUntypedNil = types.Typ[types.UntypedNil]
	tEface      = types.NewInterfaceType(nil, nil).Complete()
	tDeferStack = types.NewPointer(typeutil.NewDeferStack())

	varOk    = newVariable("ok", tBool)
	varIndex = newVariable("index", tInt)

	vOne      = intConstant(1, nil)
	vTrue     = NewConstant(constant.MakeBool(true), tBool, nil)
	vNoReturn = NewConstant(constant.MakeString("noreturn"), tString, nil)

	jReady        = intConstant(0, nil)  // range-over-func jump is ready
	jBusy         = intConstant(-1, nil) // range-over-func jump is busy
	jDone         = intConstant(-2, nil) // range-over-func jump is done
	jDroppedPanic = stringConstant("iterator call did not preserve panic", nil)
	jLateYield    = stringConstant("yield function called after range loop exit", nil)

	vDeferStack = &Builtin{
		name: "ssa:deferstack",
		sig:  types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(anonymousVariable(tDeferStack)), false),
	}
)

// builder holds state associated with the package currently being built.
// Its methods contain all the logic for AST-to-IR conversion.
//
// All Functions belong to the same Program.
//
// builders are not thread-safe.
type builder struct {
	fns []*Function // Functions that have finished their create phases.

	finished int // finished is the length of the prefix of fns containing built functions.

	// The task of building shared functions within the builder.
	// Shared functions are ones the builder may either create or lookup.
	// These may be built by other builders in parallel.
	// The task is done when the builder has finished iterating, and it
	// waits for all shared functions to finish building.
	// nil implies there are no hared functions to wait on.
	buildshared *task
}

// shared is done when the builder has built all of the
// enqueued functions to a fixed-point.
func (packageBuilder *builder) shared() *task {
	if packageBuilder.buildshared == nil { // lazily-initialize
		packageBuilder.buildshared = &task{done: make(chan unit)}
	}
	return packageBuilder.buildshared
}

// enqueue fn to be built by the builder.
func (packageBuilder *builder) enqueue(function *Function) {
	packageBuilder.fns = append(packageBuilder.fns, function)
}

// waitForSharedFunction indicates that the builder should wait until
// the potentially shared function fn has finished building.
//
// This should include any functions that may be built by other
// builders.
func (packageBuilder *builder) waitForSharedFunction(function *Function) {
	if function.buildshared != nil { // maybe need to wait?
		task := packageBuilder.shared()
		task.addEdge(function.buildshared)
	}
}

// A buildFunction is a strategy for building the SSA body for a function.
type buildFunction = func(*builder, *Function)

// iterate causes all created but unbuilt functions to be built. As
// this may create new methods, the process is iterated until it
// converges.
//
// Waits for any dependencies to finish building.
func (packageBuilder *builder) iterate() {
	for ; packageBuilder.finished < len(packageBuilder.fns); packageBuilder.finished++ {
		function := packageBuilder.fns[packageBuilder.finished]
		packageBuilder.buildFunction(function)
	}

	packageBuilder.buildshared.markDone()
	packageBuilder.buildshared.wait()
}

// buildFunction builds IR code for the body of function fn.  Idempotent: the build runs under the function's
// buildOnce, so a function a package build and a lazy Build reach together is built once, and the later caller
// waits for it.
func (packageBuilder *builder) buildFunction(function *Function) {
	function.buildOnce.Do(func() {
		if function.build != nil {
			assert(function.parent == nil, "anonymous functions should not be built by buildFunction()")

			function.build(packageBuilder, function)
			function.done()
			countBuild(function)
		}
	})
}

// buildParamsOnly builds fn.Parameters from fn.Signature, but does not build fn.Body.
func (packageBuilder *builder) buildParamsOnly(function *Function) {
	// For external (C, asm) functions or functions loaded from
	// export data, we must set fn.Parameters even though there is no
	// body code to reference them.
	if receiver := function.Signature.Recv(); receiver != nil {
		// The parameter has no syntax node, so it carries no position.
		function.addParamVariable(receiver, nil)
	}
	parameters := function.Signature.Params()
	for index, length := 0, parameters.Len(); index < length; index++ {
		// The parameter has no syntax node, so it carries no position.
		function.addParamVariable(parameters.At(index), nil)
	}
}

// buildFromSyntax builds fn.Body from fn.syntax, which must be non-nil.
//
// Mutates: function gains its body, built from its syntax
func (packageBuilder *builder) buildFromSyntax(function *Function) {
	var receiverField *ast.FieldList
	var body *ast.BlockStmt
	var functype *ast.FuncType
	switch syntax := function.syntax.(type) {
	case *ast.FuncDecl:
		functype = syntax.Type
		receiverField = syntax.Recv
		body = syntax.Body
		if body == nil {
			packageBuilder.buildParamsOnly(function) // no body (non-Go function)
			return
		}
	case *ast.FuncLit:
		functype = syntax.Type
		body = syntax.Body
	case nil:
		// Invariant: buildFromSyntax is the build strategy only of functions created with syntax, as createFunction
		// gives one without syntax buildParamsOnly.
		panic("no syntax")
	default:
		// Invariant: the syntax of a function built from syntax is the FuncDecl createFunction was given or the
		// FuncLit expression0 found.
		panic(syntax) // unexpected syntax
	}
	function.source = function
	function.startBody()
	function.createSyntacticParameters(receiverField, functype)
	function.createDeferStack()
	packageBuilder.statement(function, body)
	if cb := function.currentBlock; cb != nil &&
		(cb == function.Blocks[0] || cb == function.Recover || cb.Predecessors != nil) {
		// Control fell off the end of the function's body block.
		//
		// Block optimizations eliminate the current block, if
		// unreachable.  It is a builder invariant that
		// if this no-arg return is ill-typed for
		// fn.Signature.Results, this block must be
		// unreachable.  The sanity checker checks this.
		function.emit(new(RunDefers), nil)
		function.emit(new(Return), nil)
	}
	function.finishBody()
}

// buildYieldFunction builds the body of the yield function created
// from a range-over-func *ast.RangeStmt.
//
// Mutates: function gains its body, built from the range statement, and its source and targets
func (packageBuilder *builder) buildYieldFunction(function *Function) {
	// See builder.rangeFunc for detailed documentation on how fn is set up.
	//
	// In pseudo-Go this roughly builds:
	// func yield(_k tk, _v tv) bool {
	// 	   if jump != jReady { panic("yield function called after range loop exit") }
	//     jump = jBusy
	//     k, v = _k, _v // assign the iterator variable (if needed)
	//     ... // rng.Body
	//   continue:
	//     jump = jReady
	//     return true
	// }
	statement, isRange := function.syntax.(*ast.RangeStmt)
	if !isRange {
		// Invariant: buildYieldFunc is the build strategy only of yield functions, which rangeFunc creates with
		// their range statement as syntax.
		panic(fmt.Sprintf("yield function %s has syntax %T, not a range statement", function, function.syntax))
	}
	function.source = function.parent.source
	function.startBody()
	parameters := function.Signature.Params()
	for variable := range parameters.Variables() {
		function.addParamVariable(variable, nil)
	}

	// Initial targets
	ycont := function.newBasicBlock("yield-continue")
	// lblocks is either {} or is {label: nil} where label is the label of syntax.
	for label := range function.lblocks {
		function.lblocks[label] = &lblock{
			label:     label,
			resolved:  true,
			_goto:     ycont,
			_continue: ycont,
			// `break label` statement targets fn.parent.targets._break
		}
	}
	function.targets = &targets{
		tail:      function.targets,
		_continue: ycont,
		// `break` statement targets fn.parent.targets._break.
	}

	// continue:
	//   jump = jReady
	//   return true
	saved := function.currentBlock
	function.currentBlock = ycont
	storeVariable(function, function.jump, jReady, statement.Body)
	// A yield function's own deferstack is always empty, so rundefers is not needed.
	function.emit(&Return{Results: []Value{vTrue}}, nil)

	// Emit header:
	//
	//   if jump != jReady { panic("yield iterator accessed after exit") }
	//   jump = jBusy
	//   k, v = _k, _v
	function.currentBlock = saved
	yloop := function.newBasicBlock("yield-loop")
	invalid := function.newBasicBlock("yield-invalid")

	jumpValue := emitLoad(function, function.lookup(function.jump, true), nil)
	emitIf(function, emitCompare(function, token.EQL, jumpValue, jReady, nil), yloop, invalid, nil)
	function.currentBlock = invalid
	function.emit(
		&Panic{
			X: emitConv(function, jLateYield, tEface, nil),
		},
		nil,
	)

	function.currentBlock = yloop
	storeVariable(function, function.jump, jBusy, statement.Body)

	// Initialize k and v from params.
	var tk types.Type
	var tv types.Type
	if statement.Key != nil && !isBlankIdent(statement.Key) {
		tk = function.typeOf(statement.Key) // fn.parent.typeOf is identical
	}
	if statement.Value != nil && !isBlankIdent(statement.Value) {
		tv = function.typeOf(statement.Value)
	}
	if statement.Tok == token.DEFINE {
		if tk != nil {
			id, isIdent := statement.Key.(*ast.Ident)
			if !isIdent {
				// Invariant: go/types accepts only identifiers on the left of a range clause's :=.
				panic(fmt.Sprintf("range key %T is not an identifier", statement.Key))
			}
			emitLocalVariable(function, identVariable(function, id), statement.Key)
		}
		if tv != nil {
			id, isIdent := statement.Value.(*ast.Ident)
			if !isIdent {
				// Invariant: go/types accepts only identifiers on the left of a range clause's :=.
				panic(fmt.Sprintf("range value %T is not an identifier", statement.Value))
			}
			emitLocalVariable(function, identVariable(function, id), statement.Value)
		}
	}
	var key Value
	var value Value
	if len(function.Parameters) > 0 {
		key = function.Parameters[0]
	}
	if len(function.Parameters) > 1 {
		value = function.Parameters[1]
	}
	var kl lvalue
	var vl lvalue
	if tk != nil {
		kl = packageBuilder.address(function, statement.Key, false) // non-escaping
	}
	if tv != nil {
		vl = packageBuilder.address(function, statement.Value, false) // non-escaping
	}
	if tk != nil {
		kl.store(function, key, statement.Key)
	}
	if tv != nil {
		vl.store(function, value, statement.Value)
	}

	// Build the body of the range loop.
	packageBuilder.statement(function, statement.Body)
	if cb := function.currentBlock; cb != nil &&
		(cb == function.Blocks[0] || cb == function.Recover || cb.Predecessors != nil) {
		// Control fell off the end of the function's body block.
		// Block optimizations eliminate the current block, if
		// unreachable.
		emitJump(function, ycont, nil)
	}
	// pop the stack for the yield function
	function.targets = function.targets.tail

	// Clean up exits and promote any unresolved exits to fn.parent.
	for _, exit := range function.exits {
		if exit.label != nil {
			lb := function.lblocks[exit.label]
			if lb.resolved {
				// label was resolved. Do not turn lb into an exit.
				// e does not need to be handled by the parent.
				continue
			}

			// _goto becomes an exit.
			//   _goto:
			//     jump = id
			//     return false
			function.currentBlock = lb._goto
			id := intConstant(exit.id, exit.source)
			storeVariable(function, function.jump, id, exit.source)
			vFalse := NewConstant(constant.MakeBool(false), tBool, exit.source)
			function.emit(&Return{Results: []Value{vFalse}}, exit.source)
		}

		if exit.to != function { // e needs to be handled by the parent too.
			function.parent.exits = append(function.parent.exits, exit)
		}
	}

	function.finishBody()
}

// cpuLimit is a counting semaphore to limit CPU parallelism.
var cpuLimit = make(chan unit, runtime.GOMAXPROCS(0))

// buildPackageInit builds fn.Body for the synthetic package initializer.
//
// Mutates: init gains its body, and its version and info are set for each initializer and then cleared
func (packageBuilder *builder) buildPackageInit(init *Function) {
	irPackage := init.Package
	init.startBody()

	// Make init() skip if package is already initialized.
	initguard := irPackage.Variable("init$guard")
	doinit := init.newBasicBlock("init.start")
	done := init.newBasicBlock("init.done")
	emitIf(init, emitLoad(init, initguard, nil), done, doinit, nil)
	init.currentBlock = doinit
	emitStore(init, initguard, vTrue, nil)

	// Call the init() function of each package we import.
	for _, importsPackage := range irPackage.Types.Imports() {
		prereq := irPackage.Program.Package(importsPackage)
		if prereq == nil {
			// Invariant: Package.Build requires, as its doc says, that CreatePackage was called for every direct
			// import, so each has a package here.
			panic(fmt.Sprintf(
				"Package(%q).Build(): unsatisfied import: Program.CreatePackage(%q) was not called",
				irPackage.Types.Path(),
				importsPackage.Path(),
			))
		}
		var call Call
		call.Call.Value = prereq.init
		call.setType(types.NewTuple())
		init.emit(&call, nil)
	}

	// Initialize package-level vars in correct order.
	if len(irPackage.info.InitOrder) > 0 && len(irPackage.files) == 0 {
		// Invariant: CreatePackage takes info from type-checking the package's files, so it orders variable
		// initializers only when there are files.
		panic("no source files provided for package. cannot initialize globals")
	}

	for _, varinit := range irPackage.info.InitOrder {
		// Initializers for global vars are evaluated in dependency
		// order, but may come from arbitrary files of the package
		// with different versions, so we transiently update
		// init.goversion for each one. (Since init is a synthetic
		// function it has no syntax of its own that needs a version.)
		init.goversion = irPackage.initVersion[varinit.Rhs]
		if len(varinit.Lhs) == 1 {
			// 1:1 initialization: var x, y = a(), b()
			var lval lvalue
			if variable := varinit.Lhs[0]; variable.Name() != "_" {
				global, isGlobal := irPackage.values[variable].(*Global)
				if !isGlobal {
					// Invariant: CreatePackage records every package-level variable as a Global.
					panic(fmt.Sprintf(
						"package-level variable %s is %T, not a Global",
						variable.Name(),
						irPackage.values[variable],
					))
				}
				lval = &address{pointer: global}
			} else {
				lval = blank{}
			}
			// The assignment carries no position information.
			packageBuilder.assign(init, lval, varinit.Rhs, true, nil, nil)
		} else {
			// n:1 initialization: var x, y :=  f()
			tuple := packageBuilder.expressionN(init, varinit.Rhs)
			for index, variable := range varinit.Lhs {
				if variable.Name() == "_" {
					continue
				}
				global, isGlobal := irPackage.values[variable].(*Global)
				if !isGlobal {
					// Invariant: CreatePackage records every package-level variable as a Global.
					panic(fmt.Sprintf(
						"package-level variable %s is %T, not a Global",
						variable.Name(),
						irPackage.values[variable],
					))
				}
				emitStore(init, global, emitExtract(init, tuple, index, nil), nil)
			}
		}
	}

	// The rest of the init function is synthetic:
	// no syntax, info, goversion.
	init.info = nil
	init.goversion = ""

	// Call all of the declared init() functions in source order.
	for _, file := range irPackage.files {
		for _, decl := range file.Decls {
			if decl, ok := decl.(*ast.FuncDecl); ok {
				id := decl.Name
				if !isBlankIdent(id) && id.Name == "init" && decl.Recv == nil {
					declaredInit, isFunction := irPackage.values[irPackage.info.Defs[id]].(*Function)
					if !isFunction {
						// Invariant: CreatePackage records every package-level function as a Function.
						panic(fmt.Sprintf(
							"init function is %T, not a Function",
							irPackage.values[irPackage.info.Defs[id]],
						))
					}
					var call Call
					call.Call.Value = declaredInit
					call.setType(types.NewTuple())
					irPackage.init.emit(&call, nil)
				}
			}
		}
	}

	// Finish up init().
	emitJump(init, done, nil)
	init.currentBlock = done
	init.emit(new(Return), nil)
	init.finishBody()
}
