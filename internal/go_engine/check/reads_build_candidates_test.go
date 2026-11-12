package check

import (
	"go/types"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// A build renders each type name's candidate for a lookup's query and name once, and reads the lookup's answer past
// what it kept: an interface holds one lookup's answer and not another's, asked in either order, while the lines it
// is compared by are the ones methodCandidateOf renders (#8fbkkp4).
func TestACandidateIsRenderedOnceAndReadsEachLookupsAnswer(test *testing.T) {
	test.Parallel()

	world := checkedMethodWorld(test, "darwin", map[string]string{"foreign.go": methodForeign,
		"closer.go": "package foreign\n\ntype Closer interface {\n\tClose() error\n\tFlush()\n}\n"}, methodUser)
	closer, _ := world.foreign.Scope().Lookup("Closer").(*types.TypeName)
	closeMethod := methodSetMember(world.foreign, "Closer", "Close")
	flushMethod := methodSetMember(world.foreign, "Closer", "Flush")
	build := measuredReadsBuild()
	want, wantFound := methodCandidateOf(closer, build_reads.QueryMethod, "Close", nil, ReadsRendererVersion)
	for _, step := range []struct {
		answer      types.Object
		holdsAnswer bool
	}{
		{answer: closeMethod, holdsAnswer: true},
		{answer: nil, holdsAnswer: false},
		{answer: flushMethod, holdsAnswer: true},
		{answer: methodOf(world.user, "Mine", "Close"), holdsAnswer: false},
		{answer: closeMethod, holdsAnswer: true},
	} {
		candidate, key, found := build.candidate(closer, build_reads.QueryMethod, "Close", step.answer)
		if !found || !wantFound || key != methodForeignPath+".Closer" {
			test.Fatalf("want Closer a candidate keyed %s.Closer, got %t, %q", methodForeignPath, found, key)
		}
		if candidate.holdsAnswer != step.holdsAnswer {
			test.Errorf("answer %v: want holdsAnswer %t, got %t", step.answer, step.holdsAnswer, candidate.holdsAnswer)
		}
		if !candidate.sameLines(want) || candidate.shape != want.shape || !candidate.isInterface {
			test.Errorf("answer %v: the candidate's lines or shape differ from methodCandidateOf's", step.answer)
		}
	}
	stored := 0
	build.candidates.Range(func(_, _ any) bool {
		stored++
		return true
	})
	if stored != 1 {
		test.Errorf("want one candidate kept for Closer's Close, got %d", stored)
	}
}
