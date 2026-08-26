// Aliasing effects: what a call does to its arguments, and what an instruction does to its operands.
//
// This is React's `InferMutationAliasingEffects.ts`, run over this graph to produce an
// `AliasingEffects` table, which nothing in this tree computes today. oxc transcribes the same pass
// at `oxc_react_compiler/src/react_compiler_inference/infer_mutation_aliasing_effects.rs`, 3,297
// lines; where the two disagree React wins, and each disagreement is recorded at the line that
// resolves it.
//
// The question this answers is interprocedural and general: when a value is passed to a callee,
// does the callee mutate it, retain it past the call, or only read it? React is one consumer. The
// same table is what a rule needs to say that an object crossed an async boundary and was then
// mutated, that a `readonly` was violated by a callee, or that a `const` is lying.
//
// # `Place.Effect` is NOT the output of this pass, and that is measured rather than argued
//
// The task that commissioned this named `Place.Effect` as the thing to fill, and the `Effect` doc
// comment in hir.go warns at length that `Effect` is an OUTPUT of an abstract interpretation rather
// than a lattice to join over. Both are right about the second half and the first half is wrong
// about WHICH pass writes it, so it is worth stating exactly, because the whole shape of this file
// follows from it.
//
// Every write to a `Place.effect` field in the entire oxc compiler crate, enumerated with
// `grep -rn "\.effect = "` over `crates/oxc_react_compiler/src` and controlled against 26 reads:
//
//	infer_mutation_aliasing_effects.rs   1 write     a DOWNGRADE of a closure context capture
//	infer_mutation_aliasing_ranges.rs   12 writes    the real assignment of every effect
//	analyse_functions.rs                 2 writes    seeding a nested function's context operands
//
// So the 3,297-line pass this file ports writes `Place.Effect` exactly ONCE, and that one write is
// a correction (`Capture` downgraded to `Read` when the captured value turns out to be primitive,
// frozen or global). The twelve real writes are in the RANGES pass, which is Stage 2, and they are
// a projection: `operand_effects` is a `map[IdentifierId]Effect` built in one 40-line match over
// the `AliasingEffect` list this pass produced, then splatted onto every operand of the
// instruction. `infer_mutation_aliasing_ranges.rs:856-901` is the whole mapping.
//
// The two enums are different types and only one of them is the interesting one:
//
//	AliasingEffect   19 variants, carries FROM and INTO places, is the pass's real output
//	Effect            8 constants, one per reference, is a per-operand summary derived from it
//
// `Effect` cannot be computed without `AliasingEffect`, and the arrow runs one way. That is why
// this file produces `AliasingEffects` and exposes `ProjectEffects` for the scalar rather than
// writing `Place.Effect` directly: writing the scalar here would reproduce neither implementation,
// and would in particular write it BEFORE mutable ranges exist, which is the one input the
// projection actually reads (`mutable_range.end > eval_order` decides Capture against Read on every
// aliasing edge).
//
// # The signature table is the pass, and the "4,400 generic lines" estimate is measured false
//
// The dispatch commissioning this warned that the general assumption of a large generic port had
// been measured false three times already. It is false here too, and by a wider margin than for the
// rules that found it.
//
// `Effect` values enter the compiler from exactly one place: a hardcoded global signature table at
// `react_compiler_hir/globals.rs`, 1,828 lines, holding 122 `Effect::` literals across 31
// `positional_params` entries. Nothing infers an `Effect` from a callee's body for any callee that
// is not a local function expression. `compute_effects_for_legacy_signature` READS
// `signature.positional_params[i]` and `signature.callee_effect`; it does not compute them.
//
// Measured against React's own executable, with a passing control and a two-sided split, one probe
// per method with the freeze source and the mutation held identical and only the method name
// varying:
//
//	a.push(2)      Effect::Store in the table    REPORTS
//	a.pop()        Effect::Store in the table    REPORTS
//	s.add(1)       Effect::Store in the table    REPORTS
//	m.set(1,2)     Effect::Store in the table    REPORTS
//	a.sort()       absent from the table         CLEAN
//	a.splice(0,1)  absent from the table         CLEAN
//	a.shift()      absent from the table         CLEAN
//	a.unshift(0)   absent from the table         CLEAN
//	a.reverse()    absent from the table         CLEAN
//
// Nine probes, a clean four-against-five split on table membership alone, control firing. `sort`,
// `splice`, `shift`, `unshift` and `reverse` all mutate their receiver in fact and React is silent
// on every one of them, because they are not in the table. That is not a subtlety of the analysis,
// it is the absence of an entry. A port that "improved" on this by reasoning about what the methods
// really do would disagree with React on ordinary code.
//
// So the table is not an optimization in front of an inference engine. It IS the source of every
// effect, and the inference engine's job is to PROPAGATE those seeded facts through aliasing,
// capture and control flow.
//
// # What fraction of real calls hit a signature, measured on Kirk's tree
//
// Over 23,151 lowered functions across 3,403 files, counting every named call site:
//
//	call sites total (CallExpression 31,433 + MethodCall 57,447 + New 4,862)   93,742
//	of those, a name recoverable from the value or the syntax                  88,089
//	name appears in oxc's global signature table                               27,096   30.8%
//	no signature, falls to the DEFAULT path                                     60,993   69.2%
//	distinct names that hit                                                         57
//	distinct names that miss                                                     8,529
//
// Two-sided, and the control for the zero is the same probe reporting 88,089 named sites against
// 791 anonymous ones, so a zero would have been a real absence rather than a filter that could not
// match. The first run of that probe DID report a false zero -- `CALLEE_distinct 0` against
// `calleeNamed 57,446` -- because a plain call's callee is a nameless temporary in this
// representation and the name has to be recovered from the syntax. That is recorded because it is
// the shape of a wrong measurement that looks like a finding.
//
// 57 distinct names cover 30.8% of all calls. The remaining 69.2% get the default, and the default
// is NOT "no effect": it is `Create{Mutable}` on the result plus, for every operand,
// `MutateTransitiveConditionally` + `MaybeAlias` into the result + `Capture` into every other
// operand (`infer_mutation_aliasing_effects.rs:1823-1892`). An unknown call is assumed to mutate
// and capture everything handed to it. That is the conservative direction and it is why the table's
// gaps are safe: a missing entry means "assume the worst", not "assume nothing".
//
// # The interprocedural half exists and is narrower than its name suggests
//
// There is real inference for locally-defined callees, at `infer_mutation_aliasing_effects.rs:1716`:
// if the callee identifier `state.is_defined` and resolves to exactly ONE value which is a known
// function expression, its own inferred effects are instantiated at the call site. Measured against
// React's executable, this is much narrower than "interprocedural analysis" suggests, and the
// asymmetry is sharp enough to be worth pinning:
//
//	const o={a:0}; useFoo(o); ((x)=>{ x.a=1; })(o);              REPORTS   inline callee
//	const o={a:0}; useFoo(o); function m(x){x.a=1;} m(o);        CLEAN     named declaration
//	const o={a:0}; useFoo(o); const m=(x)=>{x.a=1;}; m(o);       CLEAN     named const arrow
//	function C(props){ function m(x){x.a=1;} m(props); }         CLEAN     even for props
//	const o={a:0}; useFoo(o); const m=()=>{o.a=1;}; m();         REPORTS   closure capture, not a param
//
// Controls in the same batch: direct `props.a=1` reports, plain local mutation is clean. So effects
// propagate out of a function expression through its CAPTURES (row 5) but not through its
// PARAMETERS when the callee is reached by name (rows 2-4). Row 1 fires because the immediately
// invoked arrow is its own callee value at the call site with no binding in between.
//
// That last distinction is the whole of the interprocedural half, and it is NOT implemented here.
// See `EffectGapInterproceduralParameters` for why and what it costs.
//
// # What this pass computes, and the two-sided count on the real tree
//
// Everything upstream derives from the instruction SHAPE, plus the signature table, plus the
// conservative default. That is the majority of the 3,297 lines and it is all of the part that is
// substrate rather than React.
//
// # Convergence: there is none here, deliberately
//
// `reactive.go` and `immutability.go` both carry a warning that convergence equality is not
// identity equality and that a fixpoint comparing anything meaninglessly variable hangs silently.
// That hazard does not arise in this file because this pass is NOT a fixpoint: upstream's
// `compute_signature_for_instruction` is a pure function of one instruction, and the fixpoint in
// `infer_mutation_aliasing_effects.rs` is over the abstract STATE (`InferenceState`), which is the
// part `immutability.go` already implements for its own six-element lattice.
//
// This file computes the effect SIGNATURES, which upstream also computes without a fixpoint. That
// split is upstream's own: `compute_signature_for_instruction` is called once per instruction and
// its result is cached (`instruction_signature_cache`), while `apply_effect` runs inside the
// worklist. Keeping the same split means this file has no convergence test to get wrong, and the
// pass that needs one already exists.
//
// # Phi operands are a Go map and nothing here reads them
//
// `Phi.Operands` is `map[BlockId]Place` and ranging it is nondeterministic; `static_components.go`
// has a latent nondeterminism from exactly that. This pass produces effects per INSTRUCTION and
// never walks a phi, so the hazard does not arise. Every place this file emits comes from an
// instruction's own operand list, which is a slice. `TestEffectsAreDeterministic` runs the pass
// twice over the corpus and compares, so a later addition that does reach for a phi fails loudly.
package hir

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// AliasingEffectKind is what one effect does. These are React's `AliasingEffect` variants.
//
// This is the real output type of this pass and it is NOT `Effect`. See the package comment: the
// two are different types, this one carries FROM and INTO places, and the scalar `Effect` is a
// per-operand projection of a whole list of these, computed later by the ranges pass.
//
// The declaration order carries no meaning and there is no join over it. Unlike `ValueKind`, which
// `immutability.go` merges with a real lattice operation, these are not merged at all: an
// instruction produces a LIST of effects and they are all applied in order. A pass that tried to
// reduce the list to one element would lose the from/into pairing that is the entire content.
type AliasingEffectKind uint8

