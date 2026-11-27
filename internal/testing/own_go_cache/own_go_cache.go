// Package own_go_cache gives a go command a test starts a Go cache of the test's own, and only that one.
//
// `GOCACHE` alone doesn't do it. When `GOCACHEPROG` names a cache program, go reads and writes every entry through the
// program and only hands it `GOCACHE` as a directory it may use, so a test that builds into its own cache and then
// reads, trims or counts that directory finds nothing there. Loom's runner sets both for an integrate job, its layer
// as `GOCACHE` and its program as `GOCACHEPROG` (#e94a6r4), and every test the job runs inherits them: the first
// integration under it went red on two go_cache tests that trimmed an empty directory. So a test that names a cache
// of its own empties `GOCACHEPROG` beside it, and the guard in internal/testing/guard holds every test to that.
//
// An empty `GOCACHEPROG` is no program: cmd/go reads an empty variable as unset, so only a go env file could still name
// one, and that file is the account's own, which a test that wants it gone sets `GOENV` for.
package own_go_cache

import (
	"strings"
	"testing"
)

// Environment is environment with its Go cache set to cache alone: any `GOCACHE` and `GOCACHEPROG` it holds dropped,
// then `GOCACHE` naming cache and `GOCACHEPROG` empty. The environment it was handed is left as it was.
func Environment(environment []string, cache string) []string {
	own := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "GOCACHE=") && !strings.HasPrefix(entry, "GOCACHEPROG=") {
			own = append(own, entry)
		}
	}
	return append(own, "GOCACHE="+cache, "GOCACHEPROG=")
}

// Set sets the test process's Go cache to cache alone for the rest of the test, `GOCACHE` naming it and `GOCACHEPROG`
// empty, so every go command the test starts with the process's environment builds into cache. Like test.Setenv, it
// can't be called from a parallel test.
func Set(test testing.TB, cache string) {
	test.Helper()
	test.Setenv("GOCACHE", cache)
	test.Setenv("GOCACHEPROG", "")
}
