package odd_slice_argument

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// Kind keys SA5012's marks in each package the resolver holds, and its counts.
var Kind = &fact_resolver.Kind{Name: "odd_slice_argument", OrderBound: orderFree}

// orderFree is the marks' order bound (fact_resolver.Kind.OrderBound): none, and known. The marks are the least set
// closed under their rules, one set whatever order a walk finds it in (see demandWalk), so no mark of any function of
// home hangs on the order its walks took.
func orderFree(*fact_resolver.Package, []*types.Func) ([]*types.Func, bool) {
	return nil, true
}

func init() {
	fact_resolver.Register(Kind, whole,
		func(resolver *fact_resolver.Resolver, home *fact_resolver.Package, object types.Object) bool {
			parameter, isParameter := object.(*types.Var)
			return isParameter && judgeIn(resolver, home, parameter)
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
		return judgeIn(resolver, home, parameter)
	}, func() bool { return bodilessMark(parameter) })
}

// judgeIn is whether parameter, of home, is marked, judged there under home's lock, or the mark kept.
//
// One judgment settles the marks of every function the asked one's marks rest on (see demandWalk), and keeps them in
// home's state, which every later question there reads them from. Only the asked mark reaches the resolver's record,
// through the question that asked it (fact_resolver.Ask); a mark the judgment settled and nobody asked is never
// recorded. Recording those would make the record turn on which judgment settled them first: slices.Insert's `s`
// would be recorded where a question of its `v` judges Insert, and not where slices's whole marks, computed as the run
// saves, settle Insert before that question comes, so two runs over the same standard library would record different
// answers.
// A reader comparing two builds at an answer needs the marks this judgment read from the kind's state on record too,
// beside the answer, and the resolver has no seam yet for recording an answer nobody asked.
func judgeIn(resolver *fact_resolver.Resolver, home *fact_resolver.Package, parameter *types.Var) bool {
	home.Lock()
	defer home.Unlock()
	state := fact_resolver.StateOf[marks](home, Kind)
	owner := state.function(resolver, home, parameter)
	if owner == nil {
		// A parameter of no function the package declares, which no call names (declaredParameter).
		return false
	}
	if kept, judged := state.functions[owner]; judged {
		return kept[parameter]
	}
	walk := state.walk(resolver, home)
	walk.reach(owner)
	walk.settle()
	return state.functions[owner][parameter]
}