const (
	// AliasingEffectCreate makes a new value of a given kind at Into. From is unset.
	AliasingEffectCreate AliasingEffectKind = iota
	// AliasingEffectCreateFrom makes a new value at Into with the same kind as From.
	AliasingEffectCreateFrom
	// AliasingEffectAssign is `Into = From`, a direct assignment.
	AliasingEffectAssign
	// AliasingEffectAlias means mutating Into implies mutating From. Direct aliasing.
	AliasingEffectAlias
	// AliasingEffectMaybeAlias is a potential aliasing relationship, used for unknown callees.
	AliasingEffectMaybeAlias
	// AliasingEffectCapture is information flow from From into Into without aliasing.
	AliasingEffectCapture
	// AliasingEffectImmutableCapture is data flow that escape analysis sees and mutable ranges do
	// not. Projects to Read rather than to Capture.
	AliasingEffectImmutableCapture
	// AliasingEffectMutate mutates the value and its direct aliases.
	AliasingEffectMutate
	// AliasingEffectMutateConditionally mutates only if the value is mutable.
	AliasingEffectMutateConditionally
	// AliasingEffectMutateTransitive mutates the value and everything it transitively captured.
	AliasingEffectMutateTransitive
	// AliasingEffectMutateTransitiveConditionally is the conditional form, and is the DEFAULT
	// applied to every operand of a call with no known signature.
	AliasingEffectMutateTransitiveConditionally
	// AliasingEffectFreeze marks the value and its direct aliases as frozen.
	AliasingEffectFreeze
	// AliasingEffectApply is an unresolved call. Upstream replaces every Apply with more precise
	// effects before the ranges pass runs, and raises an invariant if one survives; this pass
	// resolves them the same way and never emits one. Kept as a constant because
	// `EffectGapInterproceduralParameters` is exactly the case where upstream would resolve one
	// and this does not.
	AliasingEffectApply
)

func (k AliasingEffectKind) String() string {
	switch k {
	case AliasingEffectCreate:
		return "create"
	case AliasingEffectCreateFrom:
		return "create-from"
	case AliasingEffectAssign:
		return "assign"
	case AliasingEffectAlias:
		return "alias"
	case AliasingEffectMaybeAlias:
		return "maybe-alias"
	case AliasingEffectCapture:
		return "capture"
	case AliasingEffectImmutableCapture:
		return "immutable-capture"
	case AliasingEffectMutate:
		return "mutate"
	case AliasingEffectMutateConditionally:
		return "mutate-conditionally"
	case AliasingEffectMutateTransitive:
		return "mutate-transitive"
	case AliasingEffectMutateTransitiveConditionally:
		return "mutate-transitive-conditionally"
	case AliasingEffectFreeze:
		return "freeze"
	case AliasingEffectApply:
		return "apply"
	default:
		return "<unknown>"
	}
}

// IsMutation reports whether this effect widens a value's mutable range.
//
// This is the exact predicate the ranges pass reads to build its `mutations` list, at
// `infer_mutation_aliasing_ranges.rs` where `mutations` is populated from effects matching
// `Mutate | MutateConditionally | MutateTransitive | MutateTransitiveConditionally`. Exposed here
// rather than restated there so the two passes cannot drift.
func (k AliasingEffectKind) IsMutation() bool {
	switch k {
	case AliasingEffectMutate, AliasingEffectMutateConditionally,
		AliasingEffectMutateTransitive, AliasingEffectMutateTransitiveConditionally:
		return true
	default:
		return false
	}
}

// IsAliasing reports whether this effect creates a from/into data-flow edge.
//
// These are the five variants the ranges pass treats identically when projecting to a scalar
// `Effect`: all five give From either Capture or Read depending on whether Into is still mutable,
// and give Into Store. See `ProjectEffects`.
func (k AliasingEffectKind) IsAliasing() bool {
	switch k {
	case AliasingEffectAssign, AliasingEffectAlias, AliasingEffectCapture,
		AliasingEffectCreateFrom, AliasingEffectMaybeAlias:
		return true
	default:
		return false
	}
}

// AliasingEffect is one effect an instruction has.
//
// From is the source of a data-flow edge and is meaningless for the mutation and create variants,
// where only Into is set. That asymmetry is upstream's: its enum gives each variant its own fields
// and Go's does not, so `HasFrom` names which variants read it rather than leaving a caller to
// infer it from a zero value. A zero IdentifierId is a real identifier, so an unset From is not
// distinguishable by value.
type AliasingEffect struct {
	Kind AliasingEffectKind

	// From is the source place of a data-flow edge. Only meaningful when HasFrom is true.
	From Place
	// Into is the target: the value created, mutated, or flowed into.
	Into Place
	// HasFrom reports whether From is set, because a zero IdentifierId is a legal value.
	HasFrom bool

	// Value is the kind created, meaningful only for AliasingEffectCreate.
	Value EffectValueKind
}

// EffectValueKind is the abstract kind a Create effect produces.
//
// This is upstream's `ValueKind` and it is deliberately NOT the same type as the six-element
// lattice `immutability.go` builds for itself. That rule's lattice is a private type with a join
// (`immutabilityMergeKinds`) tuned to what it reports; this is only the seed kind a Create names,
// with no join, because this pass never merges two kinds. Sharing one type across the two would
// couple a rule's message selection to a substrate enum for no gain, and `immutability.go`'s
// version carries a reason bitset this one has no use for.
type EffectValueKind uint8

