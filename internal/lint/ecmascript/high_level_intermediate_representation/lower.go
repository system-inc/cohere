// Lowering: TypeScript AST to HIR.
//
// This is a proving implementation, not a complete one. It covers the statement and expression
// forms the design had to be tested against - blocks, conditionals, all four loop forms, switch,
// try/catch/finally, labeled break and continue, destructuring, calls, member access, logical and
// conditional operators, template literals, and JSX - and lowers everything else to
// `UnsupportedNode`, which is a value with unknown effects rather than a failure.
//
// Upstream's equivalent is `build_hir.rs` at 6,807 lines and it is the largest single chunk of work
// in any port of this compiler. Completing it is a later task; what is established here is that the
// data structures in this package can hold the result, which is the question that had to be
// answered before anything is built on top.
//
// # The builder, and why blocks are reserved before they are filled
//
// A terminal names its successors by id, and lowering discovers a construct's successors before it
// has lowered them: an `if` needs the ids of its consequent, alternate, and fallthrough at the
// moment it writes the terminal, and only one of those has been walked. So blocks are RESERVED -
// allocated with an id and no contents - then filled, then completed with a terminal. The pattern
// is upstream's and it is the reason the builder is a small state machine rather than a
// straightforward recursive descent.
//
// `currentBlock` is where instructions are appended right now. `terminate` closes it with a
// terminal and makes a named reserved block current.
//
// # Where lowering gives up, stated exhaustively
//
// This list is load-bearing rather than incidental. A lowering that abandons a function changes
// what every fixture below it is evidence about: a rule can only fire on code that reached it, so
// an abort condition silently redefines the corpus. Upstream's lowering aborts on `try`/`finally`
// with no `catch`, and a sibling porting `react/error-boundaries` lost real time to that, because
// the fixtures named for the rule never reached the validator at all and the fixture NAME gave no
// sign of it.
//
// So, completely, the places this lowering does not produce a faithful result:
//
//  1. `Lower` returns NIL for a node with no body - an overload signature, an ambient declaration,
//     an abstract method. Nothing was lost, because nothing runs. Callers must handle nil.
//
//  2. Everything else produces a function. There is NO condition under which lowering abandons a
//     function it started, and that is a deliberate divergence from upstream, which aborts on
//     several constructs. An unmodelled construct becomes `UnsupportedNode`: a value with unknown
//     effects, which is conservative rather than absent. A pass that must refuse such a function
//     checks for the variant and refuses explicitly, where the refusal is visible.
//
//  3. Constructs that currently reach `UnsupportedNode` rather than a faithful lowering, so a pass
//     reading them gets "something happened here" and no more:
//     class declarations and class expressions; `with`; `yield` and `yield*`; `import()`;
//     `new.target`; getters and setters as object members lower their body but the accessor
//     semantics are not modelled; decorators; and any statement or expression kind not named in the
//     switches in this file and lower_expression.go.
//
//  4. Known imprecision that is NOT an abort, listed because it will mislead a pass that assumes
//     otherwise:
//     - `var` is lowered as `let`. Function-scoped hoisting is not modelled, so a `var` used before
//     its declaration in a different block resolves as though it were block-scoped.
//     - A free identifier becomes `LoadGlobal` whether it is a true global, an import, or a
//     module-scope binding. `GlobalBindingKind`, Source and Imported now retain that distinction
//     when declarations are available. A CAPTURE is no longer in this list: see `captureOf`.
//     Note what this entry originally claimed and what was measured. It said resolution "needs the
//     checker", which was right, but `Lower` took no checker and `symbolOf` read `node.Symbol()` -
//     a field the binder writes only onto DECLARATION nodes, and only when a binder has run at
//     all. Parsing alone runs none. So the fallback caught not just free identifiers but EVERY
//     identifier: over 4,333 functions of real TypeScript, 0 `LoadLocal` instructions named a
//     source variable against 43,137 `LoadGlobal`. `Lower` now takes a checker, and this entry is
//     true again only for names that checker cannot resolve.
//     - `MaybeThrow` is not emitted. The `Try` terminal gives the handler an edge, but the
//     instruction-level "an exception can leave here" markers upstream places after each
//     throwable instruction in a try body are absent, so an effect pass cannot yet see the
//     partial state a mid-body throw leaves behind.
//     - Hoisting of `let`/`const` forward references, which upstream models with `DeclareContext`
//     for temporal-dead-zone correctness, is not modelled.
//
//  5. Instruction values and terminals that are DECLARED but that lowering never produces. They are
//     in the sets because a later pass needs them, and because adding a variant later invalidates
//     every existing exhaustive switch, so the shape is fixed now. A pass must not read the absence
//     of one as evidence about the source:
//     `Optional` (terminal) - an optional chain currently lowers to a `PropertyLoad` or
//     `ComputedLoad` with the `Optional` flag set, which records the optionality but NOT the
//     short-circuit control flow, so a pass asking "does this run on every path" gets the wrong
//     answer for `a?.b.c`; this is the likeliest of these to mislead.
//     `Sequence` (terminal) - a comma expression lowers both operands into one block, which is
//     correct for evaluation order and loses the construct.
//     `MaybeThrow` (terminal) - see above.
//     `DeclareContext` - hoisting of a binding a nested function captures is not modelled, so the
//     temporal dead zone is not visible. `LoadContext` and `StoreContext` ARE produced, for every
//     read and write of a binding an enclosing function declared.
//     `StartMemoize`, `FinishMemoize` - inserted by a later pass by design, never by lowering.
package high_level_intermediate_representation

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/property"
	"github.com/system-inc/cohere/static_single_assignment"
)

// Lower lowers one function to HIR.
//
// node must be a function-like node: a function declaration, function expression, arrow function,
// method, or accessor. Returns nil for anything else, and for a function with no body such as an
// overload signature or a declaration.
//
// # The checker is what makes variable references resolve, and it is not optional in practice
//
// typeChecker may be nil, and a nil one still produces a well-formed graph. What it does NOT
// produce is a graph where a variable reference names the binding it reads: every such reference
// falls back to `LoadGlobal`, carrying a name rather than a value. That fallback is correct for a
// true global and wrong for everything else.
//
// The distinction is invisible in the graph's shape, which is why it is stated here rather than
// left to be discovered. Measured over 4,333 functions of real TypeScript with a nil checker: 0
// `LoadLocal` instructions named a source variable, 43,137 variable reads became `LoadGlobal`, and
// single-assignment construction over that mints zero phis while passing every structural test.
//
// So a pass that reasons about VALUES - single-assignment form, effect inference, anything asking
// what a binding holds - must pass a real checker. A pass that only walks control flow need not.
// See `lowerIdentifier` for the resolution itself.
func Lower(node *ast.Node, typeChecker *checker.Checker) *Function {
	return lowerNested(node, typeChecker, nil)
}

