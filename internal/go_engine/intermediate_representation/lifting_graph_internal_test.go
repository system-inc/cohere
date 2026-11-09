package intermediate_representation

import (
	"testing"

	"github.com/system-inc/cohere/static_single_assignment"
)

// TestMintGrowsTheTableTheResolveIsSizedBy mints identifiers past the declarations a function starts with, and
// holds each to a place in lifting.declarations: liftingResolve sizes its dense tables by that slice's length, so
// an identifier Mint handed out without growing it would index past them (#beapx0q).
func TestMintGrowsTheTableTheResolveIsSizedBy(test *testing.T) {
	test.Parallel()
	lifting := &liftingFunction{
		declarations: []static_single_assignment.DeclarationId{0, 1},
		allocations:  []*Allocation{{}, {}},
	}
	var minted []static_single_assignment.IdentifierId
	for round := range 40 {
		minted = append(minted, liftingGraph{}.Mint(lifting, static_single_assignment.IdentifierId(round%2)))
	}
	if len(lifting.declarations) != 42 {
		test.Fatalf("after 40 mints over 2 declarations, the table holds %d identifiers, want 42",
			len(lifting.declarations))
	}
	for index, identifier := range minted {
		if int(identifier) >= len(lifting.declarations) {
			test.Fatalf("mint %d handed out identifier %d past the table's %d", index, identifier,
				len(lifting.declarations))
		}
		want := static_single_assignment.DeclarationId(index % 2)
		if got := (liftingGraph{}).Declaration(lifting, identifier); got != want {
			test.Errorf("identifier %d declares %d, want %d", identifier, got, want)
		}
	}
	definitions := make([]liftingDefinition, len(lifting.declarations))
	if last := minted[len(minted)-1]; int(last) >= len(definitions) {
		test.Errorf("the last identifier minted, %d, is past a resolve table of %d", last, len(definitions))
	}
}