const (
	// EffectValueMutable is a freshly created value this code may still write to.
	EffectValueMutable EffectValueKind = iota
	// EffectValuePrimitive is a number, string, boolean or similar.
	EffectValuePrimitive
	// EffectValueFrozen is a value that must not be mutated from here on.
	EffectValueFrozen
)

func (k EffectValueKind) String() string {
	switch k {
	case EffectValuePrimitive:
		return "primitive"
	case EffectValueFrozen:
		return "frozen"
	default:
		return "mutable"
	}
}

// AliasingEffects is the table this pass produces: the effect list of every instruction.
//
// Keyed by InstructionId rather than stored on the Instruction because an Instruction is shared
// through the per-file lowering cache and several rules hold the same one. Writing effects onto it
// would make one rule's pass visible to another that never asked for it, and `Construct` is not
// idempotent, so a table that has to be rebuilt is safer than a graph that has been mutated.
type AliasingEffects struct {
	byInstruction map[InstructionId][]AliasingEffect
}

// Get returns one instruction's effects, or nil when it has none.
func (e *AliasingEffects) Get(id InstructionId) []AliasingEffect {
	if e == nil {
		return nil
	}
	return e.byInstruction[id]
}

// Len is how many instructions carry at least one effect.
func (e *AliasingEffects) Len() int {
	if e == nil {
		return 0
	}
	return len(e.byInstruction)
}

// EffectGap names an effect-inference rule this pass cannot apply, for a caller that needs to know.
//
// Named in the API rather than only in a comment, the way `ReactiveGaps` and `RangeGaps` are, for
// the reason `reactive.go` gives: a caller that needs the missing half should be able to ask rather
// than discover it. Both React and oxc treat a half-filled effect table as an invariant violation
// rather than a skippable default, so a consumer being able to enumerate what is missing is the
// difference between an under-approximation it can reason about and a table that silently lies.
type EffectGap uint8

const (
	// EffectGapInterproceduralParameters is an effect that would reach a call site from the body of
	// a locally-defined callee reached BY NAME.
	//
	// Upstream instantiates a function expression's own inferred effects at a call site when the
	// callee identifier resolves to exactly one known function value
	// (`infer_mutation_aliasing_effects.rs:1716-1764`). This pass does not, so a named local
	// function that mutates its parameter is invisible.
	//
	// Measured against React's executable, this gap is NARROWER than it sounds, because React does
	// not fire on those cases either: `function m(x){x.a=1;} m(frozenValue)` is CLEAN upstream, as
	// is the `const m = (x) => {x.a=1;}` spelling. The cases where upstream DOES resolve a local
	// callee and this does not are the immediately-invoked form and the deeper propagation the
	// resolution feeds. So the observable cost of this gap on React's own diagnostics is small; the
	// cost to a general consumer asking "does this callee mutate my argument" is total.
	EffectGapInterproceduralParameters EffectGap = iota

	// EffectGapSignatureTable is an effect that would come from a global signature entry this tree
	// does not carry.
	//
	// Upstream seeds every non-default effect from a 1,828-line hardcoded table (`globals.rs`, 122
	// `Effect::` literals over 31 signature entries). This pass carries the subset in
	// `effectSignatures` below. A name outside that subset falls to the conservative default, which
	// assumes the callee mutates and captures every operand, so the direction of this gap is
	// over-approximation of mutation rather than under-approximation.
	//
	// Measured: 30.8% of 88,089 named call sites on Kirk's tree hit a name in upstream's full
	// table, and 57 distinct names account for all of it.
	EffectGapSignatureTable

	// EffectGapTypeDirectedShapes is an effect upstream derives from a value's inferred SHAPE
	// rather than from the callee's name.
	//
	// Upstream's `Environment` carries a shape registry keyed by `TypeId`, so `props.items.push(x)`
	// resolves `push` against the array shape rather than against a bare name. This pass keys on
	// the syntactic name, which is what `reactive.go` also had to do and for the same stated
	// reason: verify has the real checker but no shape registry, and building one is Stage 4 work.
	// The consequence is that a name shadowed by an unrelated method of the same spelling gets the
	// builtin's effect.
	EffectGapTypeDirectedShapes
)

// EffectGaps are the rules InferAliasingEffects does not apply. See EffectGap.
func EffectGaps() []EffectGap {
	return []EffectGap{
		EffectGapInterproceduralParameters,
		EffectGapSignatureTable,
		EffectGapTypeDirectedShapes,
	}
}

func (g EffectGap) String() string {
	switch g {
	case EffectGapInterproceduralParameters:
		return "interprocedural-parameters"
	case EffectGapSignatureTable:
		return "signature-table"
	case EffectGapTypeDirectedShapes:
		return "type-directed-shapes"
	default:
		return "<unknown>"
	}
}

// effectSignature is what one known callee does to its receiver and arguments.
//
// This is the shape of an entry in upstream's `globals.rs` table, reduced to the two fields that
// carry every `Effect` literal in it: `callee_effect` and `positional_params`. The full upstream
// struct also carries a return type, a return value kind, an impurity flag and an aliasing config;
// those select the RESULT's kind rather than the operands' effects, and `immutabilityHookResultKind`
// already covers the React-relevant half of that for the one rule that needs it.
type effectSignature struct {
	// Receiver is what calling this does to the object it is called on. Read for a pure method,
	// Store for one that writes through it.
	Receiver Effect
	// Positional is what each argument gets, by position.
	Positional []Effect
	// Rest is what arguments past Positional get. Unset means they get Rest's zero, which is
	// EffectUnknown, so a signature with fewer positionals than arguments must set this.
	Rest Effect
	// HasRest reports whether Rest is meaningful, because EffectUnknown is a legal zero.
	HasRest bool
	// Result is the kind the call produces.
	Result EffectValueKind
	// Aliasing is upstream's second signature form, preferred over the scalar fields above.
	//
	// `InferMutationAliasingEffects.ts:1070` reads `signature.aliasing` when present and only falls
	// back to `computeEffectsForLegacySignature` otherwise. The scalar form cannot express where a
	// captured operand goes: it says "this argument is captured" and leaves the destination to a
	// reconciliation step, which aliases into the lvalue. The aliasing form names both ends.
	//
	// Measured on `Object.values(object)`: the scalar path emits an immutable capture of the
	// receiver, upstream emits `Capture from @object into @returns`, and only the second carries a
	// later mutation of the result back to the argument.
	//
	// Nil means this entry has no aliasing form and takes the scalar path, which is every entry that
	// has not been transcribed yet rather than a claim that upstream lacks one.
	Aliasing []aliasingSignatureEffect
}

// aliasingOperand names one end of an aliasing-signature effect.
//
// Upstream uses `IdentifierId` placeholders bound through a substitution map. The placeholders it
// actually needs for the static table are the receiver, the positional params and the return value,
// so this is that closed set rather than an id space.
type aliasingOperand uint8

const (
	// aliasingReceiver is upstream's `@receiver`: the object a method is called on.
	aliasingReceiver aliasingOperand = iota
	// aliasingReturns is upstream's `@returns`: the call's own result.
	aliasingReturns
	// aliasingParam0 is the first positional argument. Later positions are `aliasingParam0 + n`,
	// which keeps the set contiguous for the bounds check at the substitution site.
	aliasingParam0
)

// aliasingSignatureEffect is one entry of an aliasing signature's effect list.
//
// `From` is meaningless for `AliasingEffectCreate`, which upstream spells with no source, and
// `Value` is meaningful only for it -- the same asymmetry `AliasingEffect` already carries.
type aliasingSignatureEffect struct {
	Kind  AliasingEffectKind
	From  aliasingOperand
	Into  aliasingOperand
	Value EffectValueKind
}

