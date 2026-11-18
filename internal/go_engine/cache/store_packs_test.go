package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// samplePackName is a pack's name for the tests, samplePackEntries what it holds: one entry read for its own key, one
// read through an alias, whose asked and stored keys differ, and an empty one, and samplePackStamps its shards' stamps.
var (
	samplePackName    = PackName([]string{"test"}, []string{"pack"})
	samplePackStamps  = map[byte]int64{0x01: 1_700_000_000_123_456_789, 0x03: -1, 0x04: 0}
	samplePackEntries = []PackEntry{
		{Asked: Key{0x01}, Stored: Key{0x01}, Entry: []byte("a record held whole")},
		{Asked: Key{0x02}, Stored: Key{0x03}, Entry: []byte("a record read through an alias")},
		{Asked: Key{0x04}, Stored: Key{0x04}, Entry: []byte{}},
	}
)

// A pack written is read back whole, each entry under the key it was asked by with the key it was stored under, and
// each shard's stamp, and lives in the store's `packs` directory, where no Get reads it.
func TestAPackWrittenIsReadBack(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir())
	if _, refused := store.ReadPack(samplePackName); refused != PackMissing {
		test.Fatalf("an empty store's pack was refused as %q, want %q", refused, PackMissing)
	}
	if writeError := store.WritePack(samplePackName, samplePackEntries, samplePackStamps); writeError != nil {
		test.Fatal(writeError)
	}
	if _, statError := os.Stat(filepath.Join(store.directory, "packs", samplePackName.String())); statError != nil {
		test.Fatalf("the pack is not in the store's packs directory: %v", statError)
	}
	read, refused := store.ReadPack(samplePackName)
	if refused != "" {
		test.Fatalf("the pack written was refused as %q", refused)
	}
	if len(read.Entries) != len(samplePackEntries) || !maps.Equal(read.Stamps, samplePackStamps) {
		test.Fatalf("want %d entries and the stamps %v back, got %d and %v", len(samplePackEntries), samplePackStamps,
			len(read.Entries), read.Stamps)
	}
	for _, want := range samplePackEntries {
		got, held := read.Entries[want.Asked]
		if !held || got.Stored != want.Stored || !bytes.Equal(got.Entry, want.Entry) {
			test.Errorf("want %x stored under %x as %q, got %+v (held %v)", want.Asked[:1], want.Stored[:1], want.Entry,
				got, held)
		}
	}
}

// damagedPack is a damage done to the pack file at path, its contents as WritePack wrote them.
type damagedPack func(path string, contents []byte) error

// A pack changed by a byte, cut short past its sum or within it, or of another format whose sum still matches, is
// refused whole with the first word of why, and gives no entry.
func TestADamagedPackIsRefusedWhole(test *testing.T) {
	test.Parallel()

	for name, damage := range map[string]struct {
		damage damagedPack
		want   string
	}{
		"a byte changed": {want: PackSum, damage: func(path string, contents []byte) error {
			contents[len(contents)-1] ^= 0xFF
			return os.WriteFile(path, contents, 0o644)
		}},
		"cut short": {want: PackSum, damage: func(path string, contents []byte) error {
			return os.Truncate(path, int64(len(contents)/2))
		}},
		"cut in its sum": {want: PackShort, damage: func(path string, _ []byte) error {
			return os.Truncate(path, 10)
		}},
		"another format": {want: PackVersion, damage: func(path string, contents []byte) error {
			// The version follows the magic; the sum is made again, so only the version refuses it.
			body := contents[sha256.Size:]
			binary.BigEndian.PutUint32(body[len(packMagic):], packFormatVersion+1)
			sum := sha256.Sum256(body)
			return os.WriteFile(path, append(sum[:], body...), 0o644)
		}},
		"unreadable": {want: PackUnreadable, damage: func(path string, _ []byte) error {
			return replaceWithDirectory(path)
		}},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			store := Open(test.TempDir())
			if writeError := store.WritePack(samplePackName, samplePackEntries, samplePackStamps); writeError != nil {
				test.Fatal(writeError)
			}
			path := store.packPath(samplePackName)
			contents, readError := os.ReadFile(path)
			if readError != nil {
				test.Fatal(readError)
			}
			if damageError := damage.damage(path, contents); damageError != nil {
				test.Fatal(damageError)
			}
			read, refused := store.ReadPack(samplePackName)
			if refused != damage.want || read.Entries != nil || read.Stamps != nil {
				test.Errorf("want the pack refused as %q with nothing of it, got %q and %d entries", damage.want,
					refused, len(read.Entries))
			}
		})
	}
}

