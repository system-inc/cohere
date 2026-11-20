package check

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// Both declarations-only record keys move with the whole records' version, DependenceRecordVersion (#anfjtr3): what
// the collector records is what that version names, so a collector change that moves it alone must leave every
// declarations-only record a miss, in the project's store and in the system cache's, whose engine identity a stamp can
// hold still across binaries. A record put under each key today is a miss under the same key at another whole records'
// version, and a hit at this one. Hash the version into neither key, and each record is a hit at the other version.
func TestADeclarationsRecordMissesOnceTheWholeRecordsVersionMoves(test *testing.T) {
	test.Parallel()

	store := cache.OpenAt(test.TempDir(), nil)
	key, record, moved := cache.Key{1, 2, 3}, []byte("a declarations-only record"), DependenceRecordVersion+"-moved"
	for _, kind := range []struct {
		name  string
		today cache.Key
		after cache.Key
	}{
		{"the project's", DeclarationsRecordKey(key), declarationsRecordKeyAt(moved, key)},
		{"the standard library's", StandardLibraryDeclarationsRecordKey(key, "engine", "go1.27"),
			standardLibraryDeclarationsRecordKeyAt(moved, key, "engine", "go1.27")},
	} {
		if putError := store.Put(kind.today, record); putError != nil {
			test.Fatal(putError)
		}
		if _, isHit := store.Get(kind.today); !isHit {
			test.Errorf("%s: want the record a hit under today's key", kind.name)
		}
		if _, isHit := store.Get(kind.after); isHit {
			test.Errorf("%s: want the record a miss once the whole records' version moves, got a hit", kind.name)
		}
	}
}