// effectSignatures is the transcribed subset of upstream's global signature table.
//
// Transcribed from `react_compiler_hir/globals.rs` rather than invented, and deliberately a SUBSET
// rather than a guess at the whole: every entry here was read off a specific upstream
// `MethodDef`/`FunctionDef`, and a name upstream carries that is not here falls to the conservative
// default rather than to a made-up effect. `EffectGapSignatureTable` names that.
//
// The entries were chosen by measured frequency on Kirk's tree rather than by reading the table top
// to bottom: these cover the mutating methods that actually appear. `push` alone is 2,202 call
// sites, `set` 610, `add` and `pop` smaller. The read-only entries matter more than they look,
// because without them a `map` or a `filter` falls to the default and is assumed to mutate its
// receiver, which would make the table WORSE than empty for the most common calls in the tree.
//
// # This table is keyed by NAME and upstream keys by SHAPE
//
// See `EffectGapTypeDirectedShapes`. A user-defined `push` on an unrelated class gets Array's
// signature here and does not upstream. That is a real divergence and it is the same shape
// `reactive.go` accepted for `isHookCallee` for the same reason: verify has no shape registry.
// It is recorded rather than hidden, and the direction is that a mutating effect is applied where
// upstream might apply the default -- which is also a mutating effect, so the two agree on the
// mutation question and differ only on transitivity.
var effectSignatures = map[string]effectSignature{
	// Array mutators. Every one of these carries Effect::Store as its callee_effect upstream, and
	// the measured probe against React's executable confirms all four report a frozen-value
	// mutation while the five array mutators ABSENT from upstream's table (sort, splice, shift,
	// unshift, reverse) are all clean. Those five are deliberately NOT added here: adding them
	// would be improving on upstream, which the brief names as a defect in a port.
	"push": {Receiver: EffectStore, Rest: EffectCapture, HasRest: true, Result: EffectValuePrimitive},
	"pop":  {Receiver: EffectStore, Result: EffectValueMutable},

	// Set and Map mutators, both Effect::Store upstream.
	"add": {Receiver: EffectStore, Positional: []Effect{EffectCapture}, Result: EffectValueMutable},
	"set": {Receiver: EffectStore, Positional: []Effect{EffectCapture, EffectCapture}, Result: EffectValueMutable},

	// Read-only collection methods. Effect::Capture as callee_effect upstream, because the result
	// may alias the receiver's elements, with Effect::Read on the callback argument.
	"map":     {Receiver: EffectCapture, Positional: []Effect{EffectRead}, Result: EffectValueMutable},
	"filter":  {Receiver: EffectCapture, Positional: []Effect{EffectRead}, Result: EffectValueMutable},
	"slice":   {Receiver: EffectCapture, Positional: []Effect{EffectRead, EffectRead}, Result: EffectValueMutable},
	"concat":  {Receiver: EffectCapture, Rest: EffectCapture, HasRest: true, Result: EffectValueMutable},
	"at":      {Receiver: EffectCapture, Positional: []Effect{EffectRead}, Result: EffectValueMutable},
	"entries": {Receiver: EffectCapture, Result: EffectValueMutable},

	// Pure reads returning primitives. Effect::Read throughout, PURE_PRIMITIVE_FN upstream.
	"includes":   {Receiver: EffectRead, Rest: EffectRead, HasRest: true, Result: EffectValuePrimitive},
	"indexOf":    {Receiver: EffectRead, Rest: EffectRead, HasRest: true, Result: EffectValuePrimitive},
	"join":       {Receiver: EffectRead, Positional: []Effect{EffectRead}, Result: EffectValuePrimitive},
	"has":        {Receiver: EffectRead, Positional: []Effect{EffectRead}, Result: EffectValuePrimitive},
	"toString":   {Receiver: EffectRead, Result: EffectValuePrimitive},
	"find":       {Receiver: EffectCapture, Positional: []Effect{EffectRead}, Result: EffectValueMutable},
	"findIndex":  {Receiver: EffectRead, Positional: []Effect{EffectRead}, Result: EffectValuePrimitive},
	"every":      {Receiver: EffectRead, Positional: []Effect{EffectRead}, Result: EffectValuePrimitive},
	"forEach":    {Receiver: EffectCapture, Positional: []Effect{EffectRead}, Result: EffectValuePrimitive},
	"flatMap":    {Receiver: EffectCapture, Positional: []Effect{EffectRead}, Result: EffectValueMutable},
	"difference": {Receiver: EffectRead, Positional: []Effect{EffectRead}, Result: EffectValueMutable},
}

// effectQualifiedMethods is the subset of upstream's table that only makes sense with a receiver.
//
// Keyed `Receiver.method`, and consulted BEFORE `effectSignatures`, because the bare-name table
// answers a different question for three of these names. `entries` is declared there as the Array
// method, where the receiver is the collection being read and the call takes no arguments. On
// `Object.entries(x)` the receiver is the `Object` global and the collection is a POSITIONAL
// argument, so the bare-name lookup gives `x` no declared effect at all and the loop in
// `effectsForCall` falls through to `EffectConditionallyMutate` for it.
//
// That fallthrough is the mechanism behind three fixtures, and `object-keys.js` states it in its own
// source: "Without a declaration for Object.entries(), this would be assumed to mutate `record`,
// meaning existing memoization couldn't be preserved."
//
// Transcribed from `HIR/Globals.ts:84-140` rather than reasoned about. Upstream keys its whole
// registry by SHAPE, so a receiver named `Object` that is not the global resolves differently there
// and identically here. That is `EffectGapTypeDirectedShapes` again, and the direction is the same:
// this table applies a READ where the default applies a mutation, so a shadowed `Object` would be
// treated as less mutating than upstream treats it. Bounded by there being four entries, all on a
// name that is a syntax error to declare as a local in strict-mode module scope.
var effectQualifiedMethods = map[string]effectSignature{
	// Effect.Read on the positional param, per `Globals.ts:88-96`.
	"Object.keys": {
		Receiver: EffectRead, Positional: []Effect{EffectRead}, Result: EffectValueMutable,
		// `Globals.ts:147-178`, and note this one is `ImmutableCapture` where `values` and
		// `entries` are `Capture`: keys are fresh strings, so nothing of the object flows into the
		// result and a later mutation of the array must not reach back.
		Aliasing: []aliasingSignatureEffect{
			{Kind: AliasingEffectCreate, Into: aliasingReturns, Value: EffectValueMutable},
			{Kind: AliasingEffectImmutableCapture, From: aliasingParam0, Into: aliasingReturns},
		},
	},
	// Effect.CAPTURE, `Globals.ts:177-183`, and not Read. The first spelling of this entry inferred
	// it from `keys` on the reasoning that the two are siblings returning an array, and that is
	// wrong at the source: `values` returns the object's own values, so the result may alias them,
	// exactly as `entries` does. `keys` returns fresh strings and captures nothing.
	//
	// The wrong reading cost a real defect and the corpus caught it rather than the fixtures: with
	// Read here, `recomputeIsValid` lost 6 instructions with no unmatched goto to attribute them to,
	// breaking the pinned invariant in `reactive_build_test.go` that every unattributed loss
	// coincides with one. The false-positive score did not move either way.
	"Object.values": {
		Receiver: EffectRead, Positional: []Effect{EffectCapture}, Result: EffectValueMutable,
		// `Globals.ts:185`, with upstream's own comment: "Object values are captured into the
		// return". The scalar form above says the argument is captured and leaves the destination
		// to a reconciliation step; this names it, which is what carries a later mutation of the
		// result back to the object.
		Aliasing: []aliasingSignatureEffect{
			{Kind: AliasingEffectCreate, Into: aliasingReturns, Value: EffectValueMutable},
			{Kind: AliasingEffectCapture, From: aliasingParam0, Into: aliasingReturns},
		},
	},
	// Effect.Capture, `Globals.ts:115-123`: the returned array holds the receiver's own values, so
	// the result may alias them. Capture rather than Read for exactly that reason.
	"Object.entries": {
		Receiver: EffectRead, Positional: []Effect{EffectCapture}, Result: EffectValueMutable,
		// `Globals.ts:116-147`, the same pair as `values`: the returned array holds the object's
		// own values, so they are captured into the return rather than merely read.
		Aliasing: []aliasingSignatureEffect{
			{Kind: AliasingEffectCreate, Into: aliasingReturns, Value: EffectValueMutable},
			{Kind: AliasingEffectCapture, From: aliasingParam0, Into: aliasingReturns},
		},
	},
	// Effect.ConditionallyMutate, `Globals.ts:98-113`. Upstream is deliberately WEAKER here than for
	// the other three: `fromEntries` walks an iterable, and advancing an iterator can mutate it.
	// Transcribed rather than strengthened to match its neighbours.
	"Object.fromEntries": {
		Receiver: EffectRead, Positional: []Effect{EffectConditionallyMutate},
		Result: EffectValueMutable,
	},
}

