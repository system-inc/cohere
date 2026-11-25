package go_cache

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// kibibyte is the unit the fixture's sizes are written in.
const kibibyte = 1024

// fixtureNow is the moment every fixture's ages are counted back from.
var fixtureNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

// fixtureEntry is one entry of the fixture cache: its subdirectory, its kind's suffix, its size and how long
// ago it was last used.
type fixtureEntry struct {
	subdirectory string
	suffix       string
	kibibytes    int
	age          time.Duration
	executable   bool
}

// fixtureEntries is the fixture cache, one entry a letter, oldest first, each named as Go names it by its
// subdirectory: C is an executable entry, and the rest are files. 100 KiB in all.
var fixtureEntries = map[string]fixtureEntry{
	"A": {subdirectory: "00", suffix: "-a", kibibytes: 10, age: 7 * 24 * time.Hour},
	"B": {subdirectory: "01", suffix: "-d", kibibytes: 20, age: 3 * 24 * time.Hour},
	"C": {subdirectory: "02", suffix: "-d", kibibytes: 10, age: 30 * time.Hour, executable: true},
	"D": {subdirectory: "03", suffix: "-a", kibibytes: 10, age: 25 * time.Hour},
	"E": {subdirectory: "04", suffix: "-d", kibibytes: 20, age: 3 * time.Hour},
	"F": {subdirectory: "05", suffix: "-a", kibibytes: 10, age: 80 * time.Minute},
	"G": {subdirectory: "06", suffix: "-d", kibibytes: 10, age: 45 * time.Minute},
	"H": {subdirectory: "07", suffix: "-a", kibibytes: 10, age: 5 * time.Minute},
}

// fixture is a cache written in Go's layout: the entries, by letter, and the paths no trim may remove.
type fixture struct {
	directory string
	entries   map[string]string
	kept      []string
}

