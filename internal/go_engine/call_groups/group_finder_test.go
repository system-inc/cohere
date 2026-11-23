package call_groups_test

import (
	"go/token"
	"slices"
	"testing"

	"github.com/system-inc/go-types/types"

	"github.com/system-inc/cohere/internal/go_engine/call_groups"
)

// A cycle of three is one group, a function that calls itself alone is none, and a function on the way into a cycle,
// which nothing calls back, is in no group: first, second and third call round, recursive calls itself and first, and
// entry calls recursive.
func TestOfFindsEachCycleOfTwoOrMore(test *testing.T) {
	test.Parallel()
	function := func(name string) *types.Func {
		return types.NewFunc(token.NoPos, nil, name, types.NewSignatureType(nil, nil, nil, nil, nil, false))
	}
	first, second, third := function("first"), function("second"), function("third")
	recursive, entry := function("recursive"), function("entry")
	groups := call_groups.Of(map[*types.Func][]*types.Func{first: {second}, second: {third}, third: {first},
		recursive: {recursive, first}, entry: {recursive}})
	if len(groups) != 1 {
		test.Fatalf("found %d groups, want the one cycle of first, second and third", len(groups))
	}
	var names []string
	for _, member := range groups[0] {
		names = append(names, member.Name())
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"first", "second", "third"}) {
		test.Errorf("the group is %v, want first, second and third", names)
	}
}
