package build_reads

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
)

// readFact is a fact type for the tests.
type readFact struct{}

// AFact makes readFact an analysis.Fact.
func (*readFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

// Reads from many goroutines at once, as the actions on one package add them, keep each read once, in an order that
// does not depend on the order they came in, and the first reason the package is never restored.
func TestReadsFromManyGoroutinesKeepEachReadOnceInOrder(test *testing.T) {
	test.Parallel()

	other := types.NewPackage("example.com/other", "other")
	second := types.NewPackage("example.com/second", "second")
	objects := []types.Object{
		types.NewFunc(token.NoPos, second, "Alpha", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		types.NewVar(token.NoPos, other, "Zeta", types.Typ[types.Int]),
		types.NewConst(token.NoPos, other, "Beta", types.Typ[types.Int], nil),
	}
	factType := reflect.TypeFor[*readFact]()
	reads := &Reads{}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			for _, object := range objects {
				reads.AddUse(object, UseMember)
				reads.AddFact(Fact{Object: object, Package: object.Pkg(), FactType: factType, Found: true})
			}
			reads.AddFact(Fact{Package: other, FactType: factType})
		})
	}
	group.Wait()
	reads.MarkNever("first")
	reads.MarkNever("second")

	var names []string
	for _, use := range reads.Uses() {
		names = append(names, use.Object.Pkg().Path()+"."+use.Object.Name()+" "+string(use.Kind))
	}
	want := []string{
		"example.com/other.Beta Member",
		"example.com/other.Zeta Member",
		"example.com/second.Alpha Member",
	}
	if !slices.Equal(names, want) {
		test.Fatalf("want the uses\n%q\ngot\n%q", want, names)
	}
	facts := reads.Facts()
	if len(facts) != 4 || facts[0].Object != nil || facts[0].Package != other {
		test.Fatalf("want three object facts after other's package fact, got %+v", facts)
	}
	if never := reads.Never(); never != "first" {
		test.Fatalf("want the first reason kept, got %q", never)
	}
}

// Reads are restorable only once their type reads are recorded and while no never mark is set: reads never recorded
// are unknown, which is never read as empty, however little else they hold.
func TestUnknownReadsAreNeverRestorable(test *testing.T) {
	test.Parallel()

	unknown := &Reads{}
	if restorable, why := unknown.Restorable(); restorable || why != "its type reads were never recorded" {
		test.Fatalf("want unknown reads unrestorable, never recorded, got %t, %q", restorable, why)
	}
	recorded := &Reads{}
	recorded.RecordTypes(types.NewPackage("example.com/own", "own"), &types.Info{}, nil)
	if restorable, why := recorded.Restorable(); !restorable || !recorded.Recorded() {
		test.Fatalf("want recorded empty reads restorable, got %t, %q", restorable, why)
	}
	recorded.MarkNever(RestoredFromCache)
	if restorable, why := recorded.Restorable(); restorable || why != RestoredFromCache {
		test.Fatalf("want reads marked never unrestorable for the mark, got %t, %q", restorable, why)
	}
}

// Two rules' Method lookups of one subject are two reads, each with its own answer, since each rule answers by its own
// measure: no_any found a method of the signature where abbreviated, asking whether the receiver implements an
// interface, found none (#dscc341). A Method or Interface lookup that names no rule could be taken for another's, so
// it marks the package never, while a Scope lookup, which has one measure, needs none.
func TestEachRulesMethodLookupIsItsOwnRead(test *testing.T) {
	test.Parallel()

	other := types.NewPackage("example.com/other", "other")
	own := types.NewPackage("example.com/own", "own")
	signature := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	subject := types.NewFunc(token.NoPos, own, "Close", signature)
	answer := types.NewFunc(token.NoPos, other, "Close", signature)
	reads := &Reads{}
	reads.RecordTypes(own, &types.Info{}, nil)
	reads.AddLookup(Lookup{Query: QueryMethod, Name: "Close", Subject: subject, Found: true, Answer: answer,
		Rule: "no-any"})
	reads.AddLookup(Lookup{Query: QueryMethod, Name: "Close", Subject: subject, Rule: "abbreviated",
		Instantiates: true})
	reads.AddLookup(Lookup{Query: QueryScope, Path: other.Path(), Name: "Close", Found: true, Answer: answer})

	var lines []string
	for _, lookup := range reads.Lookups() {
		lines = append(lines, fmt.Sprintf("%s %s %s found=%t instantiates=%t", lookup.Query, lookup.Name,
			lookup.Rule, lookup.Found, lookup.Instantiates))
	}
	want := []string{
		"Method Close abbreviated found=false instantiates=true",
		"Method Close no-any found=true instantiates=false",
		"Scope Close  found=true instantiates=false",
	}
	if !slices.Equal(lines, want) {
		test.Fatalf("want the lookups\n%q\ngot\n%q", want, lines)
	}
	if restorable, why := reads.Restorable(); !restorable {
		test.Fatalf("want reads whose Method lookups name their rules restorable, got %q", why)
	}

	reads.AddLookup(Lookup{Query: QueryInterface, Name: "Close", Subject: subject})
	wantNever := "its Interface lookup of Close names no asking rule"
	if restorable, why := reads.Restorable(); restorable || why != wantNever {
		test.Fatalf("want an Interface lookup naming no rule to mark the reads never, got %t, %q", restorable, why)
	}
}