// effectGlobalFunctions is the subset of upstream's global FUNCTION table, keyed by callee name.
//
// Separate from effectSignatures because these are reached as bare calls rather than as methods, so
// there is no receiver to give an effect to. Upstream folds both into one `Global` registry; the
// split here is because this representation gives a plain call a nameless temporary callee and a
// method call a named property, so the two are looked up differently and keeping them apart makes
// the lookup site honest about which one it consulted.
var effectGlobalFunctions = map[string]effectSignature{
	"String":             {Rest: EffectRead, HasRest: true, Result: EffectValuePrimitive},
	"Number":             {Rest: EffectRead, HasRest: true, Result: EffectValuePrimitive},
	"Boolean":            {Rest: EffectRead, HasRest: true, Result: EffectValuePrimitive},
	"parseInt":           {Rest: EffectRead, HasRest: true, Result: EffectValuePrimitive},
	"parseFloat":         {Rest: EffectRead, HasRest: true, Result: EffectValuePrimitive},
	"encodeURIComponent": {Rest: EffectRead, HasRest: true, Result: EffectValuePrimitive},
	"decodeURIComponent": {Rest: EffectRead, HasRest: true, Result: EffectValuePrimitive},
	"isNaN":              {Rest: EffectRead, HasRest: true, Result: EffectValuePrimitive},
}

// InferAliasingEffects computes the aliasing effects of every instruction in function.
//
// Nested functions are NOT descended into: `Function.Functions` holds them and a caller that wants
// their effects asks for each one. That matches `InferMutableRanges`, and it is what makes the
// per-instruction result addressable, since InstructionIds are per-function and overlap between a
// function and its children. `refs.go` measured that overlap as producing wrong findings rather
// than missing ones, so the two spaces are kept apart here by construction.
//
// Returns a non-nil table for a nil function so a caller need not guard.
func InferAliasingEffects(function *Function) *AliasingEffects {
	effects := &AliasingEffects{byInstruction: map[InstructionId][]AliasingEffect{}}
	if function == nil {
		return effects
	}
	for _, instruction := range function.Instructions {
		if instruction == nil {
			continue
		}
		if list := effectsForInstruction(function, instruction); len(list) > 0 {
			effects.byInstruction[instruction.Id] = list
		}
	}
	return effects
}

// InferAliasingEffectsForNested computes an effect table for each nested function.
//
// Mirrors `InferMutableRangesForNested`, and exists for the same reason: a caller wanting the whole
// tree should not have to know that the ids restart per function.
func InferAliasingEffectsForNested(function *Function) map[FunctionId]*AliasingEffects {
	out := map[FunctionId]*AliasingEffects{}
	if function == nil {
		return out
	}
	for index, nested := range function.Functions {
		out[FunctionId(index)] = InferAliasingEffects(nested)
	}
	return out
}

// create is the Create effect, which every instruction producing a value emits first.
func create(into Place, kind EffectValueKind) AliasingEffect {
	return AliasingEffect{Kind: AliasingEffectCreate, Into: into, Value: kind}
}

// flow is one from/into effect.
func flow(kind AliasingEffectKind, from Place, into Place) AliasingEffect {
	return AliasingEffect{Kind: kind, From: from, Into: into, HasFrom: true}
}

// mutate is one mutation effect, which names only the value it writes.
func mutate(kind AliasingEffectKind, value Place) AliasingEffect {
	return AliasingEffect{Kind: kind, Into: value}
}

