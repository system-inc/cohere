package patches

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/release/dispatch"
)

// The launcher refuses to cache a committed-tree build unless that binary's --version reports every
// patch present. It matches the state by its text, because importing this package would link the
// whole compiler into a launcher whose job is one stat and one exec. This pins the two spellings
// together, so changing Describe's wording fails here rather than refusing every build.
func TestDescribePrintsTheStateTheLauncherLooksFor(t *testing.T) {
	if len(All) == 0 {
		t.Skip("no patches are carried, so there is no state to print")
	}
	patch := All[0]

	present := strings.Join(Describe([]Result{{Patch: patch, Diagnostics: patch.Expected}}), "\n")
	if !strings.Contains(present, dispatch.PatchPresentMarker) {
		t.Fatalf("a present patch prints\n%s\nand the launcher looks for %q, so it would refuse every committed build",
			present, dispatch.PatchPresentMarker)
	}

	// The other direction, so the match above cannot pass by matching everything.
	missing := strings.Join(Describe([]Result{{Patch: patch, Diagnostics: patch.Expected + 1}}), "\n")
	if strings.Contains(missing, dispatch.PatchPresentMarker) {
		t.Fatalf("a missing patch also prints %q, so the launcher would accept a binary built from the stock compiler:\n%s",
			dispatch.PatchPresentMarker, missing)
	}
}
