package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// samplePackPlace is a pack's place for the tests, samplePackEntries what it holds: one entry read for its own key, one
// read through an alias, whose asked and stored keys differ, and an empty one, and samplePackStamps its shards' stamps.
var (
	samplePackPlace = PackPlace{Identity: PackIdentity("test executable"),
		Name: PackName([]string{"test"}, []string{"pack"})}
	samplePackStamps  = map[byte]int64{0x01: 1_700_000_000_123_456_789, 0x03: -1, 0x04: 0}
	samplePackEntries = []PackEntry{
		{Asked: Key{0x01}, Stored: Key{0x01}, Entry: []byte("a record held whole")},
		{Asked: Key{0x02}, Stored: Key{0x03}, Entry: []byte("a record read through an alias")},
		{Asked: Key{0x04}, Stored: Key{0x04}, Entry: []byte{}},
	}
)

// A pack written is read back whole, each entry under the key it was asked by with the key it was stored under, and
// each shard's stamp, and lives in the store's `packs` directory, in its executable identity's, where no Get reads it.
func TestAPackWrittenIsReadBack(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir(), nil)
	if _, refused := store.ReadPack(samplePackPlace); refused != PackMissing {
		test.Fatalf("an empty store's pack was refused as %q, want %q", refused, PackMissing)
	}
	if writeError := store.WritePack(samplePackPlace, samplePackEntries, samplePackStamps); writeError != nil {
		test.Fatal(writeError)
	}
	if _, statError := os.Stat(filepath.Join(store.directory, "packs", samplePackPlace.Identity.String(),
		samplePackPlace.Name.String())); statError != nil {
		test.Fatalf("the pack is not in cohere-go's directory of the store's packs directory: %v", statError)
	}
	read, refused := store.ReadPack(samplePackPlace)
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

			store := Open(test.TempDir(), nil)
			if writeError := store.WritePack(samplePackPlace, samplePackEntries, samplePackStamps); writeError != nil {
				test.Fatal(writeError)
			}
			path := store.packPath(samplePackPlace)
			contents, readError := os.ReadFile(path)
			if readError != nil {
				test.Fatal(readError)
			}
			if damageError := damage.damage(path, contents); damageError != nil {
				test.Fatal(damageError)
			}
			read, refused := store.ReadPack(samplePackPlace)
			if refused != damage.want || read.Entries != nil || read.Stamps != nil {
				test.Errorf("want the pack refused as %q with nothing of it, got %q and %d entries", damage.want,
					refused, len(read.Entries))
			}
		})
	}
}

// A pack whose contents match their sum but do not read whole as stamps and entries, its stamps' count or its entries'
// past what follows, an entry cut short within its bytes, or bytes after the last entry, is refused whole as
// malformed, with no stamp and no entry of it, those read before the fault included (#b2t9xmf). Each holds two
// entries of fifty bytes and one stamp; its sum is made again, so only its contents refuse it.
func TestASummedButMalformedPackIsRefusedWhole(test *testing.T) {
	test.Parallel()

	entries := []PackEntry{
		{Asked: Key{0x01}, Stored: Key{0x01}, Entry: bytes.Repeat([]byte{0x01}, 50)},
		{Asked: Key{0x02}, Stored: Key{0x02}, Entry: bytes.Repeat([]byte{0x02}, 50)},
	}
	// The stamps' count follows the magic and the version, one byte for one stamp, and the entries' count follows the
	// one stamp's bytes, one byte for two entries.
	stampCount := len(packMagic) + 4
	entryCount := stampCount + 1 + stampSize
	for name, malform := range map[string]func(body []byte) []byte{
		"the stamps' count past what follows": func(body []byte) []byte {
			body[stampCount] = 100
			return body
		},
		"the entries' count past what follows": func(body []byte) []byte {
			// Three entries fit the bytes left by count alone, so only reading the third finds it missing.
			body[entryCount] = 3
			return body
		},
		"an entry cut short": func(body []byte) []byte {
			return body[:len(body)-10]
		},
		"bytes after the last entry": func(body []byte) []byte {
			return append(body, 0x00)
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			store := Open(test.TempDir(), nil)
			if writeError := store.WritePack(samplePackPlace, entries, map[byte]int64{0x01: 1}); writeError != nil {
				test.Fatal(writeError)
			}
			path := store.packPath(samplePackPlace)
			contents, readError := os.ReadFile(path)
			if readError != nil {
				test.Fatal(readError)
			}
			if contents[sha256.Size+stampCount] != 1 || contents[sha256.Size+entryCount] != 2 {
				test.Fatalf("the counts are not where the test reads them: %x", contents[sha256.Size:][:entryCount+1])
			}
			body := malform(slices.Clone(contents[sha256.Size:]))
			sum := sha256.Sum256(body)
			if writeError := os.WriteFile(path, append(sum[:], body...), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			read, refused := store.ReadPack(samplePackPlace)
			if refused != PackMalformed || read.Entries != nil || read.Stamps != nil {
				test.Errorf("want the pack refused as %q with nothing of it, got %q, %d entries and %d stamps",
					PackMalformed, refused, len(read.Entries), len(read.Stamps))
			}
		})
	}
}

