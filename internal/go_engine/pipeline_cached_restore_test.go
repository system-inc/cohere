package go_engine

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// With the findings cache on, a cold run's extra build declines at its check and reloads exactly as one under
// `--no-cache` does, the host having let go of its own resolver once it kept its packages either way (#px1yxq8): over
// restore-from-host, windows declines logger and opener at its check for the same reasons and is loaded again once for
// those host declines, in which its own resolver answers the packages it checks from source, and the merged verdict
// is the one under `--no-cache`, but for the counts of the cache. What its reload restores differs by design: with a
// store, its first load wrote reader through and saved sizer, so the reload hits both in its own store.
func TestACachedColdRunsBuildDeclinesAndReloadsAsUnderNoCache(test *testing.T) {
	test.Parallel()

	fixture := filepath.Join("testdata", "restore-from-host")
	plain, plainStream := runOver(test, copyFixture(test, fixture), exitFindings, "--other-builds")
	cached, cachedStream := runOverWith(test, copyFixture(test, fixture), exitFindings, "--other-builds")
	plainWindows, cachedWindows := windowsRun(test, plain).result, windowsRun(test, cached).result
	atCheck := func(declined map[string]string) map[string]string {
		kept := maps.Clone(declined)
		maps.DeleteFunc(kept, func(identifier string, _ string) bool {
			return !slices.Contains([]string{restoreLogger, restoreOpener}, identifier)
		})
		return kept
	}
	cachedDeclined, plainDeclined := atCheck(cachedWindows.FromHost.Declined), atCheck(plainWindows.FromHost.Declined)
	if len(plainDeclined) != 2 || !maps.Equal(cachedDeclined, plainDeclined) ||
		cachedWindows.FromHost.DeclinedAtCheck != plainWindows.FromHost.DeclinedAtCheck {
		test.Errorf("with the cache windows declined %d at its check, %v; under --no-cache %d, %v",
			cachedWindows.FromHost.DeclinedAtCheck, cachedDeclined, plainWindows.FromHost.DeclinedAtCheck,
			plainDeclined)
	}
	if cachedWindows.Cache.ReloadRounds != 1 || cachedWindows.Cache.ReloadRoundsBy["hostDecline"] != 1 {
		test.Errorf("with the cache windows was loaded again %d times, by %v, want once, for its host declines",
			cachedWindows.Cache.ReloadRounds, cachedWindows.Cache.ReloadRoundsBy)
	}
	if withoutCache(cachedStream) != withoutCache(plainStream) {
		test.Errorf("the cached cold run's stream differs from the one under --no-cache:\ncached\n%s\nno cache\n%s",
			cachedStream, plainStream)
	}
}

// A fixing run with the findings cache and extra builds whose rename rounds come after the first lint reads nothing of
// the host's resolver after its keep (#px1yxq8): over rename-platforms, the first lint pass keeps the host's packages
// for the builds and lets the resolver go, the rename then loads and proves its rewrite under each platform, and the
// run lints the written tree, renamed, with no findings cache. None of it reports a read after the keep.
func TestARenameAfterTheKeepReadsNothingTheKeepLetGo(test *testing.T) {
	test.Parallel()

	engine, _, noted := runOverNoted(test, copyFixture(test, filepath.Join("testdata", "rename-platforms")),
		exitClean, nil, "--other-builds")
	if !engine.renamed {
		test.Fatal("the run renamed nothing, so it tells nothing of the rounds after the first lint")
	}
	if strings.Contains(noted, "after the run kept its packages") {
		test.Errorf("the run read what its keep let go:\n%s", noted)
	}
}
