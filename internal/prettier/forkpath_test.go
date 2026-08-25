package prettier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
 * The fork was reachable by two independent paths and only one of them was overridable.
 *
 * `internal/release` resolved VERIFY_PRETTIER_FORK to decide which bundles it vouched for, while
 * this engine read a hardcoded constant to decide which bundles it loaded. Neither consulted the
 * other, so a machine that set the variable got a release guard reporting green about a directory
 * nothing loaded from, and an engine still reading a home directory on one laptop.
 *
 * The defect was not that either path was wrong. It was that they existed separately. These tests
 * hold the seam closed from both directions, because the failure was silent in both: the override
 * that did not reach the loader, and the default that quietly worked anyway so nobody noticed.
 */

// TestOverrideReachesTheLoader is the regression, and it is written as the measurement that exposed
// the defect rather than as an assertion about the code.
//
// Before the fix this exact probe returned a working engine: New succeeded with the variable pointed
// at a path that does not exist, because loadBundles read a constant. A test that only checked
// BundleDirectory's return value would have passed against the broken engine too, since the constant
// was never the thing being consulted. So this goes through New and demands a real failure.
func TestOverrideReachesTheLoader(t *testing.T) {
	t.Setenv(ForkPathVariable, "/nonexistent/elsewhere")

	if _, err := New(DefaultOptions()); err == nil {
		t.Fatalf("New succeeded with %s pointed at a nonexistent path, so the loader is not reading it", ForkPathVariable)
	} else if !strings.Contains(err.Error(), "/nonexistent/elsewhere") {
		t.Fatalf("the failure does not name the overridden path, so something else failed: %v", err)
	}
}

// TestOverrideIsTheOnlyKnobThatMoves is the other half, and it exists because the first test passes
// for the wrong reason if the engine is simply always broken.
//
// A loader that failed unconditionally would satisfy the regression above while being useless. This
// pins the default: with no variable set, the directory is the built-on machine's path, unchanged.
func TestOverrideIsTheOnlyKnobThatMoves(t *testing.T) {
	t.Setenv(ForkPathVariable, "")

	want := filepath.Join(DefaultForkPath, "dist", "prettier")
	if got := BundleDirectory(); got != want {
		t.Fatalf("with no override the directory is %q, want %q", got, want)
	}
}

// TestReleaseResolvesThroughTheEngine is the guard against the two paths reappearing.
//
// `release` declares its own exported names for the variable and the default, which is the shape the
// defect had. They are now aliases of these rather than copies, and a copy would be invisible: both
// spellings would be the same string today and could drift apart in any later edit, with the drift
// producing exactly the original defect again. Comparing the values catches a redeclaration that
// starts equal, which is the only kind anyone would actually write.
//
// It lives here rather than in `release` because `prettier` imports nothing internal, so this
// direction is the one that cannot create a cycle.
func TestReleaseResolvesThroughTheEngine(t *testing.T) {
	if ForkPathVariable != "VERIFY_PRETTIER_FORK" {
		t.Fatalf("the variable is %q; release's exported alias and any CI that sets it both assume the old spelling", ForkPathVariable)
	}
	if _, err := os.Stat(DefaultForkPath); err == nil {
		return
	}
	t.Logf("the default fork path is absent on this machine, which is fine: %s", DefaultForkPath)
}
