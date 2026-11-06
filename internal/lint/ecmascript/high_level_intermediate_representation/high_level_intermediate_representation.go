// Package hir is a high-level intermediate representation of one JavaScript or TypeScript
// function: a control-flow graph whose blocks hold three-address instructions over named values,
// rather than a tree of syntax.
//
// It answers questions the AST cannot answer without the asker reimplementing control flow. Which
// values reach this point. Whether this call happens on every path or only some. Whether the thing
// passed here is the same object that was mutated there. A rule written against the AST that wants
// any of those either approximates them or walks the tree a second time with its own notion of
// order, and cohere already holds three rules doing exactly that.
//
// # The decision this package makes, and why
//
// cohere already has a basic-block control-flow graph in `internal/lint/ecmascript/control_flow_graph`, with
// predecessor edges, a dominator tree, and a monotone dataflow solver. It also has a resident
// whole-program type checker. The obvious move was therefore a thin IR: keep pointing at AST nodes,
// lean on the existing graph for control flow, and add only the value naming on top. That would be
// less code and would duplicate nothing.
//
// It was rejected, and the reason is a property of the existing graph rather than a preference.
//
// `control_flow_graph.Build` is an event recorder, not a lowering. A consumer supplies `Hooks` and
// receives callbacks at reads, writes, statements, and expressions; it calls `Emit` to append its
// own event type to whatever block is current. The graph itself stores `Events []E` and
// `Successors []*Block[E]` and nothing else. There is no per-block record of which AST node was
// laid out where, no notion of an ordered value produced by an expression, and no way to ask the
// builder for the block a given subexpression landed in. A consumer sees the positions the walk
// passes through, in order, and must reconstruct meaning from that stream alone.
//
// That is sufficient for the questions its four consumers ask, all of which are of the form "did
// event A occur before event B on this path". It is not sufficient to name values. The specific
// obstruction: for `f(g(x), h(y))` the hooks fire at each identifier read, but nothing reports that
// `g(x)` produced a value, that the value is the first argument to `f`, or which of the two calls
// the current block belongs to when they fork. Recovering that means walking the expression tree
// alongside the graph and re-deriving evaluation order, which is lowering, performed by the
// consumer, without the builder's cooperation, once per consumer.
//
// The second obstruction is layout. `controlflow` deliberately reproduces ESLint's code-path
// shape, and two of its properties are wrong for an IR:
//
//   - A `finally` body is laid out twice, once for normal completion and once for the abrupt path.
//     Both copies carry the same source positions. An IR whose instructions are keyed by position
//     would give a value two definitions that are not a merge of each other, and single-assignment
//     construction over that produces a phi where the source has no join.
//   - A `try` whose body cannot throw gets no edge to its `catch`, and the first throwable node in
//     a body forks to the handler so later statements are bypassed on the throwing path. That is
//     the right answer for reachability and the wrong one for a mutation lattice, which needs the
//     handler to see the state the body actually left behind.
//
// So this package lowers independently, and the duplication is real and accepted. What it buys is
// that the two graphs answer different questions and neither is bent to serve the other:
// `controlflow` keeps mirroring ESLint for the rules whose observable behaviour depends on that,
// and this keeps mirroring React's `HIR` for the passes that will be ported onto it. A shared graph
// would have to be both, and the finally-duplication alone makes that impossible.
//
// # Why the shape mirrors React's compiler rather than being designed fresh
//
// The instruction and terminal sets here are upstream's, near name-for-name. That is a deliberate
// cost: some of it is redundant with what the type checker already knows, and a from-scratch design
// for cohere would be smaller.
//
// It is paid for one reason. React ships 325 error fixtures with plain-text expectations, vendored
// at `internal/react_conformance`, and they are the only external oracle this project has for
// whether a mid-end is correct. Every pass that will be built on this IR - single-assignment form,
// type inference, effect inference, the validators - exists upstream in a form that reads against
// this exact shape. Diverging on the IR means every ported pass becomes a rewrite whose correctness
// is judged by its author, and the fixtures stop discriminating. Matching means a pass can be
// ported and then scored, and a wrong answer is visible as a number.
//
// A measurable wrong answer beats an unmeasurable right one. That is the whole argument, and it is
// the reason to prefer fidelity here specifically, not everywhere.
//
// Where upstream's shape is a workaround for something cohere does not have, this diverges and says
// so at the divergence. The two standing cases:
//
//   - Upstream carries an `Environment` holding arenas for identifiers, scopes, functions, and
//     types, because its passes need shared mutable references that Rust will not give them.
//     `Function` here owns its own tables. There is no cross-function arena, because nothing in a
//     lint pipeline compiles two functions into one graph, and a package-level arena would make
//     every pass take a parameter it does not use.
//   - Upstream's `typeinference` crate exists to infer what cohere's checker already knows.
//     `Identifier.Node` is retained on every value that came from a syntactic source precisely so a
//     later pass can hand that node to the checker instead of inferring. That field is the seam;
//     see its comment.
//
// # What is React-specific, and where it lives
//
// Measured against upstream at the pinned reading: 43 instruction values of which 5 are React
// (`JsxExpression`, `JsxFragment`, `JsxText`, `StartMemoize`, `FinishMemoize`), and 22 terminals of
// which 2 are React (`Scope`, `PrunedScope`).
//
// All 5 instruction values are here, in the core set. Of the 2 terminals, `Scope` is now here and
// `PrunedScope` is not. The paragraphs below describe the state before reactive scopes were built
// and the rule that governed it; they are kept because the rule still governs `PrunedScope`, and
// because the condition they name is exactly the one that was met. See the end of this section.
//
// The split is not a compromise between them, and the asymmetry is the point.
//
// JSX is React-specific only in name. It is syntax the parser accepts, it appears in the middle of
// ordinary expressions, and lowering must produce something for it or refuse the function outright.
// An extension seam for it would mean either an `Extension` variant carrying an opaque payload -
// which every consuming pass must then handle blindly, defeating exhaustive matching, the main
// safety property of a closed instruction set - or refusing to lower JSX at all, which discards
// most of the corpus. Three variants for syntax the language grammar has is not contamination; it
// is the same category as `TaggedTemplateExpression`. `StartMemoize` and `FinishMemoize` are
// markers a later pass inserts and a later pass reads; they cost one variant each and carry no
// semantics that any other pass must understand.
//
// `Scope` and `PrunedScope` are genuinely different. They are not syntax and they are not markers -
// they are the output of React's reactive-scope construction, which is 10,980 lines upstream, the
// largest module in the compiler, and contributes nothing to the 15 rules cohere wants. A terminal
// is the block's control flow; adding two variants that no lowering ever produces and no generic
// pass can interpret would put a permanent hole in every `switch` over `Terminal` for a pass that
// may never be written. If reactive scopes are ever built, they add two variants then, and every
// exhaustive switch goes red and gets read by a human. That is the correct time to pay it.
//
// That moment arrived, for one of the two. Reactive scopes are built: `scopes.go` assigns them,
// `align_sweep_state.go` and `merge_scopes.go` bring them to the shape upstream's own
// `assertValidBlockNesting` demands, and `scope_terminals.go` rewrites the graph to carry them.
// `Scope` is therefore in the core set, added in the same commit as the pass that constructs it.
// Every exhaustive switch did go red and was read; two more had a `default` arm and were found by
// enumeration rather than by the compiler, which is the failure mode this paragraph did not
// anticipate and the next person adding a variant should expect. `PrunedScope` stays out under the
// unchanged rule: the four passes that construct it are not ported, so it would be a variant nothing
// produces -- the exact shape of `Optional`, which this package already carries as a warning.
//
// The rule this follows, stated so the next person can apply it rather than re-litigate it: a
// variant belongs in the core set when lowering must produce it, and belongs behind a seam when
// only a later pass would produce it and no other pass must interpret it. Lowering produces JSX
// because the grammar does. Nothing lowers to a reactive scope.
//
// # What this package deliberately does not do
//
// Single-assignment form is not what lowering produces, and is a separate opt-in pass. Straight out
// of `Lower`, values are named but not uniquely defined: a `let` reassigned in a loop is several
// `Identifier`s sharing one `DeclarationId`, each store mints a fresh one, and a read binds to
// whichever store the lowering walk saw last rather than to the one that reaches it on the path.
// That last part is the important one: the pre-SSA graph's reads are lexically correct and
// dataflow-wrong at any join, so a pass must not read them as reaching definitions.
//
// `Construct` in ssa.go is what makes them reaching definitions, and it also fills `Block.Phis`.
// A pass that reasons about values should run it; a pass that only walks control flow need not.
//
// No type inference, no effect inference, no optimisation, and no rule. `Place.Effect` is threaded
// through lowering and left `EffectUnknown`. The IR carries no type: a pass that wants one asks the
// checker through `Identifier.Node`.
package high_level_intermediate_representation

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/static_single_assignment"
)