// lowerNested lowers a function that may sit inside another one being lowered.
//
// `enclosing` is the builder for the immediately surrounding function, or nil for a function
// lowered on its own. It is the entire mechanism by which captures are found: see `captureOf`.
//
// The parameter is threaded rather than made a package-level stack because lowering is re-entrant -
// a rule may lower one function while another lowering is in progress - and a stack would make two
// unrelated lowerings each other's enclosing scope.
func lowerNested(node *ast.Node, typeChecker *checker.Checker, enclosing *builder) *Function {
	body := functionBody(node)
	if body == nil {
		return nil
	}

	name := functionName(node)
	function := NewFunction(node, name, classifyFunction(name))
	function.IsAsync = hasModifier(node, ast.KindAsyncKeyword)
	function.IsGenerator = functionIsGenerator(node)

	builder := &builder{
		function:     function,
		typeChecker:  typeChecker,
		enclosing:    enclosing,
		declarations: map[*ast.Symbol]static_single_assignment.DeclarationId{},
		identifiers:  map[*ast.Symbol]static_single_assignment.IdentifierId{},
		captured:     map[*ast.Symbol]static_single_assignment.IdentifierId{},
		contextual:   findContextIdentifiers(node, typeChecker),
	}

	entry := function.NewBlock(BlockKindBlock)
	function.Entry = entry.Id
	builder.currentBlock = entry

	// The returned value is one identifier every `return` stores into, so a pass asking what this
	// function yields has one thing to ask about.
	function.Returns = builder.newTemporary(nil)

	builder.lowerParams(node)

	if body.Kind == ast.KindBlock {
		builder.lowerStatements(body.AsBlock().Statements)
		builder.terminateWith(&Return{Value: function.Returns})
	} else {
		// A concise arrow body: `x => expr` returns the expression.
		value := builder.lowerExpressionToPlace(body)
		builder.terminateWith(&Return{Value: value})
	}

	Finalize(function)
	if enclosing != nil {
		enclosing.lastNested = builder
	}
	return function
}

// builder carries the state one function's lowering needs.
type builder struct {
	function     *Function
	currentBlock *BasicBlock

	// typeChecker resolves a reference identifier to the binding it names. Nil is tolerated and
	// degrades every variable reference to LoadGlobal; see Lower.
	typeChecker *checker.Checker

	// enclosing is the builder for the function this one is nested inside, or nil at the top.
	//
	// It exists so a free identifier can be asked of the enclosing scopes before being declared a
	// global. See `captureOf`.
	enclosing *builder

	// declarations maps a source symbol to the binding it names, so two references to the same
	// variable resolve to one DeclarationId. Symbol identity is the checker's answer to scoping,
	// which is why this does not implement its own scope tree.
	declarations map[*ast.Symbol]static_single_assignment.DeclarationId
	// identifiers maps a symbol to the value currently held by that binding.
	identifiers map[*ast.Symbol]static_single_assignment.IdentifierId

	// lastNested is the builder of the most recently lowered nested function.
	//
	// It exists because `lowerNested` returns a `*Function`, which does not carry the symbol->value
	// map needed to pair captures across the boundary. Handing the builder back through the parent
	// avoids widening the public return type of `Lower` for a detail no caller outside this package
	// can use. Read and cleared immediately by `lowerNestedFunction`.
	lastNested *builder

	// captured maps an enclosing function's symbol to the value THIS function names it by.
	//
	// A capture gets a fresh identifier in this function rather than reusing the enclosing one,
	// because the two functions have separate identifier tables and a Place is only meaningful
	// against the table of the function holding it. The entry is memoised so that two reads of one
	// captured binding are the same value here, which is what makes single-assignment form over a
	// nested function meaningful.
	captured map[*ast.Symbol]static_single_assignment.IdentifierId

	// contextual is the set of bindings this function shares with the closures inside it, decided
	// syntactically before lowering. See `context_identifiers.go` for why the question cannot be
	// asked while lowering and what it costs to answer it late.
	contextual contextIdentifiers

	// jumps is the stack of enclosing constructs a break or continue can target.
	jumps []jumpTarget

	// handlers is the stack of enclosing catch blocks, innermost last.
	//
	// Maintained but NOT YET READ. It is the input `MaybeThrow` needs: emitting one requires knowing
	// which handler a throw from the current position would reach, which is exactly the top of this
	// stack. It is kept correct now rather than added later because the maintenance is threaded
	// through try lowering, and retrofitting a stack into a recursive lowering is where off-by-one
	// scoping bugs come from. `go vet` does not flag a written-and-unread struct field, so this
	// comment is the only thing that stops the next reader deleting it as dead.
	handlers []static_single_assignment.BlockId
}

// jumpTarget is one enclosing construct a break or continue can name.
type jumpTarget struct {
	// label is the source label, empty for an unlabeled construct.
	label string
	// breakBlock is where `break` goes.
	breakBlock static_single_assignment.BlockId
	// continueBlock is where `continue` goes. InvalidBlock for a construct that cannot be continued,
	// such as a switch or a labeled block.
	continueBlock static_single_assignment.BlockId
}

// ---------------------------------------------------------------------------
// Block management
// ---------------------------------------------------------------------------

// reserve allocates a block that will be filled later.
func (builder *builder) reserve(kind BlockKind) *BasicBlock {
	return builder.function.NewBlock(kind)
}

// terminateWith closes the current block with a terminal.
//
// The block that follows is left unset; the caller must call `enter` before lowering anything else.
// A caller that forgets gets a nil-pointer panic at the next append rather than silent misplacement,
// which is the failure mode worth having.
func (builder *builder) terminateWith(terminal Terminal) {
	if builder.currentBlock == nil {
		return
	}
	builder.currentBlock.Terminal = terminal
	builder.currentBlock = nil
}

// enter makes a reserved block current.
func (builder *builder) enter(block *BasicBlock) {
	builder.currentBlock = block
}

// terminateAndEnter closes the current block and makes next current.
func (builder *builder) terminateAndEnter(terminal Terminal, next *BasicBlock) {
	builder.terminateWith(terminal)
	builder.enter(next)
}

// gotoBlock closes the current block with an unconditional jump.
func (builder *builder) gotoBlock(target static_single_assignment.BlockId, variant GotoVariant) {
	builder.terminateWith(&Goto{Block: target, Variant: variant})
}

