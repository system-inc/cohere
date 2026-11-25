//go:build darwin || linux

package program

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/microsoft/TypeScript/tsc/shim/vfs/osvfs"
)

// packFixture is a directory of files and a cache directory beside it, read through a fresh content pack
// each "run", the way successive cohere runs read a project.
type packFixture struct {
	test  *testing.T
	files string
	cache string
}

func newPackFixture(test *testing.T) *packFixture {
	root := test.TempDir()
	fixture := &packFixture{test: test, files: filepath.Join(root, "files"), cache: filepath.Join(root, "cache")}
	if createDirectoryError := os.MkdirAll(fixture.files, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	return fixture
}

func (fixture *packFixture) path(name string) string { return filepath.Join(fixture.files, name) }

func (fixture *packFixture) write(name string, contents string) {
	fixture.test.Helper()
	if writeError := os.WriteFile(fixture.path(name), []byte(contents), 0o644); writeError != nil {
		fixture.test.Fatal(writeError)
	}
}

// run opens the pack, reads every name through it, saves, and returns what it read and the pack's counts.
func (fixture *packFixture) run(names ...string) (map[string]string, int, int, int) {
	fixture.test.Helper()
	pack, openContentPackError := OpenContentPack(fixture.cache)
	if openContentPackError != nil {
		fixture.test.Logf("open: %v", openContentPackError)
	}
	fileSystem := pack.wrap(osvfs.FS())
	read := map[string]string{}
	for _, name := range names {
		contents, ok := fileSystem.ReadFile(tspath.RootedFilePath(fixture.path(name)))
		if !ok {
			fixture.test.Fatalf("%s could not be read", name)
		}
		read[name] = contents
	}
	if saveError := pack.Save(); saveError != nil {
		fixture.test.Fatalf("saving the pack: %v", saveError)
	}
	hits, misses, damaged := pack.Counts()
	return read, hits, misses, damaged
}

func (fixture *packFixture) dataFile() string {
	fixture.test.Helper()
	matches, globError := filepath.Glob(filepath.Join(fixture.cache, "contents-*.pack"))
	if globError != nil {
		fixture.test.Fatalf("listing the data files: %v", globError)
	}
	if len(matches) != 1 {
		fixture.test.Fatalf("want one data file, found %v", matches)
	}
	return matches[0]
}

// A file read once is served from the pack the next time, unopened, and with the same bytes.
func TestTheContentPackServesAnUnchangedFile(test *testing.T) {
	test.Parallel()
	fixture := newPackFixture(test)
	fixture.write("a.ts", "export const a = 1;\n")
	fixture.write("b.ts", "export const b = 2;\n")
	if _, hits, misses, _ := fixture.run("a.ts", "b.ts"); hits != 0 || misses != 2 {
		test.Fatalf("an empty pack served %d and read %d, want 0 and 2", hits, misses)
	}
	read, hits, misses, damaged := fixture.run("a.ts", "b.ts")
	if hits != 2 || misses != 0 || damaged != 0 {
		test.Fatalf("the second run served %d, read %d and found %d damaged, want 2, 0 and 0", hits, misses, damaged)
	}
	if read["a.ts"] != "export const a = 1;\n" || read["b.ts"] != "export const b = 2;\n" {
		test.Fatalf("the pack served %q", read)
	}
}

// Every way a file's bytes can change is a read from disk, the change time catching the one the
// modification time cannot: different bytes of the same size with the modification time put back, as
// cp -p, rsync -t and touch -r all do. A replace by rename is a new inode.
func TestTheContentPackNeverServesChangedBytes(test *testing.T) {
	test.Parallel()
	for _, change := range []struct {
		name   string
		change func(fixture *packFixture)
	}{
		{"a different size", func(fixture *packFixture) { fixture.write("a.ts", "export const a = 100;\n") }},
		{"the same size", func(fixture *packFixture) { fixture.write("a.ts", "export const a = 9;\n") }},
		{"the same size with the modification time restored", func(fixture *packFixture) {
			information, statError := os.Stat(fixture.path("a.ts"))
			if statError != nil {
				fixture.test.Fatal(statError)
			}
			fixture.write("a.ts", "export const a = 9;\n")
			if changeTimesError := os.Chtimes(
				fixture.path("a.ts"),
				information.ModTime(),
				information.ModTime(),
			); changeTimesError != nil {
				fixture.test.Fatal(changeTimesError)
			}
			after, statAfterError := os.Stat(fixture.path("a.ts"))
			if statAfterError != nil {
				fixture.test.Fatal(statAfterError)
			}
			if !after.ModTime().Equal(information.ModTime()) || after.Size() != information.Size() {
				fixture.test.Fatal("the fixture did not restore the size and modification time, so this proves nothing")
			}
		}},
		{"a replace by rename, size and modification time kept", func(fixture *packFixture) {
			information, statError := os.Stat(fixture.path("a.ts"))
			if statError != nil {
				fixture.test.Fatal(statError)
			}
			fixture.write("replacement.ts", "export const a = 9;\n")
			if changeTimesError := os.Chtimes(
				fixture.path("replacement.ts"),
				information.ModTime(),
				information.ModTime(),
			); changeTimesError != nil {
				fixture.test.Fatal(changeTimesError)
			}
			if renameError := os.Rename(fixture.path("replacement.ts"), fixture.path("a.ts")); renameError != nil {
				fixture.test.Fatal(renameError)
			}
		}},
	} {
		test.Run(change.name, func(test *testing.T) {
			test.Parallel()
			fixture := newPackFixture(test)
			fixture.test = test
			fixture.write("a.ts", "export const a = 1;\n")
			fixture.run("a.ts")
			// Change times move in nanoseconds, but a filesystem may store coarser ones.
			time.Sleep(10 * time.Millisecond)
			change.change(fixture)
			read, hits, _, _ := fixture.run("a.ts")
			want, readError := os.ReadFile(fixture.path("a.ts"))
			if readError != nil {
				test.Fatal(readError)
			}
			if read["a.ts"] != string(want) || hits != 0 {
				test.Fatalf(
					"after %s the pack served %q (%d hits), and the disk holds %q",
					change.name,
					read["a.ts"],
					hits,
					want,
				)
			}
			// And the new bytes are what the next run is served.
			if read, hits, _, _ := fixture.run("a.ts"); read["a.ts"] != string(want) || hits != 1 {
				test.Fatalf(
					"the run after the change was served %q (%d hits), want %q from the pack",
					read["a.ts"],
					hits,
					want,
				)
			}
		})
	}
}

// A damaged pack costs reads from disk and never a wrong byte, and the next write leaves a sound pack.
func TestADamagedContentPackIsReadAround(test *testing.T) {
	test.Parallel()
	for _, damage := range []struct {
		name        string
		damage      func(fixture *packFixture)
		damagedRead bool
	}{
		{"a flipped byte in the data", func(fixture *packFixture) {
			data, readError := os.ReadFile(fixture.dataFile())
			if readError != nil {
				fixture.test.Fatal(readError)
			}
			data[3] ^= 0xff
			if writeError := os.WriteFile(fixture.dataFile(), data, 0o644); writeError != nil {
				fixture.test.Fatal(writeError)
			}
		}, true},
		{"a data file truncated to half", func(fixture *packFixture) {
			information, statError := os.Stat(fixture.dataFile())
			if statError != nil {
				fixture.test.Fatal(statError)
			}
			if truncateError := os.Truncate(fixture.dataFile(), information.Size()/2); truncateError != nil {
				fixture.test.Fatal(truncateError)
			}
		}, true},
		{"a flipped byte in the index", func(fixture *packFixture) {
			index := filepath.Join(fixture.cache, contentPackIndexName)
			data, readError := os.ReadFile(index)
			if readError != nil {
				fixture.test.Fatal(readError)
			}
			data[len(data)/2] ^= 0xff
			if writeError := os.WriteFile(index, data, 0o644); writeError != nil {
				fixture.test.Fatal(writeError)
			}
		}, false},
		{"a data file deleted", func(fixture *packFixture) {
			if removeError := os.Remove(fixture.dataFile()); removeError != nil {
				fixture.test.Fatal(removeError)
			}
		}, false},
	} {
		test.Run(damage.name, func(test *testing.T) {
			test.Parallel()
			fixture := newPackFixture(test)
			fixture.test = test
			fixture.write("a.ts", "export const a = 1;\n")
			fixture.write("b.ts", "export const b = 2;\n")
			fixture.run("a.ts", "b.ts")
			damage.damage(fixture)
			read, hits, _, damaged := fixture.run("a.ts", "b.ts")
			if read["a.ts"] != "export const a = 1;\n" || read["b.ts"] != "export const b = 2;\n" {
				test.Fatalf("a damaged pack served %q", read)
			}
			if damage.damagedRead && damaged == 0 {
				test.Errorf("the damage was read around without being counted, so the pack would not be rewritten")
			}
			if read, hits, _, damaged := fixture.run(
				"a.ts",
				"b.ts",
			); hits+damaged == 0 || damaged != 0 || read["b.ts"] != "export const b = 2;\n" {
				test.Fatalf(
					"the run after the damage was served %d, found %d damaged: the pack was not rewritten sound",
					hits,
					damaged,
				)
			}
			_ = hits
		})
	}
}

// A pack truncated in place while a run has it mapped faults on the copy. The fault is a read from disk,
// not the end of the process.
func TestAContentPackTruncatedUnderItsMappingIsReadAround(test *testing.T) {
	test.Parallel()
	fixture := newPackFixture(test)
	fixture.write("a.ts", strings.Repeat("export const a = 1;\n", 4096))
	fixture.run("a.ts")
	pack, openContentPackError := OpenContentPack(fixture.cache)
	if openContentPackError != nil {
		test.Fatal(openContentPackError)
	}
	if truncateError := os.Truncate(fixture.dataFile(), 0); truncateError != nil {
		test.Fatal(truncateError)
	}
	contents, ok := pack.wrap(osvfs.FS()).ReadFile(tspath.RootedFilePath(fixture.path("a.ts")))
	if !ok || contents != strings.Repeat("export const a = 1;\n", 4096) {
		test.Fatalf("a pack truncated under its mapping served %d bytes", len(contents))
	}
	if _, _, damaged := pack.Counts(); damaged != 1 {
		test.Errorf("the fault was read around without being counted (%d damaged)", damaged)
	}
}

// A run that reads one new file appends it rather than rewriting the pack, and a pack mostly dead is
// compacted into a new data file, the old one removed.
func TestTheContentPackAppendsAndCompacts(test *testing.T) {
	test.Parallel()
	fixture := newPackFixture(test)
	fixture.write("a.ts", strings.Repeat("a", 1000))
	fixture.write("b.ts", strings.Repeat("b", 1000))
	fixture.run("a.ts", "b.ts")
	first := fixture.dataFile()

	fixture.write("c.ts", "export const c = 3;\n")
	if _, hits, misses, _ := fixture.run("a.ts", "b.ts", "c.ts"); hits != 2 || misses != 1 {
		test.Fatalf("served %d and read %d, want 2 and 1", hits, misses)
	}
	if fixture.dataFile() != first {
		test.Fatal("one new file rewrote the data file instead of appending to it")
	}
	if read, hits, _, _ := fixture.run("a.ts", "b.ts", "c.ts"); hits != 3 || read["c.ts"] != "export const c = 3;\n" {
		test.Fatalf("the appended file was not served: %d hits, %q", hits, read["c.ts"])
	}

	// Rewriting a and b three times over leaves their first copies dead, more than what is live.
	for round := range 3 {
		fixture.write("a.ts", strings.Repeat(string(rune('d'+round)), 1000))
		fixture.write("b.ts", strings.Repeat(string(rune('d'+round)), 1001))
		fixture.run("a.ts", "b.ts", "c.ts")
	}
	if fixture.dataFile() == first {
		test.Fatal("a pack mostly dead was never compacted")
	}
	if read, hits, _, _ := fixture.run("a.ts", "b.ts", "c.ts"); hits != 3 || read["a.ts"] != strings.Repeat("f", 1000) {
		test.Fatalf("after compacting, served %d with a.ts %q...", hits, read["a.ts"][:5])
	}
}

// A served file is a string over the pack's mapping, not a copy, and it stays whole after the pack that served
// it is dropped and collected (#kdee854, 1b). The compiler keeps source text and substrings of it for the whole
// run, so a view that died with its pack would hand the checker freed memory.
func TestAServedFileIsAViewOfTheMappingThatOutlivesThePack(test *testing.T) {
	test.Parallel()
	fixture := newPackFixture(test)
	want := strings.Repeat("export const a = 1;\n", 4096)
	fixture.write("a.ts", want)
	fixture.run("a.ts")

	pack, openContentPackError := OpenContentPack(fixture.cache)
	if openContentPackError != nil {
		test.Fatal(openContentPackError)
	}
	served, ok := pack.wrap(osvfs.FS()).ReadFile(tspath.RootedFilePath(fixture.path("a.ts")))
	if !ok {
		test.Fatal("the pack served nothing, so nothing below is about a served file")
	}
	// Unsafe: the address is only compared, never followed.
	start := uintptr(unsafe.Pointer(unsafe.StringData(served)))
	// Unsafe: the address is only compared, never followed.
	mapped := uintptr(unsafe.Pointer(&pack.mapped[0]))
	if start < mapped || start+uintptr(len(served)) > mapped+uintptr(len(pack.mapped)) {
		test.Errorf("the served file lies outside the pack's mapping, so it was copied")
	}

	pack = nil
	runtime.GC()
	runtime.GC()
	if served != want {
		test.Fatalf(
			"a served file read after its pack was collected holds %d bytes that differ from the file's",
			len(served),
		)
	}
}