// Sorting a package's selections through one receiver, and its uses of one object in several kinds, compares that
// object with itself and writes nothing: rendering a large struct twice for each such comparison doubled every extra
// build's decide on deeafbf4 (#p1s9gdk). Sorting two hundred selections through a struct of two hundred fields, and two
// hundred objects each used in two kinds, allocates the result's slice and nothing per comparison.
//
// Each call sorts reads nothing has asked yet, one Reads for each call testing.AllocsPerRun makes, its runs and the
// one that warms it up: a Reads keeps the list it sorted (#f7qsmdf), so one asked on every run would sort once and
// hand the kept list back for nothing after, and the average would pass with the rendering back.
//
// Not parallel: testing.AllocsPerRun counts the whole process's allocations, so another test running beside it would
// be counted too.
func TestSortingReadsOfOneObjectWritesNothing(test *testing.T) {
	const runs = 5
	other := types.NewPackage("example.com/other", "other")
	fields := make([]*types.Var, 0, 200)
	for index := range 200 {
		fields = append(fields, types.NewField(token.NoPos, other, fmt.Sprintf("Field%d", index), types.Typ[types.Int],
			false))
	}
	receiver := types.NewTypeName(token.NoPos, other, "Wide", nil)
	types.NewNamed(receiver, types.NewStruct(fields, nil), nil)
	unasked := make([]*Reads, 0, runs+1)
	for range runs + 1 {
		reads := &Reads{}
		collected := typeReads{uses: map[ObjectUse]bool{}, selections: map[selectionIdentity]Selection{}}
		for index, field := range fields {
			selection := Selection{Receiver: receiver, Name: field.Name(), Index: []int{index}, Selected: field}
			collected.selections[selectionKey(selection)] = selection
			collected.uses[ObjectUse{Object: field, Kind: UseMember}] = true
			collected.uses[ObjectUse{Object: field, Kind: UseWhole}] = true
		}
		reads.publishTypes(collected)
		unasked = append(unasked, reads)
	}

	nextSelections := 0
	if allocations := testing.AllocsPerRun(runs, func() {
		unasked[nextSelections].Selections()
		nextSelections++
	}); allocations > 1 {
		test.Errorf("sorting %d selections through one receiver allocated %.0f times, want the result's slice alone",
			len(fields), allocations)
	}
	nextUses := 0
	if allocations := testing.AllocsPerRun(runs, func() {
		unasked[nextUses].Uses()
		nextUses++
	}); allocations > 1 {
		test.Errorf("sorting %d objects each used in two kinds allocated %.0f times, want the result's slice alone",
			len(fields), allocations)
	}
}

// everyKindOfRead is reads holding two of each kind, a use, a selection, a fact read and a lookup of each of two
// fields of another package's struct, with the first field's use, fact read and lookup, as an adder is handed them
// again.
type everyKindOfRead struct {
	reads  *Reads
	use    ObjectUse
	fact   Fact
	lookup Lookup
}