// ensureBlock guarantees there is a current block to append to.
//
// Code after an abrupt exit is lowered into a block nothing reaches rather than dropped, matching
// what `controlflow` does. ReversePostorder removes it afterwards.
func (builder *builder) ensureBlock() *BasicBlock {
	if builder.currentBlock == nil {
		builder.currentBlock = builder.reserve(BlockKindBlock)
	}
	return builder.currentBlock
}

// ---------------------------------------------------------------------------
// Values
// ---------------------------------------------------------------------------

// newTemporary mints an unnamed value.
func (builder *builder) newTemporary(node *ast.Node) Place {
	identifier := builder.function.NewIdentifier("", node, 0)
	return Place{Identifier: identifier.Id, Range: rangeOf(node)}
}

// newTemporaryUnder mints a value sharing an existing value's DECLARATION.
//
// Construction merges two writes only when they name one binding, keyed on the declaration rather
// than the identifier (`ssa.go:316`). Two arms of a logical each writing a fresh temporary give a
// join unrelated declarations and nothing to merge; both writing the SAME identifier give it one
// definition and nothing to version. Distinct identifiers under one declaration are what make them
// versions of one thing.
//
// Upstream gets this structurally: its `LogicalExpression` case stores into a single `place` in both
// arms and construction versions that one binding.
func (builder *builder) newTemporaryUnder(node *ast.Node, shared static_single_assignment.DeclarationId) Place {
	identifier := builder.function.NewIdentifier("", node, shared)
	return Place{Identifier: identifier.Id, Range: rangeOf(node)}
}

// declarationOf returns the binding a symbol names, or zero on first sight.
//
// Zero tells `NewIdentifier` to derive the declaration from the identifier id, which is upstream's
// rule for every value it mints: `makeTemporaryIdentifier` and `HIRBuilder`'s named-binding path
// both write `declarationId: makeDeclarationId(id)`. One space, one source, so a temporary and a
// binding can never be handed the same number.
//
// A separate counter lived here and collided with that space. Named bindings counted 1, 2, 3 while
// temporaries took `identifierId + 1`, so binding number n shared a declaration with the temporary
// at index n-1 -- four collisions in the first fourteen values of a five-line fixture. The damage
// was silent and downstream: `dependencyCollector.declare` is first-writer-wins, matching upstream,
// so whichever value reached a declaration first claimed it and the real binding was dropped with
// the losing scope stack. A binding whose store sat inside a scope then registered no declaration
// for it, and `PruneUnusedScopes` removed the scope for declaring nothing.
func (builder *builder) declarationOf(symbol *ast.Symbol) static_single_assignment.DeclarationId {
	if symbol == nil {
		return 0
	}
	if declaration, ok := builder.declarations[symbol]; ok {
		return declaration
	}
	return 0
}

// lowerNestedFunction lowers a function nested in this one and pairs up what it captured.
//
// The second result is this function's view of the captures, in the same order as the nested
// function's `Context`. Index i of the two slices name the same source binding seen from the two
// sides of the boundary: `Captures[i]` is a value in THIS function's identifier table,
// `nested.Context[i]` is a value in the nested one's. That pairing is the edge a pass walks to
// follow a value into a closure, and it is why the order is fixed rather than incidental.
//
// A capture the nested function found in a grandparent rather than in this function is re-captured
// here first, so the chain is complete at every level: an inner arrow reading a variable two
// functions up produces a capture in the middle function too, and the middle function's own
// `Context` grows to match. Without that the pairing would name an identifier that does not exist
// in this function's table.
func (builder *builder) lowerNestedFunction(node *ast.Node) (*Function, []Place) {
	nested := lowerNested(node, builder.typeChecker, builder)
	if nested == nil {
		return nil, nil
	}

	nestedBuilder := builder.lastNested
	builder.lastNested = nil
	if nestedBuilder == nil {
		return nested, nil
	}

	symbols := nestedBuilder.capturedSymbolsInOrder()
	captures := make([]Place, 0, len(symbols))
	for _, symbol := range symbols {
		if symbol == nil {
			captures = append(captures, Place{})
			continue
		}
		// The value THIS function names the binding by: a local if this function declared it, or a
		// capture of its own if an enclosing one did.
		if identifier, ok := builder.identifiers[symbol]; ok {
			captures = append(captures, Place{Identifier: identifier})
			continue
		}
		if place, ok := builder.captureOf(symbol); ok {
			captures = append(captures, place)
			continue
		}
		captures = append(captures, Place{})
	}
	return nested, captures
}

// captureOf reports whether a symbol names a binding declared in an ENCLOSING function, and if so
// returns the value this function refers to it by.
//
// # Why this replaces a scope walk rather than implementing one
//
// Upstream computes captures with a dedicated AST pass, `FindContextIdentifiers`, which maintains
// its own scope stack and function stack and is 519 lines in the Rust port. That pass exists because
// Babel and oxc hand it scopes and it must decide, from scope ancestry, whether a reference crosses
// a function boundary.
//
// cohere does not need any of it, and the reason was measured rather than assumed. The resident
// checker returns the SAME `*ast.Symbol` pointer for a declaration and for every reference to it,
// including references inside a nested function: for `const C = ...; const r = () => <C/>` it
// answers one distinct symbol across both occurrences. Symbol identity therefore already encodes
// "same binding" across the boundary, and the enclosing builder's `identifiers` map already records
// which symbols that function declared. Asking the enclosing builder is the whole algorithm.
//
// This is the same trade the package comment makes for type inference: where upstream infers what a
// checker would know, cohere asks the checker. `TestCaptureSymbolIdentityCrossesFunctions` pins the
// property this rests on, so a checker change that broke it would fail loudly here rather than
// quietly reverting every capture to a global.
//
// # What counts as a capture, and what deliberately does not
//
// Only a binding an enclosing LOWERED function declared. A module-scope `const`, an import, and a
// true global are all absent from every enclosing builder's map and stay `LoadGlobal`, which is the
// answer the `globals` rule already depends on. The walk goes outward through every enclosing
// builder, so a binding captured through two function boundaries is found at the depth that
// declared it.
func (builder *builder) captureOf(symbol *ast.Symbol) (Place, bool) {
	if symbol == nil || builder.enclosing == nil {
		return Place{}, false
	}
	if identifier, ok := builder.captured[symbol]; ok {
		return Place{Identifier: identifier}, true
	}

	// Find the enclosing function that declared this binding. Nothing about the value it holds
	// there is copied: only the fact that it is a local of some enclosing function.
	declaredOutside := false
	for scope := builder.enclosing; scope != nil; scope = scope.enclosing {
		if _, ok := scope.identifiers[symbol]; ok {
			declaredOutside = true
			break
		}
	}
	if !declaredOutside {
		return Place{}, false
	}

	// A fresh identifier in THIS function's table, grouped under a declaration of its own. The
	// declaration is local because DeclarationIds are per-function too, and a pass correlating a
	// capture with the outer binding does so through `FunctionExpression.Captures`, which pairs the
	// two places explicitly.
	name := ""
	if symbol.Name != "" {
		name = symbol.Name
	}
	identifier := builder.function.NewIdentifier(name, nil, 0)
	builder.captured[symbol] = identifier.Id
	builder.function.Context = append(builder.function.Context, Place{Identifier: identifier.Id})
	return Place{Identifier: identifier.Id}, true
}

