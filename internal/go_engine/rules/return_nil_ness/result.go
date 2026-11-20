package return_nil_ness

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Kind keys nilness's verdicts in each package the resolver holds, and its counts.
//
// It records every judgment (fact_resolver.Kind.RecordsEveryJudgment), set as the package initializes, beside its
// order bound. Both walks hand the resolver each verdict they keep as they keep it (keepAnswer), and another package's
// function is asked through fact_resolver.Ask, which holds the answer. A judgment reads only the instructions of the
// function it judges: of a function that one calls it asks its walk, whose answer is a verdict, the resolver's
// answer, or the defaults of the callee's signature, read from no body, for a function begun, one with no object, and
// one the push holds no fact of yet. Two verdicts are handed to no resolver, and neither is a body read with no
// record:
//
//   - a function none of whose results may be nil is answered the defaults of its signature before a block of its
//     body is read, by both walks, so no answer rests on its body;
//   - a verdict the demand walk judged after it met a call whose answer hangs on the push's order is thrown away with
//     the answers built on it, never stored, and the replay judges what the question needs again and hands over each
//     verdict it keeps (judgeIn).
//
// TestEveryVerdictNilnessHoldsIsAnAnswerTheResolverHolds pins the first claim, and check's
// TestNoAnswerOfARecordingKindRestsOnABodyWithNoAnswer the second.
var Kind = &fact_resolver.Kind{Name: "return_nil_ness"}

func init() {
	// Set here rather than in Kind's literal: OrderBound reads the kind's state under Kind, and a literal naming it
	// would make Kind's initializer depend on itself, which Go refuses as an initialization cycle.
	Kind.OrderBound = OrderBound
	Kind.RecordsEveryJudgment = true
	fact_resolver.Register(Kind, whole,
		func(resolver *fact_resolver.Resolver, home *fact_resolver.Package, object types.Object) []ValueNilness {
			function, isFunction := object.(*types.Func)
			if !isFunction {
				return nil
			}
			return heldResults(resolver, home, function)
		}, bodilessResults)
}

// bodilessResults is the nilness of the results of object, a function declared without a body, as a fact holds it,
// from its types alone (fact_resolver.Ask): judgeIn's verdict of a function with no blocks is the defaults of its
// signature, which heldResults holds only when they say more than the defaults, so never.
func bodilessResults(object types.Object) []ValueNilness {
	function, isFunction := object.(*types.Func)
	if !isFunction {
		return nil
	}
	results := defaultNilnessForSignature(function.Signature())
	if !interesting(function.Signature(), results) {
		return nil
	}
	return results
}

// Result is one package's nilness of results, as its readers ask it.
type Result struct {
	resolver *fact_resolver.Resolver
	reader   *fact_resolver.Package

	// asking is the row under `--timing` whose questions these are, empty until Asking names it.
	asking string

	// asker is the body whose rule asks, which the resolver records beside each answer, nil until For names it.
	asker *fact_resolver.Asker
}

// Asking is the package's nilness of results as the rule or analyzer whose `--timing` row is named asking asks it,
// so the resolver's work for it is billed to the resolver's rows rather than to that one (fact_resolver.Question).
func (judged *Result) Asking(asking string) *Result {
	return &Result{resolver: judged.resolver, reader: judged.reader, asking: asking}
}

// For is the package's nilness of results as its rule asks while it checks function, so the resolver records function's
// body, or the declaration a literal of it is in, as the asker of each answer (fact_resolver.Resolver.RecordAskers,
// #5vbs1np). Where the resolver records no asker, the result itself is handed back, and nothing is made.
func (judged *Result) For(function *intermediate_representation.Function) *Result {
	if !judged.resolver.RecordsAskers() {
		return judged
	}
	asker := fact_resolver.AskerOf(function)
	return &Result{resolver: judged.resolver, reader: judged.reader, asking: judged.asking, asker: &asker}
}

// Nilness is the nilness of the result at index result of function: {_ NeverNil} for a result that cannot be nil,
// {MaybeNil MaybeNil} for a function the push held no fact of here, and otherwise what the fact holds, normalized, as
// staticcheck's nilness.Result answers it.
func (judged *Result) Nilness(function *types.Func, result int) ValueNilness {
	resultType := function.Signature().Results().At(result).Type()
	if !typeutil.MaybePointerLike(resultType) {
		return ValueNilness{Outer: NeverNil}
	}
	var results []ValueNilness
	var holds bool
	judged.resolver.NoteAsker(Kind, function, judged.asker)
	judged.resolver.Question(judged.asking, func() { results, holds = ask(judged.resolver, judged.reader, function) })
	if !holds {
		return ValueNilness{Inner: MaybeNil, Outer: MaybeNil}
	}
	return normalize(results[result], resultType)
}

// ask is the nilness of function's results and whether a reader in reader holds them as the push did, as a fact: the
// fact crossed here (fact_resolver.Visible) and says more than the defaults. Its package's answer comes through
// fact_resolver.Ask, the results of a fact, nil for none: the store's, or judged in its own package, under that
// package's lock, once, and each answer after is the verdict kept.
func ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) ([]ValueNilness, bool) {
	if !fact_resolver.Visible(reader, function) {
		return nil, false
	}
	results := fact_resolver.Ask(resolver, reader, Kind, function, func(home *fact_resolver.Package) []ValueNilness {
		return heldResults(resolver, home, function)
	}, func() []ValueNilness { return bodilessResults(function) })
	return results, results != nil
}

// heldResults is the nilness of function's results, of home, as a fact holds it: nil when it says no more than the
// defaults.
func heldResults(resolver *fact_resolver.Resolver, home *fact_resolver.Package, function *types.Func) []ValueNilness {
	judged, holds := judgeIn(resolver, home, function)
	if !holds {
		return nil
	}
	return judged
}

// judgeIn is the nilness of function's results, of home, and whether they say more than the defaults, judged there
// under home's lock, or the verdict kept.
func judgeIn(
	resolver *fact_resolver.Resolver,
	home *fact_resolver.Package,
	function *types.Func,
) ([]ValueNilness, bool) {
	home.Lock()
	defer home.Unlock()
	state := stateIn(home)
	if results, judged := state.results[function]; judged {
		return results, interesting(function.Signature(), results)
	}
	declared := home.Function(function)
	if declared == nil {
		// No declared function of the package, as an instantiated method's object is not, which the push never
		// judged and so never held a fact of.
		return nil, false
	}
	walk := &demandWalk{resolver: resolver, home: home, irPackage: home.IntermediateRepresentation(), state: state}
	results := walk.verdict(declared)
	if walk.orderDependent {
		resolver.Counts(Kind).Fallbacks.Add(1)
		state.replayUntil(resolver, home, declared)
		results = state.results[function]
	}
	return results, interesting(function.Signature(), results)
}

// normalize fills what value leaves unknown with MaybeNil, and Inner too for a value of valueType that is no interface,
// where Inner means nothing.
func normalize(value ValueNilness, valueType types.Type) ValueNilness {
	if value.Inner == 0 || !types.IsInterface(valueType) {
		value.Inner = MaybeNil
	}
	if value.Outer == 0 {
		value.Outer = MaybeNil
	}
	return value
}
