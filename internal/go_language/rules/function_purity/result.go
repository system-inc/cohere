package function_purity

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
)

// Kind keys purity's verdicts in each package the resolver holds, and its counts.
//
// It records every judgment (fact_resolver.Kind.RecordsEveryJudgment). A verdict is made in one place, demandWalk's
// verdict, which hands it to the resolver as it stores it, and another package's function is asked through
// fact_resolver.Ask, which holds the answer. And judge reads only the instructions of the function it judges: of a
// function that one calls it asks its callees, whose answer is a verdict, the resolver's answer, or not pure for a
// function with no object, an anonymous one, read no further. TestEveryVerdictPurityHoldsIsAnAnswerTheResolverHolds
// pins the first, and check's TestNoAnswerOfARecordingKindRestsOnABodyWithNoAnswer the second.
var Kind = &fact_resolver.Kind{
	Name:                 "function_purity",
	OrderBound:           orderFree,
	TypesAlone:           typesAlone,
	RecordsEveryJudgment: true,
}

// orderFree is purity's order bound (fact_resolver.Kind.OrderBound): none, and known. Purity is the least set closed
// under its rules, a cycle of calls never pure, which is one set whatever order a walk finds it in (see demandWalk), so
// no answer about any function of home hangs on the order its walks took.
func orderFree(*fact_resolver.Package, []*types.Func) ([]*types.Func, bool) {
	return nil, true
}

// typesAlone is whether purity's answer about object is its types' alone (fact_resolver.Kind.TypesAlone): a function
// with no results that pureStandardLibrary does not list, an `init` function among them, which the package's
// initializer calls. judge answers not pure for one before it reads a block of its body, a stub is not pure either,
// and nor is a function declared without a body (bodilessPurity), so the answer is not pure in every build that
// declares the function with no results, whatever the build makes of its body.
func typesAlone(object types.Object) bool {
	function, isFunction := object.(*types.Func)
	if !isFunction || function.Signature().Results().Len() != 0 {
		return false
	}
	_, listed := pureStandardLibrary[function.FullName()]
	return !listed
}

func init() {
	fact_resolver.Register(Kind, whole,
		func(resolver *fact_resolver.Resolver, home *fact_resolver.Package, object types.Object) bool {
			function, isFunction := object.(*types.Func)
			return isFunction && judgeIn(resolver, home, function)
		}, bodilessPurity)
}

// bodilessPurity is whether a function declared without a body is pure, from its types alone (fact_resolver.Ask):
// never. Built from its declaration, it has no blocks and its syntax, so judge reads it as a stub, which
// intermediate_representation.IsStub finds no instruction in, before it reads pureStandardLibrary: even a listed one
// is not pure.
func bodilessPurity(types.Object) bool {
	return false
}

// Result is one package's purity, as its readers ask it.
type Result struct {
	resolver *fact_resolver.Resolver
	reader   *fact_resolver.Package

	// asking is the row under `--timing` whose questions these are, empty until Asking names it.
	asking string

	// asker is the body whose rule asks, which the resolver records beside each answer, nil until For names it.
	asker *fact_resolver.Asker
}

// Asking is the package's purity as the rule or analyzer whose `--timing` row is named asking asks it, so the
// resolver's work for it is billed to the resolver's rows rather than to that one (fact_resolver.Question).
func (result *Result) Asking(asking string) *Result {
	return &Result{resolver: result.resolver, reader: result.reader, asking: asking}
}

// For is the package's purity as its rule asks while it checks function, so the resolver records function's body, or
// the declaration a literal of it is in, as the asker of each answer (fact_resolver.Resolver.RecordAskers, #5vbs1np).
// Where the resolver records no asker, the result itself is handed back, and nothing is made.
func (result *Result) For(function *intermediate_representation.Function) *Result {
	if !result.resolver.RecordsAskers() {
		return result
	}
	asker := fact_resolver.AskerOf(function)
	return &Result{resolver: result.resolver, reader: result.reader, asking: result.asking, asker: &asker}
}

// IsPure is whether function is pure as this package's readers hold it: the package's own function judged pure, or
// another package's whose fact the checker would have carried here (fact_resolver.Visible) and that is pure there.
func (result *Result) IsPure(function *types.Func) bool {
	var pure bool
	result.resolver.NoteAsker(Kind, function, result.asker)
	result.resolver.Question(result.asking, func() { pure = ask(result.resolver, result.reader, function) })
	return pure
}

// ask is whether function is pure as a reader in reader holds it, its package's answer through fact_resolver.Ask: the
// store's, or judged in its own package, under that package's lock, once, and each answer after is the verdict kept.
func ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) bool {
	if !fact_resolver.Visible(reader, function) {
		return false
	}
	return fact_resolver.Ask(resolver, reader, Kind, function, func(home *fact_resolver.Package) bool {
		return judgeIn(resolver, home, function)
	}, func() bool { return bodilessPurity(function) })
}

// judgeIn is whether function, of home, is pure, judged there under home's lock, or the verdict kept.
func judgeIn(resolver *fact_resolver.Resolver, home *fact_resolver.Package, function *types.Func) bool {
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[verdicts](home, Kind)
	if pure, judged := state.pure[function]; judged {
		return pure
	}
	declared := home.Function(function)
	if declared == nil {
		// No declared function of the package, as an instantiated method's object is not, which the push never
		// judged and so never held pure.
		return false
	}
	return state.walk(resolver, home).verdict(declared)
}
