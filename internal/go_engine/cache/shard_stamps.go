package cache

import (
	"sync"
	"time"
)

// racyStampWindow is how near the clock a shard's stamp may be, when it is read, and still be trusted, as git distrusts
// an index entry whose file changed in the second the index read it: a file system whose times are coarse, as HFS+'s
// one-second times, leaves a shard's stamp where it was across a write later in the same tick, so a stamp that close to
// its reading is never trusted, and no entry of its shard is served from a pack (#b2t9xmf). On APFS, whose times are
// in nanoseconds, it fires only for a shard written in the two seconds before the run read it.
const racyStampWindow = 2 * time.Second

// ShardStamps are a run's stamps of the shard directories of its stores, each read once a run however many builds ask
// (#b2t9xmf): a pack serves an entry only while its shard's stamp is the one the pack holds, so a run checking every
// build stats each shard of each store once rather than once a build. A stamp is the shard directory's modification
// time (shardStamp), which every change the store makes to a file of the shard moves. The first ask reads it, before
// whatever the asker reads of the shard, so a stamp a pack records is never newer than what it holds, and reads the
// clock with it, which decides whether it is trusted (racyStampWindow). A stamp is the run's from then on: a write to
// the shard later in the run moves the directory past it, and the next run, finding another stamp, reads that shard's
// entries from the store.
type ShardStamps struct {
	// clock is the time a stamp's reading is judged by, the system's but in a test.
	clock func() time.Time

	lock     sync.Mutex
	readings map[shardInStore]ShardReading
}

// shardInStore is one shard of one store, by the store's directory.
type shardInStore struct {
	directory string
	shard     byte
}

// ShardReading is a shard's stamp as the run read it: whether there was one, a directory the store holds, and whether
// it is trusted, its time more than racyStampWindow before the clock as it was read, so no write after the reading can
// have left it where it was.
type ShardReading struct {
	Stamp   int64
	Known   bool
	Trusted bool
}

// NewShardStamps is a run's stamps, none read yet, judged by clock, the system's when it is nil.
func NewShardStamps(clock func() time.Time) *ShardStamps {
	stamps := &ShardStamps{clock: clock, readings: map[shardInStore]ShardReading{}}
	if clock == nil {
		stamps.clock = time.Now
	}
	return stamps
}

// Of is the stamp of shard in store as the run read it the first time it was asked, and whether this ask read it,
// which a caller counts. The read is under the lock, so two builds asking at once read it once. The clock is read after
// the stamp, so a write landing between the two leaves a stamp the window still distrusts.
func (stamps *ShardStamps) Of(store *Store, shard byte) (ShardReading, bool) {
	stamps.lock.Lock()
	defer stamps.lock.Unlock()
	asked := shardInStore{directory: store.directory, shard: shard}
	if reading, held := stamps.readings[asked]; held {
		return reading, false
	}
	stamp, known := store.shardStamp(shard)
	readAt := stamps.clock()
	reading := ShardReading{Stamp: stamp, Known: known,
		Trusted: known && readAt.Sub(time.Unix(0, stamp)) >= racyStampWindow}
	stamps.readings[asked] = reading
	return reading, true
}
