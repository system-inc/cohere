package main

import (
	"os"
	"testing"

	"github.com/system-inc/cohere/internal/testing/system_cache_testing"
)

// TestMain gives cohere every test here starts, through the dispatcher, a system cache of this test binary's
// own, which it inherits, so no run reads, trims or writes the person's (#syscache).
func TestMain(suite *testing.M) {
	removeSystemCache, isolateError := system_cache_testing.Isolate()
	if isolateError != nil {
		panic(isolateError)
	}
	code := suite.Run()
	removeSystemCache()
	os.Exit(code)
}

// Every cohere the tests start reads a system cache of the tests' own, never the person's: `COHERE_SYSTEM_CACHE` is
// set, absolute, and outside their user cache directory.
func TestTheTestsSystemCacheIsNotTheUsers(test *testing.T) {
	test.Parallel()

	// Ignored: a machine with no user cache directory has none a test could reach.
	usersCache, _ := os.UserCacheDir()
	system_cache_testing.ExpectIsolated(test, usersCache)
}
