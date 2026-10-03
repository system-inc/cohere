package main

import (
	"path/filepath"
	"time"
)

// The table's lock, which keeps back-to-back runs in order once a run can answer its caller before it has
// written the cache table (sendVerdict).
//
// `s c` runs cohere's fix step and then its check, one straight after the other. With an early return the
// second starts while the first is still recording, so it could read the table before the first one's
// rename lands, and whichever rename came last would win, dropping what the other recorded. Neither is
// ever corruption, since every entry carries its own proof, only a miss; but a miss on exactly the run
// that follows is the case a cache exists for (#zqsdzbq, build's review of 4a0fdbc).
//
// So a run that will write after answering holds the lock from before its verdict until its table is in
// place, and every run that reads the table waits for that, briefly, before reading. A lone run finds no
// holder and pays an open and a lock, a few microseconds.

// tableWriterWait bounds how long a run waits for the previous run's write. Past it the run goes ahead
// and the worst outcome is a miss, never a stale hit, so a writer stuck for any reason costs a run at most
// this much.
const tableWriterWait = 2 * time.Second

// tableLockPath is the lock beside a cache table.
func tableLockPath(tablePath string) string {
	return filepath.Join(filepath.Dir(tablePath), "table.lock")
}