// capturedSymbolsInOrder returns the symbols this function captured, in the order first seen.
//
// Order is first-encounter rather than map order, because `Function.Context` is built in the same
// order and the two must correspond index for index: `FunctionExpression.Captures[i]` is the
// enclosing function's value for `nested.Context[i]`. A Go map cannot supply that, which is the
// same reason `BasicBlock.Predecessors` is a slice.
func (builder *builder) capturedSymbolsInOrder() []*ast.Symbol {
	ordered := make([]*ast.Symbol, len(builder.function.Context))
	for symbol, identifier := range builder.captured {
		for index, place := range builder.function.Context {
			if place.Identifier == identifier {
				ordered[index] = symbol
				break
			}
		}
	}
	return ordered
}

// bind creates a new value for a named binding and records it as that binding's current value.
func (builder *builder) bind(name string, symbol *ast.Symbol, node *ast.Node) Place {
	declaration := builder.declarationOf(symbol)
	identifier := builder.function.NewIdentifier(name, node, declaration)
	if symbol != nil {
		// The first value a symbol takes names the binding for every value after it, which is what
		// makes `let x = 1; x = 2` one declaration across two identifiers.
		if declaration == 0 {
			builder.declarations[symbol] = identifier.Declaration
		}
		builder.identifiers[symbol] = identifier.Id
	}
	return Place{Identifier: identifier.Id, Range: rangeOf(node)}
}

// emit appends an instruction storing into a fresh temporary, and returns that temporary.
func (builder *builder) emit(value InstructionValue, node *ast.Node) Place {
	lvalue := builder.newTemporary(node)
	builder.emitTo(lvalue, value, node)
	return lvalue
}

// emitTo appends an instruction storing into a given place.
func (builder *builder) emitTo(lvalue Place, value InstructionValue, node *ast.Node) {
	block := builder.ensureBlock()
	builder.function.AddInstruction(block, &Instruction{
		LValue: lvalue,
		Value:  value,
		Node:   node,
		Range:  rangeOf(node),
	})
}

// ---------------------------------------------------------------------------
// Parameters
// ---------------------------------------------------------------------------

func (builder *builder) lowerParams(node *ast.Node) {
	parameters := functionParameters(node)
	if parameters == nil {
		return
	}
	for _, parameter := range parameters.Nodes {
		declaration := parameter.AsParameterDeclaration()
		name := declaration.Name()
		if name == nil {
			continue
		}
		// A destructured parameter becomes a temporary plus a Destructure in the entry block, so
		// Function.Params stays flat and a pass reading it never walks a pattern.
		if name.Kind == ast.KindIdentifier {
			place := builder.bind(name.Text(), builder.symbolOf(name), name)
			builder.function.Params = append(builder.function.Params, place)
			continue
		}
		temporary := builder.newTemporary(name)
		builder.function.Params = append(builder.function.Params, temporary)
		pattern := builder.lowerPattern(name)
		builder.emit(&Destructure{LValue: pattern, Value: temporary, Kind: InstructionKindLet, Pattern: pattern}, name)
	}
}

// ---------------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------------

func (builder *builder) lowerStatements(statements *ast.NodeList) {
	if statements == nil {
		return
	}
	for _, statement := range statements.Nodes {
		builder.lowerStatement(statement, "")
	}
}

// lowerStatement lowers one statement.
//
// label is the label attached to this statement by an enclosing LabeledStatement, empty otherwise.
// It is threaded rather than looked up because a labeled loop must push its jump target with the
// label attached, and the loop is what knows its own continue block.
func (builder *builder) lowerStatement(node *ast.Node, label string) {
	if node == nil {
		return
	}
	switch node.Kind {
	case ast.KindBlock:
		builder.lowerStatements(node.AsBlock().Statements)

	case ast.KindEmptyStatement:
		// Nothing.

	case ast.KindDebuggerStatement:
		builder.emit(&Debugger{}, node)

	case ast.KindExpressionStatement:
		builder.lowerExpressionToPlace(node.AsExpressionStatement().Expression)

	case ast.KindVariableStatement:
		builder.lowerVariableDeclarationList(node.AsVariableStatement().DeclarationList)

	case ast.KindIfStatement:
		builder.lowerIfStatement(node)

	case ast.KindReturnStatement:
		builder.lowerReturnStatement(node)

	case ast.KindThrowStatement:
		value := builder.lowerExpressionToPlace(node.AsThrowStatement().Expression)
		builder.terminateWith(&Throw{Value: value})

	case ast.KindWhileStatement:
		builder.lowerWhileStatement(node, label)

	case ast.KindDoStatement:
		builder.lowerDoStatement(node, label)

	case ast.KindForStatement:
		builder.lowerForStatement(node, label)

	case ast.KindForOfStatement:
		builder.lowerForOfStatement(node, label)

	case ast.KindForInStatement:
		builder.lowerForInStatement(node, label)

	case ast.KindSwitchStatement:
		builder.lowerSwitchStatement(node, label)

	case ast.KindTryStatement:
		builder.lowerTryStatement(node)

	case ast.KindBreakStatement:
		builder.lowerBreakStatement(node)

	case ast.KindContinueStatement:
		builder.lowerContinueStatement(node)

	case ast.KindLabeledStatement:
		builder.lowerLabeledStatement(node)

	case ast.KindFunctionDeclaration:
		builder.lowerFunctionDeclaration(node)

	case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
		// Types are erased. Nothing runs.

	default:
		builder.emit(&UnsupportedNode{Node: node, Reason: node.Kind.String()}, node)
	}
}