// readsOfEveryKind records two reads of each kind, the uses and selections published as a walk publishes them, the
// fact reads and lookups added as the actions add them.
func readsOfEveryKind() everyKindOfRead {
	other := types.NewPackage("example.com/other", "other")
	fields := []*types.Var{
		types.NewField(token.NoPos, other, "Bravo", types.Typ[types.Int], false),
		types.NewField(token.NoPos, other, "Delta", types.Typ[types.Int], false),
	}
	receiver := types.NewTypeName(token.NoPos, other, "Wide", nil)
	types.NewNamed(receiver, types.NewStruct(fields, nil), nil)
	factType := reflect.TypeFor[*readFact]()
	every := everyKindOfRead{reads: &Reads{}}
	collected := typeReads{uses: map[ObjectUse]bool{}, selections: map[selectionIdentity]Selection{}}
	for index, field := range fields {
		use := ObjectUse{Object: field, Kind: UseMember}
		fact := Fact{Object: field, Package: other, FactType: factType, Found: true}
		lookup := Lookup{Query: QueryScope, Path: other.Path(), Name: field.Name()}
		selection := Selection{Receiver: receiver, Name: field.Name(), Index: []int{index}, Selected: field}
		collected.uses[use] = true
		collected.selections[selectionKey(selection)] = selection
		every.reads.AddFact(fact)
		every.reads.AddLookup(lookup)
		if index == 0 {
			every.use, every.fact, every.lookup = use, fact, lookup
		}
	}
	every.reads.publishTypes(collected)
	return every
}

// sameList reports whether two lists are one slice, the same elements at the same place and as many of them. Two
// empty lists are never told to be one: they have no element to be at a place.
func sameList[Read any](left []Read, right []Read) bool {
	return len(left) > 0 && len(left) == len(right) && &left[0] == &right[0]
}

// The reads keep each sorted list the first time it is asked and hand every later ask the same slice, so asking again
// allocates nothing: each extra build's verdicts ask the host's reads for the lists the build before it asked
// (#f7qsmdf).
//
// Not parallel: testing.AllocsPerRun counts the whole process's allocations, so another test running beside it would
// be counted too.
func TestASecondAskOfAListAllocatesNothingAndIsTheSameSlice(test *testing.T) {
	every := readsOfEveryKind()
	requireKept(test, "uses", every.reads.Uses)
	requireKept(test, "selections", every.reads.Selections)
	requireKept(test, "facts", every.reads.Facts)
	requireKept(test, "lookups", every.reads.Lookups)
}

// requireKept asks list, the reads' list of kind, and requires that asking it again allocates nothing and gives the
// slice the first ask gave.
func requireKept[Read any](test *testing.T, kind string, list func() []Read) {
	test.Helper()

	first := list()
	if len(first) != 2 {
		test.Fatalf("the reads hold %d %s, want two, so the list tells nothing", len(first), kind)
	}
	if allocations := testing.AllocsPerRun(5, func() { list() }); allocations != 0 {
		test.Errorf("asking the %s again allocated %.0f times, want none", kind, allocations)
	}
	if !sameList(first, list()) {
		test.Errorf("asking the %s again gave another slice, want the one kept", kind)
	}
}

// A read added after a list was kept is in the next list asked, in its sorted place, whichever way it came: a use the
// type check added or the walk published, a selection the walk published, a fact read or a lookup. Reads with none of
// a kind keep an empty list, which the first read of the kind must drop as it would a full one.
func TestAReadAddedAfterAListWasKeptIsInTheNextList(test *testing.T) {
	test.Parallel()

	other := types.NewPackage("example.com/other", "other")
	receiver := types.NewTypeName(token.NoPos, other, "Wide", nil)
	factType := reflect.TypeFor[*readFact]()
	field := func(name string) *types.Var {
		return types.NewField(token.NoPos, other, name, types.Typ[types.Int], false)
	}
	useName := func(use ObjectUse) string { return use.Object.Name() }
	test.Run("a use added", func(test *testing.T) {
		test.Parallel()

		reads := &Reads{}
		requireAddedLater(test, reads.Uses, useName, func(name string) { reads.AddUse(field(name), UseMember) })
	})
	test.Run("a use published", func(test *testing.T) {
		test.Parallel()

		reads := &Reads{}
		requireAddedLater(test, reads.Uses, useName, func(name string) {
			reads.publishTypes(typeReads{uses: map[ObjectUse]bool{{Object: field(name), Kind: UseMember}: true}})
		})
	})
	test.Run("a selection published", func(test *testing.T) {
		test.Parallel()

		reads := &Reads{}
		requireAddedLater(test, reads.Selections, func(selection Selection) string { return selection.Name },
			func(name string) {
				selection := Selection{Receiver: receiver, Name: name, Index: []int{0}, Selected: field(name)}
				reads.publishTypes(typeReads{
					selections: map[selectionIdentity]Selection{selectionKey(selection): selection},
				})
			})
	})
	test.Run("a fact read added", func(test *testing.T) {
		test.Parallel()

		reads := &Reads{}
		requireAddedLater(test, reads.Facts, func(fact Fact) string { return fact.Object.Name() },
			func(name string) {
				reads.AddFacts([]Fact{{Object: field(name), Package: other, FactType: factType, Found: true}})
			})
	})
	test.Run("a lookup added", func(test *testing.T) {
		test.Parallel()

		reads := &Reads{}
		requireAddedLater(test, reads.Lookups, func(lookup Lookup) string { return lookup.Name },
			func(name string) { reads.AddLookup(Lookup{Query: QueryScope, Path: other.Path(), Name: name}) })
	})
}

