package go_engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// projectStoreDirectory is the findings store of the project at root, whose packs are in its `packs` directory.
func projectStoreDirectory(root string) string {
	return filepath.Join(root, ".cache", "cohere", "go")
}

// packFiles are the pack files of every store directory of directories, sorted.
func packFiles(test *testing.T, directories ...string) []string {
	test.Helper()
	var files []string
	for _, directory := range directories {
		listed, listError := filepath.Glob(filepath.Join(directory, "packs", "*"))
		if listError != nil {
			test.Fatal(listError)
		}
		files = append(files, listed...)
	}
	slices.Sort(files)
	return files
}

// packedKeys are the keys asked of every pack of directories, read as a run reads them, failing on one refused.
func packedKeys(test *testing.T, directories ...string) map[cache.Key]bool {
	test.Helper()
	keys := map[cache.Key]bool{}
	for _, file := range packFiles(test, directories...) {
		read, refused := cache.OpenAt(filepath.Dir(filepath.Dir(file))).ReadPack(packNameOf(test, file))
		if refused != "" {
			test.Fatalf("the pack %s was refused as %s", file, refused)
		}
		for asked := range read.Entries {
			keys[asked] = true
		}
	}
	return keys
}

// packingLater has a run share the system cache at systemRoot, as sharingSystemCache does, and judge its builds' shard
// stamps by a clock an hour ahead, so every stamp of the test's stores is far enough behind its reading to be trusted,
// as a run's would be had its stores been written a while before (#b2t9xmf).
func packingLater(systemRoot string) func(*pipeline) {
	return func(engine *pipeline) {
		sharingSystemCache(systemRoot)(engine)
		engine.shardStamps = cache.NewShardStamps(func() time.Time { return time.Now().Add(time.Hour) })
	}
}

// packsOf is the lint record's count of what the builds did with their packs, failing when it has none.
func packsOf(test *testing.T, stream string) contract.BuildPacks {
	test.Helper()
	packs := lintRecordOf(test, stream).Cache.Packs
	if packs == nil {
		test.Fatalf("the lint record counts no packs:\n%s", stream)
	}
	return *packs
}

// A warm run checking every build has each extra build open no record file for a key its packs hold (#b2t9xmf): the
// packs, read once, serve every record the build asks, and since they held everything the run read, none is written
// again. The cold run packs what it wrote, under stamps its own writes moved past, and the run after it packs what it
// read, under stamps taken before; the third is warm. The host's session keeps none, and the other-builds fixture's
// builds key apart from it, by target or tags, so no key the host reads is a pack's.
//
// Not parallel: cache.ObserveReads is the process's, and one observer at a time can be set.
func TestAWarmBuildOpensNoRecordItsPacksHold(test *testing.T) {
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	systemRoot := test.TempDir()
	sharing := packingLater(systemRoot)
	runOverConfigured(test, root, exitFindings, sharing, "--other-builds")
	runOverConfigured(test, root, exitFindings, sharing, "--other-builds")
	held := packedKeys(test, projectStoreDirectory(root), systemStoreDirectory(systemRoot))
	if len(held) == 0 {
		test.Fatal("the warm runs left no pack holding a key")
	}
	var counting sync.Mutex
	opened := 0
	stop := cache.ObserveReads(func(key cache.Key) {
		counting.Lock()
		defer counting.Unlock()
		if held[key] {
			opened++
		}
	})
	_, stream := runOverConfigured(test, root, exitFindings, sharing, "--other-builds")
	stop()
	if opened != 0 {
		test.Errorf("the warm run opened %d record files for keys of the %d its packs hold, want none", opened,
			len(held))
	}
	packs := packsOf(test, stream)
	if packs.Read == 0 || packs.RecordsServed == 0 || packs.RecordsStale != 0 || packs.RecordsMissed != 0 ||
		packs.Missing != 0 || packs.Written != 0 || len(packs.RefusedBy) != 0 {
		test.Errorf("want the packs read, serving every record, none missing, refused or written, got %+v", packs)
	}
}