func (builder *builder) lowerReturnStatement(node *ast.Node) {
	statement := node.AsReturnStatement()
	value := builder.function.Returns
	if statement.Expression != nil {
		result := builder.lowerExpressionToPlace(statement.Expression)
		builder.emitTo(builder.function.Returns, &LoadLocal{Place: result}, node)
		value = builder.function.Returns
	} else {
		builder.emitTo(builder.function.Returns, &Primitive{Value: nil}, node)
	}
	builder.terminateWith(&Return{Value: value})
}

func (builder *builder) lowerVariableDeclarationList(node *ast.Node) {
	if node == nil {
		return
	}
	list := node.AsVariableDeclarationList()
	kind := InstructionKindLet
	if list.Flags&ast.NodeFlagsConst != 0 {
		kind = InstructionKindConst
	}
	for _, declaration := range list.Declarations.Nodes {
		builder.lowerVariableDeclaration(declaration, kind)
	}
}

func (builder *builder) lowerVariableDeclaration(node *ast.Node, kind InstructionKind) {
	declaration := node.AsVariableDeclaration()
	name := declaration.Name()
	if name == nil {
		return
	}

	if declaration.Initializer == nil {
		if name.Kind == ast.KindIdentifier {
			place := builder.bind(name.Text(), builder.symbolOf(name), name)
			builder.emit(&DeclareLocal{LValue: place, Kind: kind}, node)
		}
		return
	}

	value := builder.lowerExpressionToPlace(declaration.Initializer)
	if name.Kind == ast.KindIdentifier {
		place := builder.bind(name.Text(), builder.symbolOf(name), name)
		builder.emit(&StoreLocal{LValue: place, Value: value, Kind: kind}, node)
		return
	}
	pattern := builder.lowerPattern(name)
	builder.emit(&Destructure{LValue: pattern, Value: value, Kind: kind, Pattern: pattern}, node)
}

func (builder *builder) lowerIfStatement(node *ast.Node) {
	statement := node.AsIfStatement()
	test := builder.lowerExpressionToPlace(statement.Expression)

	consequent := builder.reserve(BlockKindBlock)
	fallthroughBlock := builder.reserve(BlockKindBlock)
	alternate := fallthroughBlock
	if statement.ElseStatement != nil {
		alternate = builder.reserve(BlockKindBlock)
	}

	builder.terminateWith(&If{
		Test:        test,
		Consequent:  consequent.Id,
		Alternate:   alternate.Id,
		Fallthrough: fallthroughBlock.Id,
	})

	builder.enter(consequent)
	builder.lowerStatement(statement.ThenStatement, "")
	builder.gotoBlock(fallthroughBlock.Id, GotoVariantBreak)

	if statement.ElseStatement != nil {
		builder.enter(alternate)
		builder.lowerStatement(statement.ElseStatement, "")
		builder.gotoBlock(fallthroughBlock.Id, GotoVariantBreak)
	}

	builder.enter(fallthroughBlock)
}

// ---------------------------------------------------------------------------
// Loops
// ---------------------------------------------------------------------------

func (builder *builder) lowerWhileStatement(node *ast.Node, label string) {
	statement := node.AsWhileStatement()

	test := builder.reserve(BlockKindBlock)
	loop := builder.reserve(BlockKindLoop)
	fallthroughBlock := builder.reserve(BlockKindBlock)

	builder.terminateAndEnter(&While{
		Test:        test.Id,
		Loop:        loop.Id,
		Fallthrough: fallthroughBlock.Id,
	}, test)

	testValue := builder.lowerExpressionToPlace(statement.Expression)
	builder.terminateWith(&Branch{
		Test:        testValue,
		Consequent:  loop.Id,
		Alternate:   fallthroughBlock.Id,
		Fallthrough: fallthroughBlock.Id,
	})

	builder.enter(loop)
	builder.pushJump(label, fallthroughBlock.Id, test.Id)
	builder.lowerStatement(statement.Statement, "")
	builder.popJump()
	builder.gotoBlock(test.Id, GotoVariantContinue)

	builder.enter(fallthroughBlock)
}

func (builder *builder) lowerDoStatement(node *ast.Node, label string) {
	statement := node.AsDoStatement()

	loop := builder.reserve(BlockKindLoop)
	test := builder.reserve(BlockKindBlock)
	fallthroughBlock := builder.reserve(BlockKindBlock)

	builder.terminateAndEnter(&DoWhile{
		Loop:        loop.Id,
		Test:        test.Id,
		Fallthrough: fallthroughBlock.Id,
	}, loop)

	// A `continue` in a do-while restarts at the test, not at the body.
	builder.pushJump(label, fallthroughBlock.Id, test.Id)
	builder.lowerStatement(statement.Statement, "")
	builder.popJump()
	builder.gotoBlock(test.Id, GotoVariantContinue)

	builder.enter(test)
	testValue := builder.lowerExpressionToPlace(statement.Expression)
	builder.terminateWith(&Branch{
		Test:        testValue,
		Consequent:  loop.Id,
		Alternate:   fallthroughBlock.Id,
		Fallthrough: fallthroughBlock.Id,
	})

	builder.enter(fallthroughBlock)
}

func (builder *builder) lowerForStatement(node *ast.Node, label string) {
	statement := node.AsForStatement()

	init := builder.reserve(BlockKindBlock)
	test := builder.reserve(BlockKindBlock)
	loop := builder.reserve(BlockKindLoop)
	fallthroughBlock := builder.reserve(BlockKindBlock)
	update := InvalidBlock
	var updateBlock *BasicBlock
	if statement.Incrementor != nil {
		updateBlock = builder.reserve(BlockKindBlock)
		update = updateBlock.Id
	}

	builder.terminateAndEnter(&For{
		Init:        init.Id,
		Test:        test.Id,
		Update:      update,
		Loop:        loop.Id,
		Fallthrough: fallthroughBlock.Id,
	}, init)

	if statement.Initializer != nil {
		if statement.Initializer.Kind == ast.KindVariableDeclarationList {
			builder.lowerVariableDeclarationList(statement.Initializer)
		} else {
			builder.lowerExpressionToPlace(statement.Initializer)
		}
	}
	builder.gotoBlock(test.Id, GotoVariantBreak)

	builder.enter(test)
	if statement.Condition != nil {
		testValue := builder.lowerExpressionToPlace(statement.Condition)
		builder.terminateWith(&Branch{
			Test:        testValue,
			Consequent:  loop.Id,
			Alternate:   fallthroughBlock.Id,
			Fallthrough: fallthroughBlock.Id,
		})
	} else {
		// `for (;;)` never leaves through the test.
		builder.gotoBlock(loop.Id, GotoVariantBreak)
	}

	builder.enter(loop)
	continueTarget := test.Id
	if updateBlock != nil {
		continueTarget = updateBlock.Id
	}
	builder.pushJump(label, fallthroughBlock.Id, continueTarget)
	builder.lowerStatement(statement.Statement, "")
	builder.popJump()
	builder.gotoBlock(continueTarget, GotoVariantContinue)

	if updateBlock != nil {
		builder.enter(updateBlock)
		builder.lowerExpressionToPlace(statement.Incrementor)
		builder.gotoBlock(test.Id, GotoVariantBreak)
	}

	builder.enter(fallthroughBlock)
}

