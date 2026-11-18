package go_engine

import (
	"maps"
	"path/filepath"
	"testing"
)

// A warm run that hits every package writes nothing to the store (#b2t9xmf): gofmt's verdicts it read and kept as they
// were are not written again, so no file of the store moves, and no shard's stamp with it, which every extra build's
// pack would then read again from the store.
func TestAWarmAllHitRunWritesNothingToTheStore(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "clean"))
	cold := runCachedEngine(test, root)
	if !cold.counts.On || cold.counts.Written == 0 {
		test.Fatalf("want the cold run to write the store, got %+v", cold.counts)
	}
	// The second run reads what the first wrote, the verdicts among it, and may still keep what it learned from them.
	runCachedEngine(test, root)
	before := entriesIn(test, root)
	warm := runCachedEngine(test, root)
	// Each run here has a system cache of its own (isolatedSource), so the standard library misses every run; the
	// project's store, the one this snapshot reads, must take no write.
	if warm.counts.Hit == 0 || warm.counts.Written != 0 {
		test.Fatalf("want the warm run to hit the project's packages and write no entry, got %+v", warm.counts)
	}
	if after := entriesIn(test, root); !maps.Equal(after, before) {
		for path, modified := range after {
			if before[path] != modified {
				test.Errorf("the warm run wrote %s", path)
			}
		}
		for path := range before {
			if _, kept := after[path]; !kept {
				test.Errorf("the warm run removed %s", path)
			}
		}
	}
}