// packDamage is a damage done to the pack file at path, its contents as the run wrote them.
type packDamage func(path string, contents []byte) error

// A pack changed by a byte, cut short or of another format is refused whole and counted by why, each build reading its
// stores as with no pack, and the run's findings are those of a run with no pack at all (#b2t9xmf).
func TestADamagedPackIsRefusedAndChangesNoFinding(test *testing.T) {
	test.Parallel()

	for name, damage := range map[string]struct {
		damage packDamage
		want   string
	}{
		"a byte changed": {want: cache.PackSum, damage: func(path string, contents []byte) error {
			contents[len(contents)-1] ^= 0xFF
			return os.WriteFile(path, contents, 0o644)
		}},
		"cut short": {want: cache.PackSum, damage: func(path string, contents []byte) error {
			// Past the sum, so it is the sum that refuses it, a pack of no record included.
			return os.Truncate(path, int64(max(len(contents)/2, sha256.Size+1)))
		}},
		"another format": {want: cache.PackVersion, damage: func(path string, contents []byte) error {
			// The format's version follows the magic, which ends in the first zero byte after the sum; the sum is
			// made again, so only the version refuses it.
			body := contents[sha256.Size:]
			version := bytes.IndexByte(body, 0) + 1
			binary.BigEndian.PutUint32(body[version:], binary.BigEndian.Uint32(body[version:])+1)
			sum := sha256.Sum256(body)
			return os.WriteFile(path, append(sum[:], body...), 0o644)
		}},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			root := copyFixture(test, filepath.Join("testdata", "other-builds"))
			systemRoot := test.TempDir()
			sharing := packingLater(systemRoot)
			runOverConfigured(test, root, exitFindings, sharing, "--other-builds")
			files := packFiles(test, projectStoreDirectory(root), systemStoreDirectory(systemRoot))
			if len(files) == 0 {
				test.Fatal("the cold run wrote no pack")
			}
			for _, file := range files {
				contents, readError := os.ReadFile(file)
				if readError != nil {
					test.Fatal(readError)
				}
				if damageError := damage.damage(file, contents); damageError != nil {
					test.Fatal(damageError)
				}
			}
			_, damaged := runOverConfigured(test, root, exitFindings, sharing, "--other-builds")
			packs := packsOf(test, damaged)
			if packs.Read != 0 || packs.Missing != 0 || packs.RecordsServed != 0 ||
				packs.RefusedBy[damage.want] != len(files) || len(packs.RefusedBy) != 1 || packs.Written != len(files) {
				test.Errorf("want each of the %d packs refused as %s and written again, none read, got %+v",
					len(files), damage.want, packs)
			}
			// The same run with no pack at all, its packs removed one by one.
			for _, file := range files {
				if removeError := os.Remove(file); removeError != nil {
					test.Fatal(removeError)
				}
			}
			_, unpacked := runOverConfigured(test, root, exitFindings, sharing, "--other-builds")
			if got, want := findingLines(damaged), findingLines(unpacked); !slices.Equal(got, want) || len(want) == 0 {
				test.Errorf("the run over damaged packs found\n%s\nthe run with none\n%s", got, want)
			}
		})
	}
}