func (builder *builder) lowerForOfStatement(node *ast.Node, label string) {
	statement := node.AsForInOrOfStatement()

	init := builder.reserve(BlockKindLoop)
	test := builder.reserve(BlockKindLoop)
	loop := builder.reserve(BlockKindLoop)
	fallthroughBlock := builder.reserve(BlockKindBlock)

	builder.terminateAndEnter(&ForOf{
		Init:        init.Id,
		Test:        test.Id,
		Loop:        loop.Id,
		Fallthrough: fallthroughBlock.Id,
	}, init)

	// The iterator protocol is instructions, not terminal payload, so an effect pass can see that
	// iterating mutates the iterator.
	collection := builder.lowerExpressionToPlace(statement.Expression)
	iterator := builder.emit(&GetIterator{Value: collection}, statement.Expression)
	builder.gotoBlock(test.Id, GotoVariantBreak)

	builder.enter(test)
	next := builder.emit(&IteratorNext{Iterator: iterator, Collection: collection}, node)
	builder.terminateWith(&Branch{
		Test:        next,
		Consequent:  loop.Id,
		Alternate:   fallthroughBlock.Id,
		Fallthrough: fallthroughBlock.Id,
	})

	builder.enter(loop)
	builder.lowerForBinding(statement.Initializer, next)
	builder.pushJump(label, fallthroughBlock.Id, test.Id)
	builder.lowerStatement(statement.Statement, "")
	builder.popJump()
	builder.gotoBlock(test.Id, GotoVariantContinue)

	builder.enter(fallthroughBlock)
}

func (builder *builder) lowerForInStatement(node *ast.Node, label string) {
	statement := node.AsForInOrOfStatement()

	init := builder.reserve(BlockKindBlock)
	loop := builder.reserve(BlockKindLoop)
	fallthroughBlock := builder.reserve(BlockKindBlock)

	builder.terminateAndEnter(&ForIn{
		Init:        init.Id,
		Loop:        loop.Id,
		Fallthrough: fallthroughBlock.Id,
	}, init)

	object := builder.lowerExpressionToPlace(statement.Expression)
	key := builder.emit(&NextPropertyOf{Value: object}, node)
	builder.terminateWith(&Branch{
		Test:        key,
		Consequent:  loop.Id,
		Alternate:   fallthroughBlock.Id,
		Fallthrough: fallthroughBlock.Id,
	})

	builder.enter(loop)
	builder.lowerForBinding(statement.Initializer, key)
	builder.pushJump(label, fallthroughBlock.Id, init.Id)
	builder.lowerStatement(statement.Statement, "")
	builder.popJump()
	builder.gotoBlock(init.Id, GotoVariantContinue)

	builder.enter(fallthroughBlock)
}

// lowerForBinding binds the loop variable of a for-of or for-in to the value produced per iteration.
func (builder *builder) lowerForBinding(initializer *ast.Node, value Place) {
	if initializer == nil {
		return
	}
	if initializer.Kind == ast.KindVariableDeclarationList {
		list := initializer.AsVariableDeclarationList()
		kind := InstructionKindLet
		if list.Flags&ast.NodeFlagsConst != 0 {
			kind = InstructionKindConst
		}
		for _, declaration := range list.Declarations.Nodes {
			name := declaration.AsVariableDeclaration().Name()
			if name == nil {
				continue
			}
			if name.Kind == ast.KindIdentifier {
				place := builder.bind(name.Text(), builder.symbolOf(name), name)
				builder.emit(&StoreLocal{LValue: place, Value: value, Kind: kind}, name)
				continue
			}
			pattern := builder.lowerPattern(name)
			builder.emit(&Destructure{LValue: pattern, Value: value, Kind: kind, Pattern: pattern}, name)
		}
		return
	}
	// `for (x of xs)` with an existing binding.
	builder.lowerAssignmentTarget(initializer, value, InstructionKindReassign)
}

// ---------------------------------------------------------------------------
// Switch
// ---------------------------------------------------------------------------

// lowerSwitchStatement lowers a switch.
//
// Fallthrough between cases is represented by each case block's own terminal: a case that runs off
// its end jumps to the next case block, and one that breaks jumps to the switch's fallthrough. That
// is why the case blocks are all reserved before any is filled.
func (builder *builder) lowerSwitchStatement(node *ast.Node, label string) {
	statement := node.AsSwitchStatement()
	test := builder.lowerExpressionToPlace(statement.Expression)

	clauses := statement.CaseBlock.AsCaseBlock().Clauses.Nodes
	fallthroughBlock := builder.reserve(BlockKindBlock)

	blocks := make([]*BasicBlock, len(clauses))
	for index := range clauses {
		blocks[index] = builder.reserve(BlockKindBlock)
	}

	cases := make([]SwitchCase, 0, len(clauses))
	for index, clause := range clauses {
		if clause.Kind == ast.KindDefaultClause {
			cases = append(cases, SwitchCase{Test: nil, Block: blocks[index].Id})
			continue
		}
		// A case test is an expression evaluated at dispatch. It is lowered into the block holding
		// the switch terminal, which is where it evaluates.
		caseTest := builder.lowerExpressionToPlace(clause.AsCaseOrDefaultClause().Expression)
		cases = append(cases, SwitchCase{Test: &caseTest, Block: blocks[index].Id})
	}

	builder.terminateWith(&Switch{
		Test:        test,
		Cases:       cases,
		Fallthrough: fallthroughBlock.Id,
	})

	builder.pushJump(label, fallthroughBlock.Id, InvalidBlock)
	for index, clause := range clauses {
		builder.enter(blocks[index])
		builder.lowerStatements(clause.AsCaseOrDefaultClause().Statements)

		// Running off the end of a case falls into the next one, or past the switch.
		next := fallthroughBlock.Id
		if index+1 < len(blocks) {
			next = blocks[index+1].Id
		}
		builder.gotoBlock(next, GotoVariantBreak)
	}
	builder.popJump()

	builder.enter(fallthroughBlock)
}

