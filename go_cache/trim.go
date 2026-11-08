package go_cache

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// markUsedLag is Go's lag in refreshing a used entry's time: mtimeInterval in cmd/go/internal/cache's
// cache.go:348 (one hour, Go 1.27). Go marks an entry used by touching its modification time, but only
// when that time is an hour old or more, so an entry a build looked up a moment ago can carry a time
// almost an hour old.
const markUsedLag = time.Hour

// inUseMargin is how long after its last use an entry is still taken to be in use by a build no lock
// keeps out: a build reads an entry some time after looking it up.
const inUseMargin = time.Hour

// MinimumSpare is the shortest spare any trim here takes, Go's lag in marking an entry used plus an hour
// of margin, the shape Go's own trim has (its five days plus the lag) and the spare Loom's MakeRoom takes.
// An entry last used at some moment carries a time no more than the lag older than that moment, so one
// whose time is older than MinimumSpare was last used more than the margin ago. It protects any build,
// inside a caller's lock or outside it, that used an entry within the last hour. A shorter spare reads as
// unused what Go has not had the chance to mark, and is refused.
const MinimumSpare = markUsedLag + inUseMargin

// Limit is the size a cache may reach before it is trimmed, the size a trim brings it down to, and how
// recently used an entry may be and still be spared. Trimming below the cap means a cache growing steadily
// is trimmed once in a while rather than on every look.
type Limit struct {
	Cap    int64
	Target int64
	// Spare is how recently used an entry may be and never be removed, MinimumSpare at least.
	Spare time.Duration
}

// Trimmed is what one trim found and removed.
type Trimmed struct {
	Directory string
	Before    int64
	After     int64
	Removed   int
	// InUse is the bytes of entries used within the limit's Spare window, which the trim did not remove.
	// When After is over the cap, this is why.
	InUse int64
}

// Excess is how many bytes a cache of total bytes should lose under a cap and a target: none at or under
// the cap, and above it, down to the target.
func Excess(total, capacity, target int64) int64 {
	if total <= capacity || total <= target {
		return 0
	}
	return total - max(target, 0)
}

// Candidates is the entries last used before now less spare, the ones a trim may remove, least recently
// used first, and of two used at the same time the one whose path sorts first, so a trim takes the same
// entries however the walk found them. A spare under MinimumSpare is refused.
func Candidates(entries []Entry, spare time.Duration, now time.Time) ([]Entry, error) {
	if spare < MinimumSpare {
		return nil, fmt.Errorf(
			"refusing to trim entries used within %s: Go refreshes a used entry's time only once every %s, so "+
				"under that an entry a build is reading reads as unused",
			spare,
			MinimumSpare,
		)
	}
	spareSince := now.Add(-spare)
	candidates := []Entry{}
	for _, entry := range entries {
		if !entry.Used.After(spareSince) {
			candidates = append(candidates, entry)
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		if !candidates[left].Used.Equal(candidates[right].Used) {
			return candidates[left].Used.Before(candidates[right].Used)
		}
		return candidates[left].Path < candidates[right].Path
	})
	return candidates, nil
}

// Choose is the candidates, in order, a removal of remove bytes takes when every one is still there to
// remove: the fewest from the front that hold remove bytes, or all of them when they hold less. It removes
// nothing, so a caller can say what a trim would do.
func Choose(candidates []Entry, remove int64) []Entry {
	var chosen int64
	for index, candidate := range candidates {
		if chosen >= remove {
			return candidates[:index]
		}
		chosen += candidate.Bytes
	}
	return candidates
}

// Remove removes one entry, an executable entry's files by name and then its emptied directory, and
// reports whether it did. It looks at the entry again first and leaves it when its time has moved since
// it was listed, since that is Go marking it used again, or when it is no longer the kind it was listed
// as. An entry already gone is not removed and is no error.
func Remove(entry Entry) (bool, error) {
	information, statError := os.Lstat(entry.Path)
	if gone(statError) {
		return false, nil
	}
	if statError != nil {
		return false, fmt.Errorf("trimming the Go build cache: %w", statError)
	}
	if !information.ModTime().Equal(entry.Used) {
		return false, nil
	}
	if entry.Executable != information.IsDir() || (!entry.Executable && !information.Mode().IsRegular()) {
		return false, nil
	}
	for _, name := range entry.Files {
		if removeError := os.Remove(filepath.Join(entry.Path, name)); removeError != nil && !gone(removeError) {
			return false, fmt.Errorf("trimming the Go build cache: %w", removeError)
		}
	}
	removeError := os.Remove(entry.Path)
	if gone(removeError) {
		return false, nil
	}
	if removeError != nil {
		return false, fmt.Errorf("trimming the Go build cache: %w", removeError)
	}
	return true, nil
}

// RemoveOldest removes candidates in order until remove bytes are gone, and returns how many entries and
// bytes it removed. A candidate Remove leaves, used again or gone already, is passed over for the next, so
// one entry a build touched costs the trim nothing. A removal that fails for any other reason stops it.
func RemoveOldest(candidates []Entry, remove int64) (int, int64, error) {
	removedEntries := 0
	var removedBytes int64
	for _, candidate := range candidates {
		if removedBytes >= remove {
			break
		}
		removed, removeError := Remove(candidate)
		if removeError != nil {
			return removedEntries, removedBytes, removeError
		}
		if removed {
			removedEntries++
			removedBytes += candidate.Bytes
		}
	}
	return removedEntries, removedBytes, nil
}

// Trim removes a Go build cache's least recently used entries once it is over limit.Cap, until it is at or
// under limit.Target or only entries used within limit.Spare are left.
func Trim(directory string, limit Limit, now time.Time) (Trimmed, error) {
	return trim(directory, limit, now, nil)
}

// trim is Trim, with listed, when set, run between the listing and the first removal, so a test can use or
// remove an entry in between.
func trim(directory string, limit Limit, now time.Time, listed func()) (Trimmed, error) {
	trimmed := Trimmed{Directory: directory}
	// Refused before the walk, so a trim that could never remove safely reads nothing either.
	if _, candidatesError := Candidates(nil, limit.Spare, now); candidatesError != nil {
		return trimmed, candidatesError
	}
	entries, listError := List(directory)
	if listError != nil {
		return trimmed, listError
	}
	candidates, candidatesError := Candidates(entries, limit.Spare, now)
	if candidatesError != nil {
		return trimmed, candidatesError
	}
	trimmed.Before = Total(entries)
	trimmed.After = trimmed.Before
	trimmed.InUse = trimmed.Before - Total(candidates)
	remove := Excess(trimmed.Before, limit.Cap, limit.Target)
	if remove == 0 {
		return trimmed, nil
	}
	if listed != nil {
		listed()
	}
	removedEntries, removedBytes, removeError := RemoveOldest(candidates, remove)
	trimmed.Removed = removedEntries
	trimmed.After -= removedBytes
	return trimmed, removeError
}