// InstructionId indexes Function.Instructions.
//
// Instructions live in one flat table per function and blocks hold ids into it. Upstream's reason
// for the indirection applies here unchanged: a pass that needs to remember where an instruction
// was, across a mutation that reorders blocks, needs one copyable value to hold. A `(BlockId, int)`
// pair does not survive an insertion into that block.
type InstructionId uint32

// FunctionId indexes Function.Functions, the nested functions lowered within this one.
//
// A function expression lowers to an instruction holding an id, not an inline Function. Passes that
// walk nested functions therefore recurse through the table rather than through the instruction
// value, and a pass that does not care about nesting never touches it.
type FunctionId uint32

// InvalidBlock is the zero BlockId used where a terminal has no such successor.
//
// Terminals name their successors by id and several have optional ones. A sentinel is used rather
// than a pointer or an Option because a BlockId is Copy and a zero value is what an unset struct
// field already holds; the entry block is never id 0 for this reason. `HasBlock` is the guard.
const InvalidBlock static_single_assignment.BlockId = 0

// HasBlock reports whether a terminal's successor field names a real block.
func HasBlock(block static_single_assignment.BlockId) bool { return block != InvalidBlock }

const (
	// FunctionKindOther is anything not named like a component or a hook.
	FunctionKindOther FunctionKind = iota
	// FunctionKindComponent is a function whose name begins with a capital letter.
	FunctionKindComponent
	// FunctionKindHook is a function whose name begins with `use` followed by a capital or digit.
	FunctionKindHook
)

