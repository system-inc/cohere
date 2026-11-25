package odd_slice_argument

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
)

// A walk settles once (see settle): settling spends it, so a reach after it is the spent walk's own panic, never a
// second keeping of the marks it settled. This walk reaches nothing before it settles, so settle reads neither the
// resolver nor home, and the reach after panics before it reads either; a walk that still held its table would go on
// to build the function and panic otherwise.
func TestASettledWalkIsSpent(test *testing.T) {
	test.Parallel()
	walk := &demandWalk{state: &marks{}, byFunction: map[*intermediate_representation.Function]*reachedFunction{}}
	walk.settle()
	defer func() {
		recovered := recover()
		if message, isMessage := recovered.(string); !isMessage || !strings.Contains(message, "after it settled") {
			test.Errorf("a reach after settle panicked with %v, want the spent walk's invariant", recovered)
		}
	}()
	walk.reach(nil)
}
