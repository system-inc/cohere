package go_language

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/load"
)

// Every cached load of a run whose findings cache reads a store collects its inputs beside it, and every load of a run
// without one, under `--no-cache` or with a session that opened no store, collects them inline (#ybjbqm2), since warm
// beside wins and cold inline does (load.Caching.InputsBeside).
func TestOnlyARunWithAFindingsStoreCollectsItsInputsBeside(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		name    string
		session *check.CacheSession
		beside  bool
	}{
		{"with a store", check.NewCacheSession(&check.Caching{Store: cache.Open(test.TempDir(), nil)}, nil), true},
		{"with a session and no store", check.NewCacheSession(&check.Caching{}, nil), false},
		{"with no session", nil, false},
	} {
		pipeline := &pipeline{session: each.session, described: &describedProject{}}
		if beside := pipeline.watchedCaching(load.Caching{}).InputsBeside; beside != each.beside {
			test.Errorf("a run %s collects its inputs beside its loads: %t, want %t", each.name, beside, each.beside)
		}
	}
}
