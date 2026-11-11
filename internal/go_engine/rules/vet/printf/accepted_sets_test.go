package printf

import (
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/type_predicates/verbs"
)

// Every set of kinds printf matches a type against below the argument's own is among verbs.AcceptedSets, which the
// restore from the host renders another package's unexported field by: each verb's of the table but those that match
// no type, %, %v and %T, or ask only for an error, %w, and the sets checkOperation gives %q at go1.26 and %d at
// go1.27 and the * of a width or precision asks. A verb given a set of its own without a place there would let a
// build whose unexported field answers it otherwise restore a finding it should not.
func TestEveryVerbsAcceptedSetIsRenderedByTheRestore(test *testing.T) {
	test.Parallel()

	asked := []verbs.Kinds{verbs.Rune | verbs.Byte | verbs.String, verbs.Int}
	for _, known := range printVerbs {
		if known.accepted != 0 && known.accepted != verbs.Any && known.accepted != verbs.Error {
			asked = append(asked, known.accepted)
		}
	}
	for _, accepted := range asked {
		if !slices.Contains(verbs.AcceptedSets, accepted) {
			test.Errorf("printf matches against %b, which verbs.AcceptedSets does not hold", accepted)
		}
	}
}