// BasicBlock is a straight-line run of instructions ending in exactly one terminal.
//
// Control enters only at the top and leaves only through the terminal. That invariant is what makes
// a dataflow analysis over this correct with a per-block transfer function, and lowering maintains
// it: an expression that can branch mid-evaluation ends the block.
type BasicBlock struct {
	Id   static_single_assignment.BlockId
	Kind BlockKind

	// Instructions are ids into Function.Instructions, in evaluation order.
	Instructions []InstructionId

	// Terminal is how control leaves. Never nil in a completed function.
	Terminal Terminal

	// Predecessors are the blocks with an edge into this one, maintained by MarkPredecessors.
	//
	// Held as a slice rather than a set because insertion order is stable and phi operands will be
	// keyed by predecessor: single-assignment construction needs a deterministic order to produce
	// deterministic phis, and a Go map does not have one.
	Predecessors []static_single_assignment.BlockId

	// Phis are the merge points for values with several reaching definitions.
	//
	// Empty until `Construct` runs. After it, a phi appears here wherever a binding's value depends
	// on which predecessor control arrived from, and every phi is a real merge: `Construct` runs
	// redundant-phi elimination, so a phi whose operands all agree has already been removed.
	Phis []*Phi
}

const (
	// BlockKindBlock is an ordinary statement block.
	BlockKindBlock BlockKind = iota
	// BlockKindValue is a block whose purpose is to produce a value, such as one arm of a ternary.
	BlockKindValue
	// BlockKindLoop marks loop-related blocks, including loop initializers and tests.
	BlockKindLoop
	// BlockKindSequence is one element of a sequence expression.
	BlockKindSequence
	// BlockKindCatch is a catch handler.
	BlockKindCatch
)

// Phi is a merge: the value of Place at the top of a block, given which predecessor control came
// from. Its operands are a short slice sorted by predecessor; static_single_assignment.Phi says why.
type Phi = static_single_assignment.Phi[Place]

// PhiOperand is the value a phi takes when control arrives from Predecessor.
type PhiOperand = static_single_assignment.PhiOperand[Place]

// PhiOperands is a phi's operands, sorted by predecessor block id with at most one entry each.
type PhiOperands = static_single_assignment.PhiOperands[Place]