// requireAddedLater asks list of reads that hold none of its kind, then after add has added reads named Bravo and
// Delta, then after it has added one named Charlie, and requires each ask to give every read added by then, sorted,
// each named by name: the first an empty list that is not nil, the last with Charlie between the two.
func requireAddedLater[Read any](test *testing.T, list func() []Read, name func(Read) string, add func(name string)) {
	test.Helper()

	if none := list(); none == nil || len(none) != 0 {
		test.Fatalf("want an empty list that is not nil of reads that hold none, got %v", none)
	}
	for _, step := range []struct {
		added []string
		want  []string
	}{
		{[]string{"Delta", "Bravo"}, []string{"Bravo", "Delta"}},
		{[]string{"Charlie"}, []string{"Bravo", "Charlie", "Delta"}},
	} {
		for _, added := range step.added {
			add(added)
		}
		var names []string
		for _, read := range list() {
			names = append(names, name(read))
		}
		if !slices.Equal(names, step.want) {
			test.Fatalf("after %q were added, want the list %q, got %q", step.added, step.want, names)
		}
	}
}

// An adder that adds nothing new keeps the list: a use, a fact read or a lookup the reads hold already changes no
// list, so the next ask gives the slice kept, and a caller ranging over it reads what it would have.
func TestAnAdderThatAddsNothingNewKeepsTheList(test *testing.T) {
	test.Parallel()

	every := readsOfEveryKind()
	uses, selections := every.reads.Uses(), every.reads.Selections()
	facts, lookups := every.reads.Facts(), every.reads.Lookups()
	every.reads.AddUse(every.use.Object, every.use.Kind)
	every.reads.AddFacts([]Fact{every.fact})
	every.reads.AddLookup(every.lookup)
	if !sameList(uses, every.reads.Uses()) {
		test.Error("AddUse of a use the reads hold dropped the kept uses, want the same slice")
	}
	if !sameList(selections, every.reads.Selections()) {
		test.Error("adding reads the reads hold dropped the kept selections, want the same slice")
	}
	if !sameList(facts, every.reads.Facts()) {
		test.Error("AddFacts of a fact read the reads hold dropped the kept facts, want the same slice")
	}
	if !sameList(lookups, every.reads.Lookups()) {
		test.Error("AddLookup of a question the reads hold dropped the kept lookups, want the same slice")
	}
}