// effectsForInstruction is upstream's `compute_signature_for_instruction`.
//
// A pure function of one instruction, which is why this pass needs no fixpoint: see the package
// comment. Upstream caches the result per instruction for the same reason.
func effectsForInstruction(function *Function, instruction *Instruction) []AliasingEffect {
	lvalue := instruction.LValue
	switch value := instruction.Value.(type) {

	// --- Values that create something primitive and read their operands -------------------

	case *Primitive, *RegExpLiteral, *Debugger, *MetaProperty, *JsxText:
		return []AliasingEffect{create(lvalue, EffectValuePrimitive)}

	case *BinaryExpression:
		return []AliasingEffect{
			create(lvalue, EffectValuePrimitive),
			flow(AliasingEffectImmutableCapture, value.Left, lvalue),
			flow(AliasingEffectImmutableCapture, value.Right, lvalue),
		}

	case *UnaryExpression:
		return []AliasingEffect{
			create(lvalue, EffectValuePrimitive),
			flow(AliasingEffectImmutableCapture, value.Value, lvalue),
		}

	case *TemplateLiteral:
		out := []AliasingEffect{create(lvalue, EffectValuePrimitive)}
		for _, part := range value.Subexprs {
			out = append(out, flow(AliasingEffectImmutableCapture, part, lvalue))
		}
		return out

	// --- Updates, which both read and write their operand ---------------------------------

	case *PrefixUpdate:
		return []AliasingEffect{
			create(lvalue, EffectValuePrimitive),
			mutate(AliasingEffectMutate, value.Value),
		}

	case *PostfixUpdate:
		return []AliasingEffect{
			create(lvalue, EffectValuePrimitive),
			mutate(AliasingEffectMutate, value.Value),
		}

	// --- Loads and stores of locals ------------------------------------------------------

	case *LoadLocal:
		return []AliasingEffect{flow(AliasingEffectAssign, value.Place, lvalue)}

	case *LoadContext:
		return []AliasingEffect{flow(AliasingEffectAssign, value.Place, lvalue)}

	case *StoreLocal:
		return []AliasingEffect{
			flow(AliasingEffectAssign, value.Value, value.LValue),
			flow(AliasingEffectAssign, value.Value, lvalue),
		}

	case *StoreContext:
		// A write into a captured binding. Upstream widens the rvalue's range from the instruction
		// shape alone here, with no effect facts needed; `ranges.go` reproduces that independently
		// and this emits the matching Mutate so the two agree.
		return []AliasingEffect{
			mutate(AliasingEffectMutate, value.LValue),
			flow(AliasingEffectCapture, value.Value, value.LValue),
			flow(AliasingEffectAssign, value.Value, lvalue),
		}

	case *DeclareLocal:
		return []AliasingEffect{create(lvalue, EffectValueMutable)}

	case *DeclareContext:
		return []AliasingEffect{create(lvalue, EffectValueMutable)}

	case *LoadGlobal:
		// A global is Frozen rather than Mutable: writing to one is a finding upstream, which
		// `immutability.go` reports with its own reason. Measured there and reused rather than
		// re-derived.
		return []AliasingEffect{create(lvalue, EffectValueFrozen)}

	case *StoreGlobal:
		return []AliasingEffect{
			mutate(AliasingEffectMutate, value.Value),
			create(lvalue, EffectValuePrimitive),
		}

	// --- Property access ------------------------------------------------------------------

	case *PropertyLoad:
		// The loaded value may alias the object's interior, so this is a Capture rather than a
		// Read. Upstream additionally consults the checker to emit ImmutableCapture when the
		// result type is primitive; that is `EffectGapTypeDirectedShapes` and the conservative
		// direction is the one taken here.
		return []AliasingEffect{
			create(lvalue, EffectValueMutable),
			flow(AliasingEffectCapture, value.Object, lvalue),
		}

	case *ComputedLoad:
		return []AliasingEffect{
			create(lvalue, EffectValueMutable),
			flow(AliasingEffectCapture, value.Object, lvalue),
			flow(AliasingEffectImmutableCapture, value.Property, lvalue),
		}

	case *PropertyStore:
		return []AliasingEffect{
			mutate(AliasingEffectMutate, value.Object),
			flow(AliasingEffectCapture, value.Value, value.Object),
			flow(AliasingEffectAssign, value.Value, lvalue),
		}

	case *ComputedStore:
		return []AliasingEffect{
			mutate(AliasingEffectMutate, value.Object),
			flow(AliasingEffectCapture, value.Value, value.Object),
			flow(AliasingEffectImmutableCapture, value.Property, value.Object),
			flow(AliasingEffectAssign, value.Value, lvalue),
		}

	case *PropertyDelete:
		return []AliasingEffect{
			create(lvalue, EffectValuePrimitive),
			mutate(AliasingEffectMutate, value.Object),
		}

	case *ComputedDelete:
		return []AliasingEffect{
			create(lvalue, EffectValuePrimitive),
			mutate(AliasingEffectMutate, value.Object),
		}

	// --- Aggregates -----------------------------------------------------------------------

	case *ObjectExpression:
		out := []AliasingEffect{create(lvalue, EffectValueMutable)}
		for _, property := range value.Properties {
			if property.ComputedKey != nil {
				out = append(out, flow(AliasingEffectImmutableCapture, *property.ComputedKey, lvalue))
			}
			out = append(out, flow(AliasingEffectCapture, property.Value, lvalue))
		}
		return out

	case *ArrayExpression:
		out := []AliasingEffect{create(lvalue, EffectValueMutable)}
		for _, element := range value.Elements {
			if element.Hole {
				continue
			}
			if element.Spread {
				// A spread advances an iterator, which mutates it. Upstream suppresses this for
				// builtin collection types via the shape registry; without one the conservative
				// form is emitted. See EffectGapTypeDirectedShapes.
				out = append(out, mutate(AliasingEffectMutateTransitiveConditionally, element.Place))
			}
			out = append(out, flow(AliasingEffectCapture, element.Place, lvalue))
		}
		return out

	// --- Functions ------------------------------------------------------------------------

	case *FunctionExpression:
		// Creating a closure captures everything it closes over. Upstream additionally decides
		// Mutable against Frozen for the closure itself by asking whether the inner function has
		// tracked side effects; that read is what `EffectGapInterproceduralParameters` covers, and
		// Mutable is the conservative choice.
		out := []AliasingEffect{create(lvalue, EffectValueMutable)}
		for _, capture := range value.Captures {
			out = append(out, flow(AliasingEffectCapture, capture, lvalue))
		}
		return out

	case *ObjectMethod:
		return []AliasingEffect{create(lvalue, EffectValueMutable)}

	// --- Calls, which is where the signature table enters ----------------------------------

	case *CallExpression:
		return effectsForCall(function, instruction, value.Callee, value.Callee, value.Args, lvalue, true)

	case *MethodCall:
		return effectsForCall(function, instruction, value.Receiver, value.Property, value.Args, lvalue, false)

	case *NewExpression:
		return effectsForCall(function, instruction, value.Callee, value.Callee, value.Args, lvalue, false)

	case *TaggedTemplateExpression:
		// Upstream passes a Hole for the strings array then the subexpressions, and never trusts a
		// primitive return type to mean the tag is pure. Reproduced by routing through the same
		// unknown-callee default.
		args := make([]Argument, 0, len(value.Subexprs))
		for _, sub := range value.Subexprs {
			args = append(args, Argument{Place: sub})
		}
		return effectsForCall(function, instruction, value.Tag, value.Tag, args, lvalue, true)

	// --- Async and iteration ---------------------------------------------------------------

	case *Await:
		// An await is where a value crosses a suspension point, and upstream treats the awaited
		// value as conditionally mutated because anything may have run in between. This is the
		// effect a rule asking "was this mutated after crossing an async boundary" reads.
		return []AliasingEffect{
			create(lvalue, EffectValueMutable),
			mutate(AliasingEffectMutateTransitiveConditionally, value.Value),
			flow(AliasingEffectCapture, value.Value, lvalue),
		}

	case *GetIterator:
		return []AliasingEffect{
			create(lvalue, EffectValueMutable),
			mutate(AliasingEffectMutateTransitiveConditionally, value.Value),
			flow(AliasingEffectCapture, value.Value, lvalue),
		}

	case *IteratorNext:
		return []AliasingEffect{
			create(lvalue, EffectValueMutable),
			mutate(AliasingEffectMutate, value.Iterator),
			flow(AliasingEffectCapture, value.Collection, lvalue),
		}

	case *NextPropertyOf:
		return []AliasingEffect{
			create(lvalue, EffectValuePrimitive),
			flow(AliasingEffectImmutableCapture, value.Value, lvalue),
		}

	// --- JSX ---------------------------------------------------------------------------------

	case *JsxExpression:
		// Every value interpolated into JSX is FROZEN, which is the fact `immutability.go` reports
		// on and the reason a value read by React cannot be written afterwards. This is the one
		// React-flavoured effect in the set, and it generalises: Freeze means "must not be mutated
		// past this point", which is also what a readonly type and Object.freeze mean.
		out := []AliasingEffect{create(lvalue, EffectValueFrozen)}
		if value.Tag.Place != nil {
			out = append(out, mutate(AliasingEffectFreeze, *value.Tag.Place))
		}
		for _, prop := range value.Props {
			out = append(out, mutate(AliasingEffectFreeze, prop.Value))
			out = append(out, flow(AliasingEffectCapture, prop.Value, lvalue))
		}
		for _, child := range value.Children {
			out = append(out, mutate(AliasingEffectFreeze, child))
			out = append(out, flow(AliasingEffectCapture, child, lvalue))
		}
		return out

	case *JsxFragment:
		out := []AliasingEffect{create(lvalue, EffectValueFrozen)}
		for _, child := range value.Children {
			out = append(out, mutate(AliasingEffectFreeze, child))
			out = append(out, flow(AliasingEffectCapture, child, lvalue))
		}
		return out

	// --- Everything else -------------------------------------------------------------------

	case *TypeCastExpression:
		return []AliasingEffect{flow(AliasingEffectAssign, value.Value, lvalue)}

	case *Destructure:
		// Destructuring reads the source and binds each target. The individual targets are separate
		// instructions in this representation, so only the source edge belongs here.
		return []AliasingEffect{flow(AliasingEffectCapture, value.Value, lvalue)}
	}

	return nil
}