// ---------------------------------------------------------------------------
// Try
// ---------------------------------------------------------------------------

// lowerTryStatement lowers try/catch/finally.
//
// # The finally divergence
//
// The finally body is lowered ONCE and every path that must run it jumps to it. cohere's
// `controlflow` lays it out twice, once per completion path, and both copies carry the same source
// positions. See the Try terminal's comment for why this IR cannot do that: two layouts give a
// value two definitions that are not a merge, and single-assignment construction over that mints a
// phi where the source has no join.
//
// What this costs: with one layout, the value flowing out of a finally is a merge of the normal and
// abrupt paths, so a `return` inside a `try` and the fallthrough both reach the same block. A pass
// that must distinguish them needs the predecessor, which is exactly what Predecessors provides.
// This is the trade: the information is preserved as graph structure rather than as duplication.
//
// # try with no catch
//
// A bare `try...finally` still gets a handler edge, to a synthetic block that runs the finally and
// rethrows. That differs from `controlflow`, which gives a body that cannot throw no edge to its
// catch at all. The reason is that "cannot throw" is a judgement a later, more precise pass may
// revise, and a graph whose shape depends on it must be rebuilt when it changes.
func (builder *builder) lowerTryStatement(node *ast.Node) {
	statement := node.AsTryStatement()

	tryBlock := builder.reserve(BlockKindBlock)
	fallthroughBlock := builder.reserve(BlockKindBlock)

	hasCatch := statement.CatchClause != nil
	hasFinally := statement.FinallyBlock != nil

	var finallyBlock *BasicBlock
	if hasFinally {
		finallyBlock = builder.reserve(BlockKindBlock)
	}

	// Where the body's normal completion goes: the finally if there is one, else past the statement.
	normalExit := fallthroughBlock
	if hasFinally {
		normalExit = finallyBlock
	}

	handler := builder.reserve(BlockKindCatch)
	var handlerBinding *Place
	if hasCatch {
		catchClause := statement.CatchClause.AsCatchClause()
		if catchClause.VariableDeclaration != nil {
			name := catchClause.VariableDeclaration.AsVariableDeclaration().Name()
			if name != nil && name.Kind == ast.KindIdentifier {
				place := builder.bind(name.Text(), builder.symbolOf(name), name)
				handlerBinding = &place
			}
		}
	}

	builder.terminateWith(&Try{
		Block:          tryBlock.Id,
		HandlerBinding: handlerBinding,
		Handler:        handler.Id,
		Fallthrough:    fallthroughBlock.Id,
	})

	builder.enter(tryBlock)
	builder.handlers = append(builder.handlers, handler.Id)
	builder.lowerStatement(statement.TryBlock, "")
	builder.handlers = builder.handlers[:len(builder.handlers)-1]
	builder.gotoBlock(normalExit.Id, GotoVariantTry)

	builder.enter(handler)
	if hasCatch {
		catchClause := statement.CatchClause.AsCatchClause()
		if catchClause.VariableDeclaration != nil {
			name := catchClause.VariableDeclaration.AsVariableDeclaration().Name()
			if name != nil && name.Kind != ast.KindIdentifier && handlerBinding == nil {
				temporary := builder.newTemporary(name)
				pattern := builder.lowerPattern(name)
				builder.emit(&Destructure{
					LValue:  pattern,
					Value:   temporary,
					Kind:    InstructionKindCatch,
					Pattern: pattern,
				}, name)
			}
		}
		builder.lowerStatement(catchClause.Block, "")
		builder.gotoBlock(normalExit.Id, GotoVariantTry)
	} else {
		// try/finally with no catch: run the finally, then let the exception continue outward.
		if hasFinally {
			builder.gotoBlock(finallyBlock.Id, GotoVariantTry)
		} else {
			builder.terminateWith(&Unreachable{})
		}
	}

	if hasFinally {
		builder.enter(finallyBlock)
		builder.lowerStatement(statement.FinallyBlock, "")
		builder.gotoBlock(fallthroughBlock.Id, GotoVariantTry)
	}

	builder.enter(fallthroughBlock)
}

// ---------------------------------------------------------------------------
// Labels and jumps
// ---------------------------------------------------------------------------

func (builder *builder) pushJump(label string, breakBlock, continueBlock static_single_assignment.BlockId) {
	builder.jumps = append(builder.jumps, jumpTarget{
		label:         label,
		breakBlock:    breakBlock,
		continueBlock: continueBlock,
	})
}

func (builder *builder) popJump() {
	if len(builder.jumps) > 0 {
		builder.jumps = builder.jumps[:len(builder.jumps)-1]
	}
}

func (builder *builder) lookupBreak(label string) static_single_assignment.BlockId {
	for index := len(builder.jumps) - 1; index >= 0; index-- {
		jump := builder.jumps[index]
		if label == "" || jump.label == label {
			return jump.breakBlock
		}
	}
	return InvalidBlock
}

func (builder *builder) lookupContinue(label string) static_single_assignment.BlockId {
	for index := len(builder.jumps) - 1; index >= 0; index-- {
		jump := builder.jumps[index]
		if !HasBlock(jump.continueBlock) {
			// A switch or labeled block cannot be continued; keep looking outward.
			if label != "" && jump.label == label {
				return InvalidBlock
			}
			continue
		}
		if label == "" || jump.label == label {
			return jump.continueBlock
		}
	}
	return InvalidBlock
}

func (builder *builder) lowerBreakStatement(node *ast.Node) {
	label := ""
	if name := node.AsBreakStatement().Label; name != nil {
		label = name.Text()
	}
	target := builder.lookupBreak(label)
	if !HasBlock(target) {
		builder.terminateWith(&Unsupported{})
		return
	}
	builder.gotoBlock(target, GotoVariantBreak)
}

func (builder *builder) lowerContinueStatement(node *ast.Node) {
	label := ""
	if name := node.AsContinueStatement().Label; name != nil {
		label = name.Text()
	}
	target := builder.lookupContinue(label)
	if !HasBlock(target) {
		builder.terminateWith(&Unsupported{})
		return
	}
	builder.gotoBlock(target, GotoVariantContinue)
}