// A record a build's pack serves whose export data its store no longer holds, as after a trim or a clearing, reads as a
// miss exactly as when read from the store, and is checked from source, and the run's findings are those of a run with
// no cache (#b2t9xmf): the pack holds records alone, never what they name. Every export data file of the stores goes
// after the cold run, and each pack is written again with its shards' stamps as they are then, as a run that read the
// records after the loss would write it, so the packs serve the records. The root package is then edited, so it misses
// in every build and reads the types of label and separator, whose records the packs serve. Windows' separator, whose
// separator is its own, has export data no other build or the host writes again, so it stays gone. The builds restore
// nothing from the host's run, so each miss is checked from source rather than restored from the host.
func TestARecordServedWhoseEntryWentIsAMiss(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	systemRoot := test.TempDir()
	sharing := func(engine *pipeline) {
		packingLater(systemRoot)(engine)
		engine.keepsHostRestoreOff = true
	}
	runOverConfigured(test, root, exitFindings, sharing, "--other-builds")
	stores := []string{projectStoreDirectory(root), systemStoreDirectory(systemRoot)}
	// An export data file is kept under the digest of the digest of its bytes (check's exportDataKey), so it is told
	// from every other file by its name alone, and each goes, one by one.
	removed := 0
	for _, directory := range stores {
		walkError := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkError error) error {
			if walkError != nil {
				return walkError
			}
			if entry.IsDir() && entry.Name() == "packs" {
				return filepath.SkipDir
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			contents, readError := os.ReadFile(path)
			if readError != nil || len(contents) < sha256.Size {
				return readError
			}
			digest := sha256.Sum256(contents[sha256.Size:])
			if cache.Key(sha256.Sum256(append([]byte("export data by content "), digest[:]...))).String() !=
				entry.Name() {
				return nil
			}
			removed++
			return os.Remove(path)
		})
		if walkError != nil {
			test.Fatal(walkError)
		}
	}
	if removed == 0 {
		test.Fatal("the cold run left no export data in the stores to remove")
	}
	for _, file := range packFiles(test, stores...) {
		store := cache.OpenAt(filepath.Dir(filepath.Dir(file)))
		name := packNameOf(test, file)
		read, refused := store.ReadPack(name)
		if refused != "" {
			test.Fatalf("the pack %s was refused as %s", file, refused)
		}
		stamps := cache.NewShardStamps(nil)
		restamped := map[byte]int64{}
		entries := make([]cache.PackEntry, 0, len(read.Entries))
		for _, entry := range read.Entries {
			entries = append(entries, entry)
			if reading, _ := stamps.Of(store, cache.ShardOf(entry.Stored)); reading.Known {
				restamped[cache.ShardOf(entry.Stored)] = reading.Stamp
			}
		}
		if writeError := store.WritePack(name, entries, restamped); writeError != nil {
			test.Fatal(writeError)
		}
	}
	// A comment at the file's end changes the root package's key and moves no line any finding names.
	joined := filepath.Join(root, "builds.go")
	contents, readError := os.ReadFile(joined)
	if readError != nil {
		test.Fatal(readError)
	}
	if writeError := os.WriteFile(joined, append(contents, "\n// Edited after the export data went.\n"...),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	engine, emptied := runOverConfigured(test, root, exitFindings, sharing, "--other-builds")
	packs := packsOf(test, emptied)
	if packs.RecordsServed == 0 {
		test.Fatalf("want the packs to serve the records whose export data went, got %+v", packs)
	}
	// The root package, edited, and windows' separator, whose export data went, at the least: two, where a build whose
	// store held its export data checks the root package alone.
	windows := 0
	for _, run := range engine.buildRuns[1:] {
		if run.build.String() != "windows/"+runtime.GOARCH {
			continue
		}
		windows++
		if run.checkedFromSource < 2 {
			test.Errorf("%s checked %d packages from source, want the edited root and separator, whose export data "+
				"its store lost, at the least", run.build, run.checkedFromSource)
		}
	}
	if windows != 1 {
		test.Fatalf("the run checked %d windows builds, want one", windows)
	}
	_, uncached := runOver(test, root, exitFindings, "--other-builds")
	if got, want := findingLines(normalize(emptied, root)), findingLines(uncached); !slices.Equal(got, want) ||
		len(want) == 0 {
		test.Errorf("the run over stores without their export data found\n%s\na run with no cache\n%s", got, want)
	}
}

// packNameOf is the name of the pack file at path, its key in hex.
func packNameOf(test *testing.T, path string) cache.Key {
	test.Helper()
	decoded, decodeError := hex.DecodeString(filepath.Base(path))
	if decodeError != nil || len(decoded) != len(cache.Key{}) {
		test.Fatalf("a pack file named %s, want a key's hex", path)
	}
	return cache.Key(decoded)
}
