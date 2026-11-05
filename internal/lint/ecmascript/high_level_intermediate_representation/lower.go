// Lowering: TypeScript AST to `HIR`.
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
// moment it writes the terminal, and only one of those has been walked. So blocks are reserved -
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
// the fixtures named for the rule never reached the validator at all and the fixture name gave no
// sign of it.
//
// So, completely, the places this lowering does not produce a faithful result:
//
//  1. `Lower` returns nil for a node with no body - an overload signature, an ambient declaration,
//     an abstract method. Nothing was lost, because nothing runs. Callers must handle nil.
//
//  2. Everything else produces a function. There is no condition under which lowering abandons a
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
//     switches in this file and builder_lower_expression.go.
//
//  4. Known imprecision that is not an abort, listed because it will mislead a pass that assumes
//     otherwise:
//     - `var` is lowered as `let`. Function-scoped hoisting is not modelled, so a `var` used before
//     its declaration in a different block resolves as though it were block-scoped.
//     - A free identifier becomes `LoadGlobal` whether it is a true global, an import, or a
//     module-scope binding. `GlobalBindingKind`, Source and Imported now retain that distinction
//     when declarations are available. A capture is no longer in this list: see `captureOf`.
//     Note what this entry originally claimed and what was measured. It said resolution "needs the
//     checker", which was right, but `Lower` took no checker and `symbolOf` read `node.Symbol()` -
//     a field the binder writes only onto declaration nodes, and only when a binder has run at
//     all. Parsing alone runs none. So the fallback caught not just free identifiers but every
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
//  5. Instruction values and terminals that are declared but that lowering never produces. They are
//     in the sets because a later pass needs them, and because adding a variant later invalidates
//     every existing exhaustive switch, so the shape is fixed now. A pass must not read the absence
//     of one as evidence about the source:
//     `Optional` (terminal) - an optional chain currently lowers to a `PropertyLoad` or
//     `ComputedLoad` with the `Optional` flag set, which records the optionality but not the
//     short-circuit control flow, so a pass asking "does this run on every path" gets the wrong
//     answer for `a?.b.c`; this is the likeliest of these to mislead.
//     `Sequence` (terminal) - a comma expression lowers both operands into one block, which is
//     correct for evaluation order and loses the construct.
//     `MaybeThrow` (terminal) - see above.
//     `DeclareContext` - hoisting of a binding a nested function captures is not modelled, so the
//     temporal dead zone is not visible. `LoadContext` and `StoreContext` are produced, for every
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

// Lower lowers one function to `HIR`.
//
// node must be a function-like node: a function declaration, function expression, arrow function,
// method, or accessor. Returns nil for anything else, and for a function with no body such as an
// overload signature or a declaration.
//
// # The checker is what makes variable references resolve, and it is not optional in practice
//
// typeChecker may be nil, and a nil one still produces a well-formed graph. What it does not
// produce is a graph where a variable reference names the binding it reads: every such reference
// falls back to `LoadGlobal`, carrying a name rather than a value. That fallback is correct for a
// true global and wrong for everything else.
//
// The distinction is invisible in the graph's shape, which is why it is stated here rather than
// left to be discovered. Measured over 4,333 functions of real TypeScript with a nil checker: 0
// `LoadLocal` instructions named a source variable, 43,137 variable reads became `LoadGlobal`, and
// single-assignment construction over that mints zero phis while passing every structural test.
//
// So a pass that reasons about values - single-assignment form, effect inference, anything asking
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
//
// Mutates: enclosing its lastNested is set to this lowering's builder, which is how the enclosing one finds
// the nested function's captures once the call returns
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

	builder.lowerParameters(node)

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

// ---------------------------------------------------------------------------
// Values
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Parameters
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Loops
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Switch
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Try
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Labels and jumps
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Helpers over the AST shim
// ---------------------------------------------------------------------------

func rangeOf(node *ast.Node) core.TextRange {
	if node == nil {
		return core.TextRange{}
	}
	return core.NewTextRange(node.Pos(), node.End())
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
		text, known := property.Name(
			name,
			property.Named|property.Quoted|property.Templated|property.Numeric|property.Private,
		)
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