// effectsForCall is upstream's Apply resolution: consult the signature table, else the default.
//
// receiver and callee differ only for a method call, where the receiver is the object and the
// callee is the property. mutatesCallee reproduces upstream's `mutates_function`, which is true for
// a plain call and false for a method call or a construction; it decides whether the callee value
// itself is treated as mutated by being invoked.
func effectsForCall(
	function *Function,
	instruction *Instruction,
	receiver Place,
	callee Place,
	args []Argument,
	lvalue Place,
	mutatesCallee bool,
) []AliasingEffect {
	name := calleeName(function, instruction, callee)
	signature, known := lookupSignature(function, instruction, name)
	if !known {
		return effectsForUnknownCall(receiver, callee, args, lvalue, mutatesCallee)
	}

	if len(signature.Aliasing) > 0 {
		if applied, ok := effectsFromAliasingSignature(signature, receiver, args, lvalue); ok {
			return applied
		}
		// Upstream returns nil from `computeEffectsForSignature` when the arity does not fit and
		// then falls through to the legacy path (`:1087`). Reproduced rather than raising.
	}

	out := []AliasingEffect{create(lvalue, signature.Result)}

	// Upstream aliases the receiver into the result unless the receiver's own effect is Capture,
	// in which case the capture bookkeeping below handles it
	// (`infer_mutation_aliasing_effects.rs:2483-2485`).
	if signature.Receiver != EffectCapture {
		out = append(out, flow(AliasingEffectAlias, receiver, lvalue))
	}

	var stores []Place
	var captures []Place
	out = visitSignatureOperand(out, receiver, signature.Receiver, lvalue, &stores, &captures)

	for index, argument := range args {
		effect := signature.Rest
		if !argument.Spread && index < len(signature.Positional) {
			effect = signature.Positional[index]
		} else if !signature.HasRest && index >= len(signature.Positional) {
			// A signature that names fewer positionals than the call passes, with no rest, gets the
			// conservative default for the extras. Upstream's `rest_param.unwrap_or` does exactly
			// this with ConditionallyMutate.
			effect = EffectConditionallyMutate
		}
		if argument.Spread && effect != EffectMutate && effect != EffectConditionallyMutate {
			// Upstream's `get_argument_effect`: a spread of anything other than an already-mutating
			// effect becomes ConditionallyMutateIterator, because spreading advances an iterator.
			effect = EffectConditionallyMutateIterator
		}
		out = visitSignatureOperand(out, argument.Place, effect, lvalue, &stores, &captures)
	}

	// Upstream's capture reconciliation: a captured operand aliases the result when nothing was
	// stored, and is captured into each store when something was
	// (`infer_mutation_aliasing_effects.rs:2508-2520`).
	if len(captures) > 0 {
		if len(stores) == 0 {
			for _, capture := range captures {
				out = append(out, flow(AliasingEffectAlias, capture, lvalue))
			}
		} else {
			for _, capture := range captures {
				for _, store := range stores {
					out = append(out, flow(AliasingEffectCapture, capture, store))
				}
			}
		}
	}
	return out
}

// visitSignatureOperand is upstream's `visit` closure inside compute_effects_for_legacy_signature.
//
// It translates one scalar signature Effect into the AliasingEffect it implies. This is the ONE
// place in this file where a scalar `Effect` is read rather than produced, and it reads it as an
// INPUT off the signature table, which is exactly how upstream uses the type. See the package
// comment for why that direction matters.
func visitSignatureOperand(
	out []AliasingEffect,
	place Place,
	effect Effect,
	lvalue Place,
	stores *[]Place,
	captures *[]Place,
) []AliasingEffect {
	switch effect {
	case EffectStore:
		out = append(out, mutate(AliasingEffectMutate, place))
		*stores = append(*stores, place)
	case EffectCapture:
		*captures = append(*captures, place)
	case EffectConditionallyMutate:
		out = append(out, mutate(AliasingEffectMutateTransitiveConditionally, place))
	case EffectConditionallyMutateIterator:
		// Upstream suppresses the mutation for a builtin collection type and keeps the capture. No
		// shape registry here, so the conservative form is emitted. See EffectGapTypeDirectedShapes.
		out = append(out, mutate(AliasingEffectMutateTransitiveConditionally, place))
		out = append(out, flow(AliasingEffectCapture, place, lvalue))
	case EffectFreeze:
		out = append(out, mutate(AliasingEffectFreeze, place))
	case EffectMutate:
		out = append(out, mutate(AliasingEffectMutateTransitive, place))
	case EffectRead:
		out = append(out, flow(AliasingEffectImmutableCapture, place, lvalue))
	}
	return out
}

// effectsForUnknownCall is upstream's no-signature default, and it is NOT "no effect".
//
// `infer_mutation_aliasing_effects.rs:1823-1892`. Every operand is conditionally mutated, maybe
// aliases into the result, and captures into every OTHER operand. That last pairing is what makes
// `foo(a, b)` record that `a` may now hold `b`, which is the escape half of the analysis.
//
// This is the path 69.2% of named call sites on Kirk's tree take, measured, so its shape matters
// more than any single table entry.
func effectsForUnknownCall(
	receiver Place,
	callee Place,
	args []Argument,
	lvalue Place,
	mutatesCallee bool,
) []AliasingEffect {
	out := []AliasingEffect{create(lvalue, EffectValueMutable)}

	operands := make([]Place, 0, len(args)+2)
	operands = append(operands, receiver)
	if callee.Identifier != receiver.Identifier {
		operands = append(operands, callee)
	}
	for _, argument := range args {
		operands = append(operands, argument.Place)
	}

	for _, operand := range operands {
		// Upstream compares by reference identity, so for a plain call where receiver IS the
		// callee, both are skipped when the call does not mutate its function. Reproduced by
		// comparing identifiers, which is the same decision in this representation because the two
		// places are built from one identifier.
		if operand.Identifier == callee.Identifier && !mutatesCallee {
			// Do not treat the callee as mutated by being called.
		} else {
			out = append(out, mutate(AliasingEffectMutateTransitiveConditionally, operand))
		}
		out = append(out, flow(AliasingEffectMaybeAlias, operand, lvalue))
		for _, other := range operands {
			if other.Identifier == operand.Identifier {
				continue
			}
			out = append(out, flow(AliasingEffectCapture, operand, other))
		}
	}
	return out
}

// lookupSignature finds a callee's signature by name.
//
// A method name is looked up in the method table and a bare callee in the function table, kept
// apart for the reason effectGlobalFunctions gives. Returns false for anything unknown, which sends
// the call to the conservative default.
func lookupSignature(function *Function, instruction *Instruction, name string) (effectSignature, bool) {
	if name == "" {
		return effectSignature{}, false
	}
	switch instruction.Value.(type) {
	case *MethodCall:
		// Receiver-qualified first. Three of the four names it carries also exist in the bare table
		// meaning something else, so consulting the bare table first would answer with the Array
		// method and never reach this one.
		if receiver := receiverSyntaxName(instruction); receiver != "" {
			if signature, found := effectQualifiedMethods[receiver+"."+name]; found {
				return signature, true
			}
		}
		signature, found := effectSignatures[name]
		return signature, found
	default:
		signature, found := effectGlobalFunctions[name]
		return signature, found
	}
}

// receiverSyntaxName recovers the source name of a method call's receiver, for the qualified table.
//
// Syntax rather than the identifier, matching `calleeSyntaxName`: a receiver that is a global is a
// `LoadGlobal` into a temporary here, so `Identifier.Name` is empty for exactly the population this
// table is about. Returns empty for anything that is not a plain `receiver.method(...)`, which sends
// the lookup to the bare-name table.
func receiverSyntaxName(instruction *Instruction) string {
	if instruction == nil || instruction.Node == nil {
		return ""
	}
	node := instruction.Node
	if node.Kind != ast.KindCallExpression {
		return ""
	}
	expression := node.Expression()
	if expression == nil || expression.Kind != ast.KindPropertyAccessExpression {
		return ""
	}
	receiver := expression.Expression()
	if receiver == nil || receiver.Kind != ast.KindIdentifier {
		return ""
	}
	return receiver.Text()
}

