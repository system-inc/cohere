package fact_resolver_test

import (
	"cmp"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// A resolver records the bodies whose rules ask each answer only once told to (#5vbs1np, must 5): before, a note takes
// no lock, since it returns before the record's, allocates nothing, and leaves no record; after, each body is held
// once, however many times its rule asks, and a question with no asker is held as one no place names (round 2's N3).
//
// Not parallel: testing.AllocsPerRun refuses to run in a parallel test, whose neighbours' allocations it would count.
func TestAResolverRecordsAskersOnlyOnceToldTo(test *testing.T) {
	resolver := fact_resolver.New(nil)
	kind := &fact_resolver.Kind{Name: "asker_probe"}
	object := types.NewFunc(token.NoPos, nil, "asked", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	asker := fact_resolver.Asker{File: "asking.go", Offset: 7}
	if allocated := testing.AllocsPerRun(100, func() { resolver.NoteAsker(kind, object, &asker) }); allocated != 0 {
		test.Errorf("a note to a resolver recording no asker allocated %v times a call, want none", allocated)
	}
	if resolver.RecordsAskers() || resolver.Askers() != nil {
		test.Fatalf("a resolver never told to records askers %t, holding %v", resolver.RecordsAskers(),
			resolver.Askers())
	}
	resolver.RecordAskers()
	resolver.NoteAsker(kind, object, &asker)
	resolver.NoteAsker(kind, object, &asker)
	resolver.NoteAsker(kind, object, nil)
	held := resolver.Askers()[kind.Name][object]
	slices.SortFunc(held, func(first fact_resolver.Asker, second fact_resolver.Asker) int {
		return cmp.Compare(first.File, second.File)
	})
	if want := []fact_resolver.Asker{{}, asker}; !slices.Equal(held, want) {
		test.Errorf("the resolver holds %v as the askers of the answer, want %v", held, want)
	}
}