// Instruction is one operation: a value computed and stored into an lvalue.
//
// Every instruction has an lvalue, including those evaluated for effect alone, which store into a
// temporary nothing reads. That uniformity is what makes the instruction set three-address and it
// is why a pass can ask "what defines this value" and get exactly one instruction back before
// single-assignment form and exactly one after.
type Instruction struct {
	Id InstructionId

	// Order is where this evaluated relative to everything else in the function.
	Order static_single_assignment.EvaluationOrder

	// LValue is where the result is stored.
	LValue Place

	// Value is what was computed.
	Value InstructionValue

	// Node is the syntax this came from, for diagnostics and for the type checker. Nil for an
	// instruction lowering introduced with no direct source, such as an iterator step.
	Node *ast.Node

	// Range is the source span. Zero when Node is nil.
	Range core.TextRange
}

// Identifier names one value and carries what is known about it.
type Identifier struct {
	Id static_single_assignment.IdentifierId

	// Declaration groups every value one source binding takes. See DeclarationId.
	Declaration static_single_assignment.DeclarationId

	// Name is the source name, or empty for a temporary. Temporaries are the common case: every
	// intermediate value in an expression gets one.
	Name string

	// Node is the syntactic node that introduced this value, where one exists.
	//
	// This field is the seam to the type checker and the reason this IR does not carry a type
	// inference pass. Upstream has 1,450 lines of `typeinference` whose job is to guess what a
	// checker would know; cohere has the checker resident in the same process. A pass that wants
	// the type of a value hands this node to `checker.GetTypeAtLocation` and gets the real answer,
	// including through imports and generics, which no amount of local inference recovers.
	//
	// Nil for a value with no direct syntactic source. A pass consulting the checker must handle
	// that by falling back to the defining instruction's Node, which is why Instruction keeps one
	// too.
	Node *ast.Node
}

// Place is a reference to a value at one point in the program, plus what this reference does to it.
//
// Two places naming the same IdentifierId are the same value; the Effect differs per reference,
// because reading a value and mutating it are different facts about the same value.
//
// This is the one structure upstream marks as React-contaminated, and the contamination is one bit.
// `Effect` is a general mutation lattice - only `EffectFreeze` is React-flavoured and it generalises
// to "must not be mutated past this point", which is what `Object.freeze`, a readonly type, and a
// React prop all separately mean. `Reactive` is genuinely React's and is carried as a bool because
// the alternative is a parallel side table that every pass must be handed.
type Place struct {
	Identifier static_single_assignment.IdentifierId

	// Effect is what this reference does to the value. Left EffectUnknown by lowering; an effect
	// inference pass fills it in.
	Effect Effect

	// Reactive reports whether this value can change between renders. Left false by lowering.
	Reactive bool

	// Range is the source span this reference occupies, for diagnostics.
	Range core.TextRange
}

const (
	// EffectUnknown is the unset value. Every place lowering produces holds this.
	EffectUnknown Effect = iota
	// EffectFreeze means the value must not be mutated after this point.
	EffectFreeze
	// EffectRead means the value is read and not retained.
	EffectRead
	// EffectCapture means the value may be retained by something that outlives this reference.
	EffectCapture
	// EffectConditionallyMutateIterator means an iterator may be advanced, which mutates it.
	EffectConditionallyMutateIterator
	// EffectConditionallyMutate means the value may be mutated, depending on a path not known here.
	EffectConditionallyMutate
	// EffectMutate means the value is mutated.
	EffectMutate
	// EffectStore means the value is stored into.
	EffectStore
)

const (
	// InstructionKindConst declares a `const` binding.
	InstructionKindConst InstructionKind = iota
	// InstructionKindLet declares a `let` binding.
	InstructionKindLet
	// InstructionKindReassign stores to an already-declared binding.
	InstructionKindReassign
	// InstructionKindCatch binds a catch parameter.
	InstructionKindCatch
	// InstructionKindHoistedConst declares a hoisted `const`.
	InstructionKindHoistedConst
	// InstructionKindHoistedLet declares a hoisted `let`.
	InstructionKindHoistedLet
	// InstructionKindHoistedFunction declares a hoisted function.
	InstructionKindHoistedFunction
	// InstructionKindFunction declares a function binding.
	InstructionKindFunction
)

// NewFunction creates an empty function ready for lowering.
func NewFunction(node *ast.Node, name string, kind FunctionKind) *Function {
	return &Function{
		Node:       node,
		Name:       name,
		Kind:       kind,
		blocksById: map[static_single_assignment.BlockId]*BasicBlock{},
		nextBlock:  1, // 0 is InvalidBlock.
	}
}