// Two writes of one pack side by side, as two runs saving the same build at once, leave one of theirs whole: a reader
// reading as they write reads a pack whole every time, and the pack left is one writer's, with no temporary file
// behind.
func TestPacksWrittenSideBySideLeaveOneWhole(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir(), nil)
	// Each writer's entries are its own, and large enough that a write is many system calls, so a reader would catch
	// one half written were the pack written in place.
	entriesOf := func(writer int) []PackEntry {
		return []PackEntry{
			{Asked: Key{0x01}, Stored: Key{byte(writer)}, Entry: bytes.Repeat([]byte{byte(writer)}, 1<<20)},
		}
	}
	if writeError := store.WritePack(samplePackPlace, entriesOf(0), nil); writeError != nil {
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
				if writeError := store.WritePack(samplePackPlace, entriesOf(writer+1), nil); writeError != nil {
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
			if _, refused := store.ReadPack(samplePackPlace); refused != "" {
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
	read, refused := store.ReadPack(samplePackPlace)
	entry, held := read.Entries[Key{0x01}]
	if refused != "" || !held || len(entry.Entry) != 1<<20 || entry.Stored[0] != entry.Entry[0] ||
		!bytes.Equal(entry.Entry, bytes.Repeat(entry.Entry[:1], 1<<20)) {
		test.Fatalf("want one writer's whole pack left, got %q, held %v, %d bytes", refused, held, len(entry.Entry))
	}
	files, listError := os.ReadDir(filepath.Dir(store.packPath(samplePackPlace)))
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
	store := OpenAt(directory, nil)
	unread := PackPlace{Identity: PackIdentity("an executable"), Name: PackName([]string{"unread"})}
	read := PackPlace{Identity: PackIdentity("an executable"), Name: PackName([]string{"read"})}
	for _, place := range []PackPlace{unread, read} {
		if writeError := store.WritePack(place, samplePackEntries, nil); writeError != nil {
			test.Fatal(writeError)
		}
		aged := time.Now().Add(-6 * 24 * time.Hour)
		if timesError := os.Chtimes(store.packPath(place), aged, aged); timesError != nil {
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
	cappedStore := OpenAt(capped, nil)
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

// packOf is a pack of the executable named executable in store, its time set to at, as the last run of it to write or
// read it left it.
func packOf(test *testing.T, store *Store, executable string, at time.Time) PackPlace {
	test.Helper()
	place := PackPlace{Identity: PackIdentity(executable), Name: PackName([]string{executable})}
	if writeError := store.WritePack(place, samplePackEntries, nil); writeError != nil {
		test.Fatal(writeError)
	}
	if timesError := os.Chtimes(store.packPath(place), at, at); timesError != nil {
		test.Fatal(timesError)
	}
	return place
}

// A sweep keeps the packs of the keptOtherIdentities other executables whose newest pack is the newest, so two
// executables taking turns over one tree both stay settled, and removes the rest's, file by file and then the
// directory (#frh9sp4). An identity whose directory holds no pack reads as the oldest, and the sweep's own identity
// stays however old its packs are.
func TestASweepKeepsTheMostRecentlyUsedOtherIdentities(test *testing.T) {
	test.Parallel()

	for name, swept := range map[string]struct {
		// newestFirst are the other executables holding a pack, each an hour older than the one before it.
		newestFirst []string
		// empty are the other executables whose directory holds no pack.
		empty   []string
		removed []string
	}{
		"the oldest of three":   {newestFirst: []string{"x", "y", "z"}, removed: []string{"z"}},
		"one holding no pack":   {newestFirst: []string{"x", "y"}, empty: []string{"e"}, removed: []string{"e"}},
		"no more than it keeps": {newestFirst: []string{"x", "y"}},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			store := Open(test.TempDir(), nil)
			now := time.Now()
			// The sweep's own pack is the oldest of all, a month old, so only its identity keeps it.
			own := packOf(test, store, "w", now.Add(-30*24*time.Hour))
			for index, executable := range swept.newestFirst {
				packOf(test, store, executable, now.Add(-time.Duration(index)*time.Hour))
			}
			for _, executable := range swept.empty {
				directory := filepath.Join(store.directory, packDirectoryName, PackIdentity(executable).String())
				if makeError := os.MkdirAll(directory, 0o755); makeError != nil {
					test.Fatal(makeError)
				}
			}
			store.RemoveOtherIdentitiesPacks(own.Identity)
			if _, refused := store.ReadPack(own); refused != "" {
				test.Errorf("the sweep's own pack, the oldest, was %s", refused)
			}
			for _, executable := range append(slices.Clone(swept.newestFirst), swept.empty...) {
				directory := filepath.Join(store.directory, packDirectoryName, PackIdentity(executable).String())
				_, statError := os.Stat(directory)
				if want := slices.Contains(swept.removed, executable); want != os.IsNotExist(statError) {
					test.Errorf("want %s's packs removed %v, got its directory %v", executable, want, statError)
				}
			}
		})
	}
}

// Other executables whose newest packs are as new are chosen by their directories' names, the first kept, so every
// sweep keeps the same ones (#frh9sp4).
func TestASweepChoosesBetweenIdentitiesAsNewByName(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir(), nil)
	at := time.Now().Add(-time.Hour)
	executables := []string{"x", "y", "z"}
	for _, executable := range executables {
		packOf(test, store, executable, at)
	}
	byName := slices.SortedFunc(slices.Values(executables), func(left string, right string) int {
		return strings.Compare(PackIdentity(left).String(), PackIdentity(right).String())
	})
	store.RemoveOtherIdentitiesPacks(PackIdentity("w"))
	for index, executable := range byName {
		_, statError := os.Stat(filepath.Join(store.directory, packDirectoryName, PackIdentity(executable).String()))
		if want := index >= keptOtherIdentities; want != os.IsNotExist(statError) {
			test.Errorf("want %s's packs, by its name %d of %d, removed %v, got its directory %v", executable,
				index+1, len(byName), want, statError)
		}
	}
}

// Two executables taking turns over one store, as a dev build beside a release or a timing pair's main and head do,
// never remove each other's pack: x writes its pack and sweeps, y writes its own and sweeps, and x sweeps again, and
// each pack is there throughout, so neither's run finds its own gone and reads every record one by one (#frh9sp4).
func TestTwoExecutablesTakingTurnsKeepEachOthersPacks(test *testing.T) {
	test.Parallel()

	// A store holding no stamps sweeps at each ask, as each run's first write does.
	store := Open(test.TempDir(), nil)
	x := PackPlace{Identity: PackIdentity("x"), Name: PackName([]string{"x"})}
	y := PackPlace{Identity: PackIdentity("y"), Name: PackName([]string{"y"})}
	for turn, sweep := range []struct {
		place PackPlace
		write bool
	}{{place: x, write: true}, {place: y, write: true}, {place: x}} {
		if sweep.write {
			if writeError := store.WritePack(sweep.place, samplePackEntries, nil); writeError != nil {
				test.Fatal(writeError)
			}
		}
		store.RemoveOtherIdentitiesPacks(sweep.place.Identity)
		for _, held := range []PackPlace{x, y}[:min(turn+1, 2)] {
			if _, refused := store.ReadPack(held); refused != "" {
				test.Errorf("after turn %d, the pack of %x was %s", turn+1, held.Identity[:2], refused)
			}
		}
	}
}