// Two writes of one pack side by side, as two runs saving the same build at once, leave one of theirs whole: a reader
// reading as they write reads a pack whole every time, and the pack left is one writer's, with no temporary file
// behind.
func TestPacksWrittenSideBySideLeaveOneWhole(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir())
	// Each writer's entries are its own, and large enough that a write is many system calls, so a reader would catch
	// one half written were the pack written in place.
	entriesOf := func(writer int) []PackEntry {
		return []PackEntry{
			{Asked: Key{0x01}, Stored: Key{byte(writer)}, Entry: bytes.Repeat([]byte{byte(writer)}, 1<<20)},
		}
	}
	if writeError := store.WritePack(samplePackName, entriesOf(0), nil); writeError != nil {
		test.Fatal(writeError)
	}
	const writers = 2
	const rounds = 40
	var writing sync.WaitGroup
	var done atomic.Bool
	writeErrors := make([]error, writers)
	for writer := range writers {
		writing.Go(func() {
			for range rounds {
				if writeError := store.WritePack(samplePackName, entriesOf(writer+1), nil); writeError != nil {
					writeErrors[writer] = writeError
					return
				}
			}
		})
	}
	var reading sync.WaitGroup
	var refusals atomic.Int64
	var firstRefusal atomic.Value
	reading.Go(func() {
		for !done.Load() {
			if _, refused := store.ReadPack(samplePackName); refused != "" {
				refusals.Add(1)
				firstRefusal.CompareAndSwap(nil, refused)
			}
		}
	})
	writing.Wait()
	done.Store(true)
	reading.Wait()
	for writer, writeError := range writeErrors {
		if writeError != nil {
			test.Errorf("writer %d: %v", writer, writeError)
		}
	}
	if refusals.Load() > 0 {
		test.Errorf("a reader beside the writers found the pack refused %d times, the first as %v", refusals.Load(),
			firstRefusal.Load())
	}
	read, refused := store.ReadPack(samplePackName)
	entry, held := read.Entries[Key{0x01}]
	if refused != "" || !held || len(entry.Entry) != 1<<20 || entry.Stored[0] != entry.Entry[0] ||
		!bytes.Equal(entry.Entry, bytes.Repeat(entry.Entry[:1], 1<<20)) {
		test.Fatalf("want one writer's whole pack left, got %q, held %v, %d bytes", refused, held, len(entry.Entry))
	}
	files, listError := os.ReadDir(filepath.Dir(store.packPath(samplePackName)))
	if listError != nil {
		test.Fatal(listError)
	}
	if len(files) != 1 {
		test.Errorf("want the pack alone in the packs directory, got %d files", len(files))
	}
}

// The system cache's trim deletes a pack no run has read for five days, as it deletes any entry, keeps one a run marked
// used (MarkPackUsed), and counts a pack in the size cap: past it, an older pack goes before a newer entry.
func TestTheTrimTakesAPackAsAnEntry(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	directory := filepath.Join(root, "go", "store")
	store := OpenAt(directory)
	unread := PackName([]string{"unread"})
	read := PackName([]string{"read"})
	for _, name := range []Key{unread, read} {
		if writeError := store.WritePack(name, samplePackEntries, nil); writeError != nil {
			test.Fatal(writeError)
		}
		aged := time.Now().Add(-6 * 24 * time.Hour)
		if timesError := os.Chtimes(store.packPath(name), aged, aged); timesError != nil {
			test.Fatal(timesError)
		}
	}
	store.MarkPackUsed(read)
	if notKept := TrimSystemCache(root, directory, SystemCacheSizeCap); notKept != nil {
		test.Fatalf("the trim failed: %s", notKept)
	}
	if present(store.packPath(unread)) {
		test.Error("a pack unread for six days is still there")
	}
	if !present(store.packPath(read)) {
		test.Error("a pack a run marked used was deleted")
	}

	// Under a cap that holds the newest entry alone, the pack, read an hour before it, goes, which only a trim that
	// counts the pack's bytes does: without them the files left fit.
	capped := filepath.Join(test.TempDir(), "go", "store")
	cappedStore := OpenAt(capped)
	if writeError := cappedStore.WritePack(read, samplePackEntries, nil); writeError != nil {
		test.Fatal(writeError)
	}
	hourAgo := time.Now().Add(-2 * time.Hour)
	if timesError := os.Chtimes(cappedStore.packPath(read), hourAgo, hourAgo); timesError != nil {
		test.Fatal(timesError)
	}
	newest := filepath.Join(capped, "ab", "newest")
	writeAgedFile(test, newest, 100, time.Hour)
	if notKept := TrimSystemCache(filepath.Dir(filepath.Dir(capped)), capped, 150); notKept != nil {
		test.Fatalf("the trim failed: %s", notKept)
	}
	if present(cappedStore.packPath(read)) {
		test.Error("a pack past the size cap, older than the entry kept, is still there")
	}
	if !present(newest) {
		test.Error("the newest entry, within the cap alone, was deleted")
	}
}
