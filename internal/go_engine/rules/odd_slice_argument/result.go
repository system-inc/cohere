package odd_slice_argument

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Kind keys SA5012's marks in each package the resolver holds, and its counts.
var Kind = &fact_resolver.Kind{Name: "odd_slice_argument"}

func init() {
	fact_resolver.Register(Kind, whole,
		func(resolver *fact_resolver.Resolver, home *fact_resolver.Package, object types.Object) bool {
			parameter, isParameter := object.(*types.Var)
			return isParameter && markIn(resolver, home, parameter)
		}, bodilessMark)
}

// bodilessMark is whether a parameter of a function declared without a body is marked, from its types alone
// (fact_resolver.Ask): never. A parameter is marked only by a check or a call in its function's blocks, and such a
// function, built from its declaration, has none.
func bodilessMark(types.Object) bool {
	return false
}

// Result is one package's marks, as its readers ask them: every mark a reader holds, the package's own among them, is
// the run's fact resolver's answer, judged where the parameter is declared and only when asked.
type Result struct {
	resolver *fact_resolver.Resolver
	reader   *fact_resolver.Package

	// asking is the row under `--timing` whose questions these are, empty until Asking names it.
	asking string
}

// Asking is the package's marks as the rule or analyzer whose `--timing` row is named asking asks them, so the
// resolver's work for it is billed to the resolver's rows rather than to that one (fact_resolver.Question).
func (marks *Result) Asking(asking string) *Result {
	return &Result{resolver: marks.resolver, reader: marks.reader, asking: asking}
}

// Marked is whether parameter is marked as this package's readers hold it: the package's own parameter, or another
// package's whose mark the checker would have carried here (fact_resolver.Visible), that its function needs of even
// length.
func (marks *Result) Marked(parameter *types.Var) bool {
	var marked bool
	marks.resolver.Question(marks.asking, func() { marked = ask(marks.resolver, marks.reader, parameter) })
	return marked
}

// ask is whether parameter is marked as a reader in reader holds it: the mark crossed here (fact_resolver.Visible), and
// parameter's function marks it. Its package's answer comes through fact_resolver.Ask: the store's, or the function
// judged in its own package, under that package's lock, once, and each answer after is the mark kept.
func ask(resolver *fact_resolver.Resolver, reader *fact_resolver.Package, parameter *types.Var) bool {
	if !fact_resolver.Visible(reader, parameter) {
		return false
	}
	return fact_resolver.Ask(resolver, reader, Kind, parameter, func(home *fact_resolver.Package) bool {
		return markIn(resolver, home, parameter)
	}, func() bool { return bodilessMark(parameter) })
}

// markIn is whether parameter, of home, is marked, judged there, and has every other mark that judgment settled
// recorded as an asked one is.
//
// One judgment settles the marks of every function the asked one's marks rest on (see demandWalk), and keeps them in
// home's state, where no question ever reaches the resolver's own record of answers. An answer the resolver holds is a
// sound place to stop comparing two builds only when everything it read is on record beside it, so each mark settled
// for a parameter of another declared function is asked here in its turn, of home itself, once home's lock is given
// back: the question finds the mark final, computes nothing, and is memoized, recorded for the store and told to a
// test's hand exactly as the first was. The resolver has no door for an answer nobody asked, so asking is the door.
func markIn(resolver *fact_resolver.Resolver, home *fact_resolver.Package, parameter *types.Var) bool {
	marked, settled := judgeIn(resolver, home, parameter)
	for _, other := range settled {
		ask(resolver, home, other)
	}
	return marked
}

// judgeIn is whether parameter, of home, is marked, judged there under home's lock, or the mark kept, and each other
// parameter of a declared function of home whose mark this call settled, none when the mark was kept.
func judgeIn(
	resolver *fact_resolver.Resolver,
	home *fact_resolver.Package,
	parameter *types.Var,
) (marked bool, settled []*types.Var) {
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[marks](home, Kind)
	owner := state.function(resolver, home, parameter)
	if owner == nil {
		// A parameter of no function the package declares, as an instantiation's is, which no call names.
		return false, nil
	}
	if kept, judged := state.functions[owner]; judged {
		return kept[parameter], nil
	}
	walk := state.walk(resolver, home)
	walk.reach(owner)
	for _, function := range walk.settle() {
		settled = append(settled, declaredSlices(function, parameter)...)
	}
	return state.functions[owner][parameter], settled
}

// declaredSlices is each slice parameter of function, a declared function or method of a package, but for asked: the
// parameters whose marks are answers of their own. A function literal's parameter is left out, since nothing outside
// its function can name it, and so is one of a function of no package, whose parameters are another function's, and a
// receiver, which no call passes a slice to as a parameter.
func declaredSlices(function *intermediate_representation.Function, asked *types.Var) []*types.Var {
	if function.Package == nil || function.Parent() != nil || function.Object() == nil {
		return nil
	}
	var declared []*types.Var
	for parameter := range function.Signature.Params().Variables() {
		if parameter != asked && typeutil.All(parameter.Type(), typeutil.IsSlice) {
			declared = append(declared, parameter)
		}
	}
	return declared
}