// lowerLabeledStatement lowers `label: statement`.
//
// A label on a loop is threaded into the loop, which pushes its own jump target carrying the label,
// so `continue label` finds the loop's continue block. A label on anything else gets a Label
// terminal, which only `break label` can target.
func (builder *builder) lowerLabeledStatement(node *ast.Node) {
	statement := node.AsLabeledStatement()
	label := statement.Label.Text()

	if isLoopStatement(statement.Statement) {
		builder.lowerStatement(statement.Statement, label)
		return
	}

	block := builder.reserve(BlockKindBlock)
	fallthroughBlock := builder.reserve(BlockKindBlock)
	builder.terminateAndEnter(&Label{Block: block.Id, Fallthrough: fallthroughBlock.Id}, block)

	builder.pushJump(label, fallthroughBlock.Id, InvalidBlock)
	builder.lowerStatement(statement.Statement, "")
	builder.popJump()
	builder.gotoBlock(fallthroughBlock.Id, GotoVariantBreak)

	builder.enter(fallthroughBlock)
}

func isLoopStatement(node *ast.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement,
		ast.KindWhileStatement, ast.KindDoStatement:
		return true
	}
	return false
}

func (builder *builder) lowerFunctionDeclaration(node *ast.Node) {
	nested, captures := builder.lowerNestedFunction(node)
	if nested == nil {
		return
	}
	id := FunctionId(len(builder.function.Functions))
	builder.function.Functions = append(builder.function.Functions, nested)

	name := functionName(node)
	value := builder.emit(&FunctionExpression{Function: id, Captures: captures}, node)
	if name != "" {
		var symbol *ast.Symbol
		if nameNode := node.Name(); nameNode != nil {
			symbol = builder.symbolOf(nameNode)
		}
		place := builder.bind(name, symbol, node)
		builder.emit(&StoreLocal{LValue: place, Value: value, Kind: InstructionKindHoistedFunction}, node)
	}
}

// ---------------------------------------------------------------------------
// Helpers over the AST shim
// ---------------------------------------------------------------------------

func rangeOf(node *ast.Node) core.TextRange {
	if node == nil {
		return core.TextRange{}
	}
	return core.NewTextRange(node.Pos(), node.End())
}

// symbolOf resolves a node to the symbol it names.
//
// # Why the checker is consulted rather than the node's own field
//
// `node.Symbol()` is the DECLARATION symbol: a field the binder writes onto a declaration node. A
// reference - the `y` in `y > 1` - is not a declaration and its field is nil, so resolving a
// reference through it always fails. Worse, the field is nil on declaration names too unless a
// binder has run over the file, and parsing alone does not run one.
//
// Both failures are silent: the caller sees nil and treats the name as a global.
//
// `GetSymbolAtLocation` answers for references and declarations alike, and returns the SAME symbol
// pointer for a binding and every reference to it, which is the identity `declarationOf` groups on.
// It distinguishes shadowed bindings of one name, verified rather than assumed: in
// `let y = 2; { let y = 99; }` it returns two distinct symbols, four occurrences against two.
func (builder *builder) symbolOf(node *ast.Node) *ast.Symbol {
	if node == nil {
		return nil
	}
	if builder.typeChecker != nil {
		// At the identifier in `{value}`, the generic accessor returns the symbol for the
		// object's `value` PROPERTY. The lowering is reading the expression side, whose symbol is
		// the local binding. TypeScript exposes that binding through a dedicated accessor.
		if parent := node.Parent; parent != nil &&
			parent.Kind == ast.KindShorthandPropertyAssignment && parent.Name() == node {
			if symbol := builder.typeChecker.GetShorthandAssignmentValueSymbol(parent); symbol != nil {
				return symbol
			}
		}
		if symbol := builder.typeChecker.GetSymbolAtLocation(node); symbol != nil {
			return symbol
		}
	}
	// No checker, or a name the checker cannot resolve - a true global, or an unresolved import.
	// The declaration field is the only remaining source and is correct when a binder has run.
	return node.Symbol()
}

func functionBody(node *ast.Node) *ast.Node {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindConstructor:
		return node.Body()
	}
	return nil
}

func functionParameters(node *ast.Node) *ast.NodeList {
	if node == nil {
		return nil
	}
	return node.ParameterList()
}

func functionName(node *ast.Node) string {
	if node == nil {
		return ""
	}
	if name := node.Name(); name != nil {
		// `Node.Text()` panics on a computed name rather than returning anything, so the kind is
		// checked before the read. A class member spelled `[Symbol.for('x')]() {}` reaches here
		// through `lowerNested`, and reading its text crashed the whole file: the linter recovers
		// per file, so one method named this way meant nothing in that file was checked by any rule.
		//
		// The empty string is the right answer rather than a fallback. This name feeds
		// `classifyFunction`, which asks whether it looks like a component or a hook, and a computed
		// name is neither: `Symbol.for('nodejs.util.inspect.custom')` is not `Foo` and not `useFoo`.
		// An anonymous function is already spelled this way here.
		text, known := property.Name(name, property.Named|property.Quoted|property.Templated|property.Numeric|property.Private)
		if !known {
			return ""
		}
		return text
	}
	// An anonymous function assigned to a variable takes that variable's name, which is what makes
	// `const Foo = () => ...` classify as a component.
	if parent := node.Parent; parent != nil && parent.Kind == ast.KindVariableDeclaration {
		if name := parent.AsVariableDeclaration().Name(); name != nil && name.Kind == ast.KindIdentifier {
			return name.Text()
		}
	}
	return ""
}

func functionIsGenerator(node *ast.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.KindFunctionDeclaration:
		return node.AsFunctionDeclaration().AsteriskToken != nil
	case ast.KindFunctionExpression:
		return node.AsFunctionExpression().AsteriskToken != nil
	case ast.KindMethodDeclaration:
		return node.AsMethodDeclaration().AsteriskToken != nil
	}
	return false
}

func hasModifier(node *ast.Node, kind ast.Kind) bool {
	if node == nil {
		return false
	}
	modifiers := node.Modifiers()
	if modifiers == nil {
		return false
	}
	for _, modifier := range modifiers.Nodes {
		if modifier.Kind == kind {
			return true
		}
	}
	return false
}

// classifyFunction guesses whether a name is a React component or hook.
//
// Syntactic, and recorded once here so every consumer is wrong in the same way. See
// Function.Kind.
func classifyFunction(name string) FunctionKind {
	if name == "" {
		return FunctionKindOther
	}
	first := rune(name[0])
	if first >= 'A' && first <= 'Z' {
		return FunctionKindComponent
	}
	if len(name) > 3 && name[:3] == "use" {
		next := rune(name[3])
		if (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9') {
			return FunctionKindHook
		}
	}
	return FunctionKindOther
}