// calleeName recovers what the source calls this callee.
//
// A plain call's callee is a NAMELESS TEMPORARY in this representation, which is a substrate fact
// that cost a wrong measurement before it was noticed: a first probe reported zero distinct callee
// names against 57,446 named call sites, which reads like a broken tally rather than like the
// identifier space it actually is. So the value's own name is asked first and the syntax second.
//
// Every `Node.Xxx()` accessor panics off its kind, so each descent is guarded by a kind check
// rather than by a nil check alone.
func calleeName(function *Function, instruction *Instruction, callee Place) string {
	if function != nil && int(callee.Identifier) < len(function.Identifiers) {
		if identifier := function.Identifiers[callee.Identifier]; identifier != nil && identifier.Name != "" {
			return identifier.Name
		}
	}
	return calleeSyntaxName(instruction)
}

// calleeSyntaxName reads a call's callee name off the syntax.
func calleeSyntaxName(instruction *Instruction) string {
	if instruction == nil || instruction.Node == nil {
		return ""
	}
	node := instruction.Node
	if node.Kind != ast.KindCallExpression && node.Kind != ast.KindNewExpression {
		return ""
	}
	expression := node.Expression()
	if expression == nil {
		return ""
	}
	switch expression.Kind {
	case ast.KindIdentifier:
		return expression.Text()
	case ast.KindPropertyAccessExpression:
		name := expression.Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			return ""
		}
		return name.Text()
	}
	return ""
}

// ProjectEffects computes the scalar `Effect` of every operand of one instruction.
//
// This is the bridge from this pass's real output to the `Place.Effect` field, and it is a faithful
// port of `infer_mutation_aliasing_ranges.rs:856-901` -- which lives in the RANGES pass, not this
// one. It is exposed here rather than there because the mapping is a property of the
// `AliasingEffect` variants, which this file owns, and because a caller that has effects but no
// ranges can still ask for the subset that does not depend on them.
//
// # It genuinely needs mutable ranges, and that is the reason Place.Effect is not written here
//
// The five aliasing variants all give the SOURCE either `EffectCapture` or `EffectRead`, and the
// discriminator is `env.identifiers[into.identifier].mutable_range.end > eval_order` -- whether the
// target is still being written after this point. There is no way to answer that from the effect
// list alone. Pass a nil `ranges` and every aliasing edge resolves to `EffectRead`, which is the
// under-approximating half; pass a real table and it resolves exactly.
//
// The direction of the error with nil ranges is stated because a consumer must know it: `EffectRead`
// where upstream says `EffectCapture` means "not retained" where upstream says "retained", which
// UNDER-reports escape. It never invents a capture upstream does not have.
//
// # This deliberately does not write into the graph
//
// It returns a map rather than assigning `place.Effect`, because the lowered `Function` is shared
// through the per-file cache and several rules hold the same pointer. Upstream mutates its graph in
// place and can, because it owns the whole compilation; here a pass that wrote effects onto shared
// places would make one rule's analysis visible to a rule that never ran it, and `Construct` is
// already documented as not idempotent. A caller wanting the field populated copies the place.
func ProjectEffects(effects []AliasingEffect, ranges *MutableRanges, order EvaluationOrder) map[IdentifierId]Effect {
	out := map[IdentifierId]Effect{}
	for _, effect := range effects {
		switch {
		case effect.Kind.IsAliasing():
			// Upstream sets BOTH sides here, and the target always gets Store regardless of the
			// range test. Only the source's effect varies.
			stillMutable := ranges != nil && ranges.Get(effect.Into.Identifier).End > order
			if stillMutable {
				out[effect.From.Identifier] = EffectCapture
			} else {
				out[effect.From.Identifier] = EffectRead
			}
			out[effect.Into.Identifier] = EffectStore

		case effect.Kind == AliasingEffectMutate:
			out[effect.Into.Identifier] = EffectStore

		case effect.Kind == AliasingEffectMutateTransitive,
			effect.Kind == AliasingEffectMutateConditionally,
			effect.Kind == AliasingEffectMutateTransitiveConditionally:
			// All three collapse to ConditionallyMutate upstream. That is a real narrowing in the
			// projection rather than in this pass: the AliasingEffect list keeps the three apart
			// and only the scalar loses them, which is one more reason the list is the output that
			// matters.
			out[effect.Into.Identifier] = EffectConditionallyMutate

		case effect.Kind == AliasingEffectFreeze:
			out[effect.Into.Identifier] = EffectFreeze

		case effect.Kind == AliasingEffectCreate, effect.Kind == AliasingEffectApply:
			// Upstream treats Create as a no-op here and raises an invariant on a surviving Apply.
			// This pass never emits an Apply, so the arm exists to keep the switch total rather
			// than because it is reachable; a later addition that emits one gets Read rather than
			// a silent drop.

		case effect.Kind == AliasingEffectImmutableCapture:
			// Explicitly a no-op upstream: Read is the default and writing it would be the same.
		}
	}
	return out
}

// effectsFromAliasingSignature applies an aliasing signature by substituting its operands.
//
// Upstream's `computeEffectsForSignature` (`InferMutationAliasingEffects.ts:2563`), reduced to what
// the static table needs. Upstream builds a substitution map from placeholder id to place and then
// rewrites each effect through it; the placeholders in the transcribed entries are only the
// receiver, the positional params and the return value, so this substitutes directly.
//
// Returns false when the call does not fit the signature's arity, which is upstream's `return null`
// at `:2580`. The caller then takes the legacy path, which is upstream's own fallback rather than a
// softening of it.
//
// # What is NOT ported, and why it costs nothing here
//
// Upstream also handles a rest parameter, spread arguments, signature temporaries, and a dynamic
// context array for signatures built from function expressions. None of the transcribed entries uses
// any of them: they are static two-effect signatures over a receiver, one argument and a result. An
// entry that needs those must extend this rather than be added silently, which the arity check
// enforces by declining any call whose argument count the signature does not name.
func effectsFromAliasingSignature(signature effectSignature, receiver Place, args []Argument,
	lvalue Place) ([]AliasingEffect, bool) {
	positional := 0
	for _, effect := range signature.Aliasing {
		for _, operand := range [2]aliasingOperand{effect.From, effect.Into} {
			if operand >= aliasingParam0 {
				if index := int(operand-aliasingParam0) + 1; index > positional {
					positional = index
				}
			}
		}
	}
	if positional > len(args) {
		return nil, false
	}
	for index := 0; index < positional; index++ {
		if args[index].Spread {
			// A spread makes the positional binding meaningless: the argument at that position is
			// not one place. Upstream routes spreads to the rest param, which no transcribed entry
			// has, so declining is the same answer.
			return nil, false
		}
	}

	resolve := func(operand aliasingOperand) (Place, bool) {
		switch {
		case operand == aliasingReceiver:
			return receiver, true
		case operand == aliasingReturns:
			return lvalue, true
		default:
			index := int(operand - aliasingParam0)
			if index >= len(args) {
				return Place{}, false
			}
			return args[index].Place, true
		}
	}

	out := make([]AliasingEffect, 0, len(signature.Aliasing))
	for _, effect := range signature.Aliasing {
		into, ok := resolve(effect.Into)
		if !ok {
			return nil, false
		}
		if effect.Kind == AliasingEffectCreate {
			out = append(out, create(into, effect.Value))
			continue
		}
		from, ok := resolve(effect.From)
		if !ok {
			return nil, false
		}
		out = append(out, flow(effect.Kind, from, into))
	}
	return out, true
}