// Objects of one package and name are ordered by how go/types writes them, then by position, in each of the four
// lists whose sort compares objects. Each sort remembers the text it wrote for each object, so this holds that the
// text is the object's own: text remembered under the wrong key would make the tied objects write alike and leave
// their order to position, which here is another order.
func TestTiedObjectsAreOrderedByTheirTextThenPosition(test *testing.T) {
	test.Parallel()
	owner := types.NewPackage("example.com/owner", "owner")
	field := func(position token.Pos, kind types.BasicKind) types.Object {
		return types.NewField(position, owner, "Value", types.Typ[kind], false)
	}
	// By text, `field Value bool`, `field Value int` twice, then `field Value string`; by position, 1, 2, 4, 7.
	laterInt, text, earlierBool, earlierInt := field(7, types.Int), field(2, types.String), field(1, types.Bool),
		field(4, types.Int)
	wantObjects := []types.Object{earlierBool, earlierInt, laterInt, text}
	// By text, `type example.com/owner.Holder int` before its struct twin; by position, the struct first.
	integerHolder := types.NewTypeName(9, owner, "Holder", nil)
	types.NewNamed(integerHolder, types.Typ[types.Int], nil)
	structHolder := types.NewTypeName(3, owner, "Holder", nil)
	types.NewNamed(structHolder, types.NewStruct(nil, nil), nil)
	wantReceivers := []*types.TypeName{integerHolder, structHolder}

	reads := &Reads{}
	collected := typeReads{uses: map[ObjectUse]bool{}, selections: map[selectionIdentity]Selection{}}
	for _, object := range []types.Object{laterInt, text, earlierBool, earlierInt} {
		collected.uses[ObjectUse{Object: object, Kind: UseMember}] = true
	}
	for _, receiver := range []*types.TypeName{structHolder, integerHolder} {
		selection := Selection{Receiver: receiver, Name: "Value", Index: []int{0}, Selected: text}
		collected.selections[selectionKey(selection)] = selection
	}
	reads.publishTypes(collected)
	for _, object := range []types.Object{text, laterInt, earlierInt, earlierBool} {
		reads.AddFact(Fact{Object: object, Package: owner, FactType: reflect.TypeFor[*readFact](), Found: true})
		reads.AddLookup(Lookup{Query: QueryMethod, Path: owner.Path(), Name: "Value", Subject: object, Rule: "test"})
	}

	uses, facts, lookups, selections := reads.Uses(), reads.Facts(), reads.Lookups(), reads.Selections()
	if len(uses) != len(wantObjects) || len(facts) != len(wantObjects) || len(lookups) != len(wantObjects) ||
		len(selections) != len(wantReceivers) {
		test.Fatalf("got %d uses, %d facts, %d lookups and %d selections, want %d, %d, %d and %d", len(uses),
			len(facts), len(lookups), len(selections), len(wantObjects), len(wantObjects), len(wantObjects),
			len(wantReceivers))
	}
	for index, object := range wantObjects {
		if uses[index].Object != object {
			test.Errorf("use %d is %s at %d, want %s at %d", index, uses[index].Object, uses[index].Object.Pos(),
				object, object.Pos())
		}
		if facts[index].Object != object {
			test.Errorf("fact %d is of %s at %d, want %s at %d", index, facts[index].Object,
				facts[index].Object.Pos(), object, object.Pos())
		}
		if lookups[index].Subject != object {
			test.Errorf("lookup %d is of %s at %d, want %s at %d", index, lookups[index].Subject,
				lookups[index].Subject.Pos(), object, object.Pos())
		}
	}
	for index, receiver := range wantReceivers {
		if selections[index].Receiver != receiver {
			test.Errorf("selection %d is through %s at %d, want %s at %d", index, selections[index].Receiver,
				selections[index].Receiver.Pos(), receiver, receiver.Pos())
		}
	}
}

// BenchmarkSortingTheReadsOfRealPackages sorts the uses and selections the walk records of two real packages of the
// repository, this one and the engine's check package, both reading go/ast and go/types throughout, whose many
// methods and fields of one name, twelve fields of go/ast named X among them, tie on package and name and are told
// apart by how go/types writes them. Each iteration drops the kept lists and asks for them again, so each is one
// sort of each list. The packages are loaded with go/packages, since the engine's own loader imports this package.
// It is skipped under -short.
func BenchmarkSortingTheReadsOfRealPackages(benchmark *testing.B) {
	if testing.Short() {
		benchmark.Skip("loads packages of the repository")
	}
	configuration := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedImports,
	}
	roots, loadError := packages.Load(configuration, ".", "github.com/system-inc/cohere/internal/go_engine/check")
	if loadError != nil {
		benchmark.Fatal(loadError)
	}
	recorded := []*Reads{}
	for _, root := range roots {
		if len(root.Errors) > 0 {
			benchmark.Fatalf("package %s does not load clean: %v", root.ID, root.Errors)
		}
		reads := &Reads{}
		reads.RecordTypes(root.Types, root.TypesInfo, root.Syntax)
		recorded = append(recorded, reads)
	}
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		for _, reads := range recorded {
			reads.sortedUses, reads.sortedSelections = nil, nil
			reads.Uses()
			reads.Selections()
		}
	}
}