// newFixture writes the fixture cache: fixtureEntries, Go's README and bookkeeping, a fuzz corpus file, and
// K, a -d name that is a link to a file outside the cache, ten days old.
func newFixture(test *testing.T) fixture {
	test.Helper()
	written := fixture{directory: newCache(test), entries: map[string]string{}}
	for letter, entry := range fixtureEntries {
		path := entryPath(written.directory, entry.subdirectory, entry.subdirectory[1:], entry.suffix)
		used := fixtureNow.Add(-entry.age)
		if entry.executable {
			put(test, path, "tool", entry.kibibytes*kibibyte, used)
			setTime(test, path, used)
		} else {
			put(test, filepath.Dir(path), filepath.Base(path), entry.kibibytes*kibibyte, used)
		}
		written.entries[letter] = path
	}
	old := fixtureNow.Add(-10 * 24 * time.Hour)
	outside := put(test, test.TempDir(), "target", 10*kibibyte, old)
	link := entryPath(written.directory, "09", "f", "-d")
	if createDirectoryError := os.MkdirAll(filepath.Dir(link), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if linkError := os.Symlink(outside, link); linkError != nil {
		test.Fatal(linkError)
	}
	written.kept = []string{
		filepath.Join(written.directory, "README"),
		put(test, written.directory, "trim.txt", 16, old),
		put(test, written.directory, "testexpire.txt", 16, old),
		put(test, written.directory, "fuzz/p/FuzzX/0123456789abcdef", 50*kibibyte, old),
		link,
		outside,
	}
	return written
}

// entryPath is the path in the cache at directory of the entry Go would name with suffix in subdirectory,
// its hash being the subdirectory's digits and then digit repeated.
func entryPath(directory, subdirectory, digit, suffix string) string {
	return filepath.Join(directory, subdirectory, subdirectory+strings.Repeat(digit, hashDigits-2)+suffix)
}

// newCache makes a directory in Go's cache layout, README included.
func newCache(test *testing.T) string {
	test.Helper()
	directory := test.TempDir()
	if writeError := os.WriteFile(
		filepath.Join(directory, "README"),
		[]byte(readmeMarker+"\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	return directory
}

// put writes a file of the given size into directory, last used at the given time, and returns its path.
func put(test *testing.T, directory, name string, bytes int, used time.Time) string {
	test.Helper()
	path := filepath.Join(directory, name)
	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(path, []byte(strings.Repeat("x", bytes)), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	setTime(test, path, used)
	return path
}

// setTime sets a path's access and modification times, the latter being what Go reads as last used.
func setTime(test *testing.T, path string, used time.Time) {
	test.Helper()
	if changeTimesError := os.Chtimes(path, used, used); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
}

func exists(path string) bool {
	_, statError := os.Lstat(path)
	return statError == nil
}

// gone is the fixture's letters whose entries are no longer on disk, in order.
func (written fixture) gone() string {
	letters := []string{}
	for letter, path := range written.entries {
		if !exists(path) {
			letters = append(letters, letter)
		}
	}
	sort.Strings(letters)
	return strings.Join(letters, "")
}

// letters is the fixture's letters for entries, in their order.
func (written fixture) letters(entries []Entry) string {
	byPath := map[string]string{}
	for letter, path := range written.entries {
		byPath[path] = letter
	}
	letters := ""
	for _, entry := range entries {
		letter, known := byPath[entry.Path]
		if !known {
			letter = "?"
		}
		letters += letter
	}
	return letters
}

// checkKept fails the test for any path no trim may remove that is gone.
func (written fixture) checkKept(test *testing.T) {
	test.Helper()
	for _, path := range written.kept {
		if !exists(path) {
			test.Errorf("%s was removed", path)
		}
	}
}

// The listing holds every entry Go writes, executable C as one entry of its directory's files, and leaves out
// K's link, Go's bookkeeping and the fuzz corpus.
func TestTheListingHoldsEveryEntryGoWrites(test *testing.T) {
	test.Parallel()
	written := newFixture(test)
	entries, listError := List(written.directory)
	if listError != nil {
		test.Fatal(listError)
	}
	if letters := written.letters(entries); letters != "ABCDEFGH" {
		test.Errorf("listed %s, want ABCDEFGH", letters)
	}
	if total := Total(entries); total != 100*kibibyte {
		test.Errorf("the entries hold %d bytes, want %d", total, 100*kibibyte)
	}
	for _, entry := range entries {
		if entry.Path != written.entries["C"] {
			continue
		}
		if !entry.Executable || strings.Join(entry.Files, ",") != "tool" || entry.Bytes != 10*kibibyte {
			test.Errorf("the executable entry listed as %+v, want its one file, tool, of 10 KiB", entry)
		}
	}
}

// Each policy removes exactly its entries, oldest first: none under the cap, down to the target over it, and
// to nothing with each spare window showing what it keeps. Choose, which removes nothing, names the same set.
func TestEachPolicyRemovesExactlyItsEntries(test *testing.T) {
	test.Parallel()
	for _, policy := range []struct {
		name    string
		limit   Limit
		removed string
	}{
		{name: "under the cap", limit: Limit{Cap: 100 * kibibyte, Target: 60 * kibibyte, Spare: MinimumSpare}},
		{
			name:    "over the cap, the minimum spare",
			limit:   Limit{Cap: 80 * kibibyte, Target: 60 * kibibyte, Spare: MinimumSpare},
			removed: "ABC",
		},
		{
			name:    "over the cap, Loom's room spare of two hours",
			limit:   Limit{Cap: 80 * kibibyte, Target: 60 * kibibyte, Spare: 2 * time.Hour},
			removed: "ABC",
		},
		{
			name:    "over the cap, Loom's day",
			limit:   Limit{Cap: 80 * kibibyte, Target: 60 * kibibyte, Spare: 24 * time.Hour},
			removed: "ABC",
		},
		{name: "to nothing, the minimum spare", limit: Limit{Spare: MinimumSpare}, removed: "ABCDE"},
		{name: "to nothing, three hours", limit: Limit{Spare: 3 * time.Hour}, removed: "ABCDE"},
		{name: "to nothing, four hours", limit: Limit{Spare: 4 * time.Hour}, removed: "ABCD"},
		{name: "to nothing, a day", limit: Limit{Spare: 24 * time.Hour}, removed: "ABCD"},
		{name: "to nothing, Go's five days and an hour", limit: Limit{Spare: 5*24*time.Hour + time.Hour}, removed: "A"},
	} {
		test.Run(policy.name, func(test *testing.T) {
			test.Parallel()
			written := newFixture(test)
			entries, listError := List(written.directory)
			if listError != nil {
				test.Fatal(listError)
			}
			candidates, candidatesError := Candidates(entries, policy.limit.Spare, fixtureNow)
			if candidatesError != nil {
				test.Fatal(candidatesError)
			}
			remove := Excess(Total(entries), policy.limit.Cap, policy.limit.Target)
			if chosen := written.letters(Choose(candidates, remove)); chosen != policy.removed {
				test.Errorf("Choose named %q, want %q", chosen, policy.removed)
			}

			trimmed, trimError := Trim(written.directory, policy.limit, fixtureNow)
			if trimError != nil {
				test.Fatal(trimError)
			}
			if gone := written.gone(); gone != policy.removed {
				test.Errorf("the trim removed %q, want %q", gone, policy.removed)
			}
			var removedBytes int64
			for _, letter := range policy.removed {
				removedBytes += int64(fixtureEntries[string(letter)].kibibytes * kibibyte)
			}
			if trimmed.Before != 100*kibibyte || trimmed.After != trimmed.Before-removedBytes ||
				trimmed.Removed != len(policy.removed) {
				test.Errorf("trimmed %+v, want 102400 bytes before, %d removed in %d entries",
					trimmed, removedBytes, len(policy.removed))
			}
			written.checkKept(test)
		})
	}
}

// The floor is two hours, Go's one-hour lag in marking an entry used and an hour of margin, and a spare under
// it, the lag alone included, is refused before anything is read or removed, however far over its cap the
// cache is.
func TestASpareUnderTheMinimumIsRefused(test *testing.T) {
	test.Parallel()
	if MinimumSpare != 2*time.Hour {
		test.Errorf(
			"MinimumSpare is %s, want 2h: Go's lag of an hour (mtimeInterval) and an hour of margin",
			MinimumSpare,
		)
	}
	for _, spare := range []time.Duration{
		0,
		10 * time.Minute,
		time.Hour,
		90 * time.Minute,
		MinimumSpare - time.Nanosecond,
	} {
		written := newFixture(test)
		if _, trimError := Trim(
			written.directory,
			Limit{Spare: spare},
			fixtureNow,
		); trimError == nil || !strings.Contains(trimError.Error(), "refusing to trim entries used within") {
			test.Errorf("a trim sparing %s was not refused: %v", spare, trimError)
		}
		if gone := written.gone(); gone != "" {
			test.Errorf("a refused trim sparing %s removed %s", spare, gone)
		}
		if _, candidatesError := Candidates(nil, spare, fixtureNow); candidatesError == nil {
			test.Errorf("Candidates took a spare of %s", spare)
		}
	}
}

// An entry Go marks used between the listing and its removal is in use again, so it survives, and the trim
// takes the next oldest in its place.
func TestAnEntryUsedAgainAfterTheListingSurvives(test *testing.T) {
	test.Parallel()
	written := newFixture(test)
	trimmed, trimError := trim(
		written.directory,
		Limit{Cap: 80 * kibibyte, Target: 60 * kibibyte, Spare: MinimumSpare},
		fixtureNow,
		func() { setTime(test, written.entries["A"], fixtureNow) },
	)
	if trimError != nil {
		test.Fatal(trimError)
	}
	if gone := written.gone(); gone != "BCD" {
		test.Errorf("the trim removed %q, want BCD with A, used again, kept", gone)
	}
	if trimmed.Removed != 3 || trimmed.After != 60*kibibyte {
		test.Errorf("trimmed %+v, want 3 entries removed down to 61440 bytes", trimmed)
	}
	written.checkKept(test)
}

// A file another remover takes between the listing and its removal is no error, and the trim goes on to the
// rest (Adamic's case: Go's own trim, or another, removing beside this one).
func TestAnEntryGoneBeforeItsRemovalIsNoError(test *testing.T) {
	test.Parallel()
	written := newFixture(test)
	trimmed, trimError := trim(
		written.directory,
		Limit{Cap: 80 * kibibyte, Target: 60 * kibibyte, Spare: MinimumSpare},
		fixtureNow,
		func() {
			if removeError := os.Remove(written.entries["A"]); removeError != nil {
				test.Fatal(removeError)
			}
		},
	)
	if trimError != nil {
		test.Fatalf("a trim that found an entry gone failed: %v", trimError)
	}
	if gone := written.gone(); gone != "ABCD" {
		test.Errorf("after the trim %q are gone, want ABCD: A by another hand, then B, C and D", gone)
	}
	if trimmed.Removed != 3 {
		test.Errorf("trimmed %+v, want the 3 entries it removed itself", trimmed)
	}
	written.checkKept(test)
}

// A file removed from under the walk, the one being visited or one further on, is left out of the listing and
// is no error, and what was listed still trims.
func TestAnEntryGoneDuringTheWalkIsLeftOut(test *testing.T) {
	test.Parallel()
	written := newFixture(test)
	entries, listError := list(written.directory, func(path string) {
		removed := ""
		switch path {
		case written.entries["A"]:
			removed = written.entries["D"]
		case written.entries["B"]:
			removed = written.entries["B"]
		}
		if removed == "" {
			return
		}
		if removeError := os.Remove(removed); removeError != nil {
			test.Fatal(removeError)
		}
	})
	if listError != nil {
		test.Fatalf("a walk that met a vanished entry failed: %v", listError)
	}
	if letters := written.letters(entries); letters != "ACEFGH" {
		test.Errorf("listed %s, want ACEFGH, B and D gone", letters)
	}
	candidates, candidatesError := Candidates(entries, MinimumSpare, fixtureNow)
	if candidatesError != nil {
		test.Fatal(candidatesError)
	}
	removedEntries, _, removeError := RemoveOldest(candidates, 20*kibibyte)
	if removeError != nil || removedEntries != 2 {
		test.Fatalf("removed %d entries (%v), want A and C", removedEntries, removeError)
	}
	if gone := written.gone(); gone != "ABCD" {
		test.Errorf("after the trim %q are gone, want ABCD", gone)
	}
}

// An executable entry is a directory holding the binary. It is counted whole and removed file by file, and
// one holding anything but plain files is left alone.
func TestAnExecutableEntryIsCountedAndRemoved(test *testing.T) {
	test.Parallel()
	directory := newCache(test)
	old := fixtureNow.Add(-48 * time.Hour)
	executable := entryPath(directory, "00", "a", "-d")
	put(test, executable, "cohere", 2000, old)
	setTime(test, executable, old)
	nested := put(test, filepath.Join(entryPath(directory, "01", "b", "-d"), "inner"), "file", 2000, old)
	setTime(test, filepath.Dir(filepath.Dir(nested)), old)

	trimmed, trimError := Trim(directory, Limit{Cap: 1000, Target: 0, Spare: MinimumSpare}, fixtureNow)
	if trimError != nil {
		test.Fatal(trimError)
	}
	if trimmed.Removed != 1 || trimmed.Before != 2000 || trimmed.After != 0 {
		test.Fatalf("trimmed %+v, want the one executable entry of 2000 bytes", trimmed)
	}
	if exists(executable) {
		test.Error("the executable entry survived")
	}
	if !exists(nested) {
		test.Error("an entry holding a directory was removed")
	}
}

// A directory that does not say it is a Go build cache is never listed or trimmed, however large.
func TestADirectoryWithoutGosReadmeIsRefused(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	file := put(test, filepath.Join(directory, "ab"), "0000ab-d", 4096, fixtureNow.Add(-48*time.Hour))
	if _, trimError := Trim(
		directory,
		Limit{Cap: 1, Target: 0, Spare: MinimumSpare},
		fixtureNow,
	); trimError == nil || !strings.Contains(trimError.Error(), "refusing to trim") {
		test.Fatalf("a directory with no README was trimmed: %v", trimError)
	}
	if !exists(file) {
		test.Fatal("a refused trim removed a file")
	}
}

// putOutputs writes an old action file and an old executable output into directory, named as Go names entries
// of subdirectory, and returns the paths of the action file and the executable's binary.
func putOutputs(test *testing.T, directory, subdirectory string, used time.Time) []string {
	test.Helper()
	action := put(test, directory, filepath.Base(entryPath(directory, subdirectory, "a", "-a")), 4096, used)
	executable := filepath.Join(directory, filepath.Base(entryPath(directory, subdirectory, "b", "-d")))
	binary := put(test, executable, "tool", 4096, used)
	setTime(test, executable, used)
	return []string{action, binary}
}

// link makes path a symbolic link to target.
func link(test *testing.T, target, path string) {
	test.Helper()
	if linkError := os.Symlink(target, path); linkError != nil {
		test.Fatal(linkError)
	}
}

// A subdirectory that is a link is never read, wherever it points: one planted in place of 0a pointing outside
// the cache, and one in place of 0b pointing to a directory inside it. The old files and the executable output
// each target holds are never listed and survive a trim to nothing, while the real entry beside them goes
// (#7jx42ed).
func TestALinkedSubdirectoryIsNeverListedOrTrimmed(test *testing.T) {
	test.Parallel()
	directory := newCache(test)
	old := fixtureNow.Add(-10 * 24 * time.Hour)
	genuine := entryPath(directory, "00", "0", "-a")
	put(test, filepath.Dir(genuine), filepath.Base(genuine), 4096, old)
	outside := test.TempDir()
	kept := putOutputs(test, outside, "0a", old)
	link(test, outside, filepath.Join(directory, "0a"))
	kept = append(kept, putOutputs(test, filepath.Join(directory, "elsewhere"), "0b", old)...)
	link(test, "elsewhere", filepath.Join(directory, "0b"))

	entries, listError := List(directory)
	if listError != nil {
		test.Fatal(listError)
	}
	if len(entries) != 1 || entries[0].Path != genuine {
		test.Errorf("listed %+v, want only %s", entries, genuine)
	}
	trimmed, trimError := Trim(directory, Limit{Spare: MinimumSpare}, fixtureNow)
	if trimError != nil {
		test.Fatal(trimError)
	}
	if trimmed.Removed != 1 || exists(genuine) {
		test.Errorf("trimmed %+v, want the one real entry removed", trimmed)
	}
	for _, path := range kept {
		if !exists(path) {
			test.Errorf("%s, behind a linked subdirectory, was removed", path)
		}
	}
}

// A link swapped in for a real subdirectory between the listing and the removal leaves the entries listed
// through it where they are, with no error, and removes nothing it points to: 00 becomes a link outside the
// cache and 02 one to a directory inside it, each target holding A's and C's names, with their times, so the
// recheck before a remove would pass. The rest of the trim goes on.
func TestALinkSwappedInAfterTheListingRemovesNothingOutside(test *testing.T) {
	test.Parallel()
	written := newFixture(test)
	outside := test.TempDir()
	inside := filepath.Join(written.directory, "elsewhere")
	moved := test.TempDir()
	targets := []string{}
	swap := func() {
		for _, swapped := range []struct {
			letter string
			target string
			link   string
		}{
			{letter: "A", target: outside, link: outside},
			{letter: "C", target: inside, link: "elsewhere"},
		} {
			original := written.entries[swapped.letter]
			used := fixtureNow.Add(-fixtureEntries[swapped.letter].age)
			if fixtureEntries[swapped.letter].executable {
				executable := filepath.Join(swapped.target, filepath.Base(original))
				targets = append(targets, put(test, executable, "tool", 4096, used))
				setTime(test, executable, used)
			} else {
				targets = append(targets, put(test, swapped.target, filepath.Base(original), 4096, used))
			}
			subdirectory := filepath.Dir(original)
			if renameError := os.Rename(
				subdirectory,
				filepath.Join(moved, filepath.Base(subdirectory)),
			); renameError != nil {
				test.Fatal(renameError)
			}
			link(test, swapped.link, subdirectory)
		}
	}
	trimmed, trimError := trim(written.directory, Limit{Spare: MinimumSpare}, fixtureNow, swap)
	if trimError != nil {
		test.Fatalf("a trim that met a swapped link failed: %v", trimError)
	}
	if len(targets) != 2 {
		test.Fatalf("the swap planted %d targets, want 2", len(targets))
	}
	for _, path := range targets {
		if !exists(path) {
			test.Errorf("%s, reached through a link swapped in after the listing, was removed", path)
		}
	}
	if trimmed.Removed != 3 || written.gone() != "BDE" {
		test.Errorf(
			"trimmed %+v and removed %q, want B, D and E, with A and C behind their links",
			trimmed,
			written.gone(),
		)
	}
	written.checkKept(test)
}

// Only a name Go gives an entry is listed or removed: its subdirectory's digits leading 64 lowercase hex, then
// -a or -d. A name that is not hex, one a digit short, one in uppercase, one under the wrong subdirectory and
// one with another suffix all survive a trim to nothing beside the one Go would write, and Remove refuses an
// entry named so.
func TestOnlyNamesGoGivesAnEntryAreListed(test *testing.T) {
	test.Parallel()
	directory := newCache(test)
	old := fixtureNow.Add(-10 * 24 * time.Hour)
	subdirectory := filepath.Join(directory, "0c")
	genuine := put(test, subdirectory, filepath.Base(entryPath(directory, "0c", "0", "-a")), 4096, old)
	kept := []string{
		put(test, subdirectory, "x-a", 4096, old),
		put(test, subdirectory, "0c"+strings.Repeat("c", hashDigits-3)+"-a", 4096, old),
		put(test, subdirectory, "0c"+strings.Repeat("C", hashDigits-2)+"-a", 4096, old),
		put(test, subdirectory, "0d"+strings.Repeat("d", hashDigits-2)+"-a", 4096, old),
		put(test, subdirectory, "0c"+strings.Repeat("e", hashDigits-2)+"-x", 4096, old),
	}

	entries, listError := List(directory)
	if listError != nil {
		test.Fatal(listError)
	}
	if len(entries) != 1 || entries[0].Path != genuine {
		test.Errorf("listed %+v, want only %s", entries, genuine)
	}
	if _, trimError := Trim(directory, Limit{Spare: MinimumSpare}, fixtureNow); trimError != nil {
		test.Fatal(trimError)
	}
	if exists(genuine) {
		test.Error("the entry Go would write survived a trim to nothing")
	}
	for _, path := range kept {
		if !exists(path) {
			test.Errorf("%s, no name Go gives an entry, was removed", filepath.Base(path))
		}
		removed, removeError := Remove(Entry{Path: path, Bytes: 4096, Used: old})
		if removed || removeError == nil || !strings.Contains(removeError.Error(), "refusing to remove") {
			test.Errorf("Remove of %s was not refused: %t, %v", filepath.Base(path), removed, removeError)
		}
		if !exists(path) {
			test.Errorf("%s, no name Go gives an entry, was removed by Remove", filepath.Base(path))
		}
	}
}

// A link swapped in for a real subdirectory after its Lstat and before its open is caught by the identity
// check alone, since the open follows a link that stays inside the cache: 00 becomes a link to elsewhere, a
// directory in the cache holding A's name with A's time and size. Swapped during the listing, nothing in
// elsewhere is listed. Swapped during the removal, after a listing of the real 00, nothing in elsewhere is
// removed. Neither stops the trim, and B, C, D and E go.
// Not parallel: afterLstatForTest is one hook for the package, and a top-level test that is not parallel runs
// while no parallel one does.
func TestALinkSwappedInBetweenTheCheckAndTheOpenIsRefused(test *testing.T) {
	for _, phase := range []struct {
		name   string
		listed bool
		before int64
	}{
		{name: "during the listing", before: 90 * kibibyte},
		{name: "during the removal", listed: true, before: 100 * kibibyte},
	} {
		// Not parallel: each phase sets afterLstatForTest, the one hook for the package.
		test.Run(phase.name, func(test *testing.T) {
			written := newFixture(test)
			original := written.entries["A"]
			target := put(
				test,
				filepath.Join(written.directory, "elsewhere"),
				filepath.Base(original),
				fixtureEntries["A"].kibibytes*kibibyte,
				fixtureNow.Add(-fixtureEntries["A"].age),
			)
			armed, swapped := !phase.listed, false
			afterLstatForTest = func(name string) {
				if !armed || swapped || name != "00" {
					return
				}
				swapped = true
				if renameError := os.Rename(
					filepath.Dir(original),
					filepath.Join(test.TempDir(), "00"),
				); renameError != nil {
					test.Fatal(renameError)
				}
				link(test, "elsewhere", filepath.Dir(original))
			}
			defer func() { afterLstatForTest = nil }()

			trimmed, trimError := trim(
				written.directory,
				Limit{Spare: MinimumSpare},
				fixtureNow,
				func() { armed = true },
			)
			if trimError != nil {
				test.Fatalf("a trim that met a link swapped in after its check failed: %v", trimError)
			}
			if !swapped {
				test.Fatal("the link was never swapped in, so the test shows nothing")
			}
			if trimmed.Before != phase.before {
				test.Errorf(
					"the trim listed %d bytes, want %d, with nothing in elsewhere",
					trimmed.Before,
					phase.before,
				)
			}
			if !exists(target) {
				test.Errorf("%s, reached through a link swapped in after its check, was removed", target)
			}
			if gone := written.gone(); gone != "BCDE" || trimmed.Removed != 4 {
				test.Errorf("trimmed %+v and removed %q, want B, C, D and E, with A behind its link", trimmed, gone)
			}
			written.checkKept(test)
		})
	}
}
