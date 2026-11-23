package function_triviality

import (
	"fmt"
	"slices"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Kind keys triviality's answers in the resolver, and its counts.
//
// It is deliberately not registered with fact_resolver.Register (#h0k644s), so no package is computed whole in it.
// Registered, it would be judged for every function of every package a run saves an entry of, the standard library's
// entries in the system cache among them, and each of those values names files by absolute path. Nothing needs it
// whole, since only a package's own reader asks, of a package the run holds from source. An answer a reader asked
// for, about a function with an objectpath, is still recorded (fact_resolver.Ask), may be kept in the package's entry,
// and a store may give it back on a later run. What keeps an answer given back right is the key that entry is under,
// which holds each of the package's files by its path and contents, so an entry found under the same key names the
// same files at the same offsets.
var Kind = &fact_resolver.Kind{Name: "function_triviality", OrderBound: orderFree}

// orderFree is triviality's order bound (fact_resolver.Kind.OrderBound): none, and known. Each answer is one walk of
// IsTrivial from the asked function with a seen set of its own, reading no other answer (see judgeIn), so no answer
// about any function of home hangs on the order its questions came in.
func orderFree(*fact_resolver.Package, []*types.Func) ([]*types.Func, bool) {
	return nil, true
}

// bodilessTriviality is the answer about a function declared without a body, from its types alone (fact_resolver.Ask):
// trivial, having read no other body. Built from its declaration, it has its syntax and no blocks, so the walk finds
// no instruction in it, and none is work.
func bodilessTriviality(types.Object) triviality {
	return triviality{Trivial: true}
}

// Result is one package's triviality, as its readers ask it.
type Result struct {
	resolver *fact_resolver.Resolver
	reader   *fact_resolver.Package

	// asking is the row under `--timing` whose questions these are, empty until Asking names it.
	asking string

	// asker is the body whose rule asks, which the resolver records beside each answer, nil until For names it.
	asker *fact_resolver.Asker
}

// Asking is the package's triviality as the rule or analyzer whose `--timing` row is named asking asks it, so the
// resolver's work for it is billed to the resolver's rows rather than to that one (fact_resolver.Question).
func (result *Result) Asking(asking string) *Result {
	return &Result{resolver: result.resolver, reader: result.reader, asking: asking}
}

// For is the package's triviality as its rule asks while it checks function, so the resolver records function's body,
// or the declaration a literal of it is in, as the asker of each answer (fact_resolver.Resolver.RecordAskers,
// #5vbs1np). Where the resolver records no asker, the result itself is handed back, and nothing is made.
func (result *Result) For(function *intermediate_representation.Function) *Result {
	if !result.resolver.RecordsAskers() {
		return result
	}
	asker := fact_resolver.AskerOf(function)
	return &Result{resolver: result.resolver, reader: result.reader, asking: result.asking, asker: &asker}
}

// IsTrivial is whether function, a function or method the package itself declares, is trivial, as
// intermediate_representation.IsTrivial answers of it. Asked of another package's function it panics: that one has no
// syntax where a reader walks, which IsTrivial answers not trivial, so a reader takes it as not trivial and never asks.
func (result *Result) IsTrivial(function *types.Func) bool {
	var answer triviality
	result.resolver.NoteAsker(Kind, function, result.asker)
	result.resolver.Question(result.asking, func() { answer = ask(result.resolver, result.reader, function) })
	return answer.Trivial
}

// ask is function's answer, a function of reader's own package, through fact_resolver.Ask: judged there under the
// package's lock, once, and each answer after is the one kept.
func ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, function *types.Func) triviality {
	if function.Pkg() != reader.Types {
		// Invariant: a reader asks only of its own package's functions. Asking of another's is a mistake in the
		// reader, never something a package can cause, and the dispatcher's recover makes it that rule's crash.
		panic(fmt.Sprintf("function_triviality: %s asked of %s, a function of another package", reader.Types.Path(),
			function.FullName()))
	}
	return fact_resolver.Ask(resolver, reader, Kind, function, func(home *fact_resolver.Package) triviality {
		return judgeIn(resolver, home, function)
	}, func() triviality { return bodilessTriviality(function) })
}

// judgeIn is function's answer, of home, judged there under home's lock: one walk of IsTrivial from it, and the place
// of every other top-level declaration whose body the walk read, a function literal's being the declaration that
// encloses it.
//
// The answer is the one a reader's own walk gives. A reader walks the package where every other package is made from
// its types alone, so another package's function has no syntax there and the walk refuses it at once: not trivial,
// reading no further. Where home builds in the resolver's own program instead, other packages have their syntax, so
// the walk is told to refuse every function whose top-level declaration another package holds, and it ends exactly
// where and how a reader's walk ends: not trivial, with the places entered before it. No function of another package
// is entered or built, which matters beyond the answer: its body may name a package home does not import, which the
// resolver's program never created for home, and a build of that body panics.
func judgeIn(resolver *fact_resolver.Resolver, home *fact_resolver.Package, function *types.Func) triviality {
	home.Lock()
	defer home.Unlock()
	asked := home.Function(function)
	if asked == nil {
		// No function of the package in its program, as an interface's method has none: no body to walk, so not
		// trivial, as the walk takes a call with no static callee. An instantiated method's object never reaches
		// here, since fact_resolver.Ask answers it with no fact before it computes.
		return triviality{}
	}
	trivial, entered := intermediate_representation.IsTrivialEntering(asked,
		func(met *intermediate_representation.Function) bool {
			object := declaringFunction(met).Object()
			return object != nil && object.Pkg() != home.Types
		})
	resolver.Counts(Kind).Computed.Add(1)
	answer := triviality{Trivial: trivial}
	own := placeOf(asked)
	for _, read := range entered {
		if place := placeOf(declaringFunction(read)); place != own && !slices.Contains(answer.Entered, place) {
			answer.Entered = append(answer.Entered, place)
		}
	}
	slices.SortFunc(answer.Entered, comparePlaces)
	return answer
}
