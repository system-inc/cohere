package http_status_literal

import (
	"path/filepath"
	"testing"

	"honnef.co/go/tools/stylecheck/st1013"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for `ST1013`, copied unchanged from stylecheck/st1013/testdata/go1.0 into
// testdata/go1.0, hold the port to every `//@ diag(...)` note the original expects, and to nothing more. The folder
// holds the go.mod staticcheck's testutil lays over it, `module example.com` at Go 1.0.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0")
}

// The golden file staticcheck's fixtures hold beside CheckHTTPStatusCodes.go, copied unchanged, holds the port's fixes
// there to the file the original's fixes make, as staticcheck's testutil holds the original's.
func TestPortFixesMatchUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), "go1.0")
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings, each with the same fix.
// parity holds every call the check reads, every argument written as a literal it reads and every near miss, under
// staticcheck's default list of codes left alone and, in parity/configured, under a staticcheck.conf of its own. The
// one place the two differ is held apart, under the user's cache directory (TestPortDropsUnderTheUserCacheDirectory).
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, st1013.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "parity/...")
}

// The differences fixture sits under a stand-in for the user's cache directory, beside a staticcheck.conf that allows
// what it holds. The original reads no staticcheck.conf for a file there and reports it, and the port reads the
// engine's, which skips only Go's build caches, and leaves it alone, each a `drops:` record (#sc5qmy7).
//
// Not parallel: the harness moves the process's user cache directory while the two run.
func TestPortDropsUnderTheUserCacheDirectory(test *testing.T) {
	userCacheDirectory := filepath.Join(analysistest.TestData(), "user_cache", "Library", "Caches")
	parity.RequireOnlyAdjudicatedUnderUserCacheDirectory(test, st1013.Analyzer, Analyzer, analysistest.TestData(),
		"user_cache/Library/Caches/differences", userCacheDirectory)
}
