package program

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

// The run cache: replay a whole run's output without building anything, when every input it
// depended on is provably unchanged.
//
// This is a different cache from the lint findings cache beside it, and the difference is the whole
// reason it exists. That cache was measured at about 9% and left unwired because it still walked the
// tree: it could only skip map lookups the one-traversal design had already made nearly free. This
// one skips the walk and the graph build as well, which is roughly 0.45s on the ahra tree before any rule
// runs. Its prize is the entire run, so the earlier verdict says nothing about it.
//
// # The rule it has to keep
//
// A stale run cache that replays a clean verdict over a tree that is no longer clean is the worst
// thing this tool could do, and it would look exactly like success. So the cache answers "hit" only
// when it can prove every input is unchanged, and any doubt at all is a miss: an unreadable
// manifest, a format version it does not recognize, a single entry that does not match. A miss costs
// one ordinary run. A false hit costs a lie.
//
// # The input it cannot see, and how it sees it anyway
//
// The program's file list only exists after the graph is built, which is the cost this cache exists
// to skip. So a full run records the list and the next run re-stats what was recorded. That is sound
// for a file edited or deleted. It is blind to a file that was added: a new source file under an include glob
// is an input the recorded list has never heard of, and re-statting the old list finds nothing wrong.
//
// Directories close that gap. Adding or removing an entry changes the mtime of the directory that
// holds it, so the manifest records every directory the program draws from and re-stats those too.
// Every directory, not the include roots: verified on macOS, a file added in a subdirectory changes
// that subdirectory's mtime and leaves its parent's alone, so watching only the roots would miss
// anything added one level down. On the ahra tree that is 1,725 directories beside 10,360 files, and
// statting all 12,085 costs about 13ms across eight workers.
//
// # What "unchanged" means for one entry
//
// Size and modification time, at nanosecond resolution. A file whose bytes changed and whose mtime
// did not is the case this cannot see, and it requires something deliberately resetting the mtime:
// `touch -r`, some archive extractors, a tool that preserves timestamps on copy. Content hashing every
// file would close it at a cost of reading 110 MB per run, which is the run this cache exists to
// avoid. The trade is stated here so nobody discovers it by surprise; Record also hashes each file's
// content so a later change of policy has the data to tighten this without a format change.
type RunCache struct {
	// Version is the manifest format. A manifest written by a different format is a miss, never a
	// best-effort read: the fields would be read from the wrong places and still parse.
	Version int

	// Key covers everything about the run that is not a file on disk: the flags, the binary's
	// identity, the working directory. Two runs that differ in any of those are different runs even
	// over identical files, and must not replay each other.
	Key string

	// KeyParts is Key's parts, so a run under another key can say which part moved. See RunCacheKeyParts.
	KeyParts RunCacheKeyParts

	// Inputs is every file and directory the run depended on, with the signature it had.
	Inputs []RunCacheInput

	// Output is exactly what the run printed to stdout, replayed byte for byte on a hit.
	Output []byte

	// Errors is what it printed to stderr. Kept apart rather than interleaved, so each stream replays
	// to the stream it was written to.
	Errors []byte

	// RecordedUnixNanoseconds is when the replayed run happened. A replay prints that run's report,
	// durations included, so it has to be able to say when those numbers are from.
	RecordedUnixNanoseconds int64

	// ExitCode is the process exit code to return on a hit. A cache that replayed the output and
	// exited zero on a failing tree would pass CI while printing a failure.
	ExitCode int

	// Summary is the run's summary as the command encodes it, opaque here. The footer a run ends with
	// carries that run's time, so it is never in Output; a replay renders its own from this, with the
	// replay's time, rather than reprinting a recorded one that would claim the recorded run's.
	Summary []byte
}

// runCacheVersion is bumped whenever the manifest's meaning changes, not only its shape.
//
// 2: Output no longer carries the lines that describe the recorded invocation (graph built in, types and
// lint durations, the phases and total lines). A version-1 manifest has them, and replaying one would
// print a graph build that did not happen.
//
// 3: Output carries a tag on lines a replay prints with where they came from, the fix line among them.
// A version-2 manifest has none, and would replay "fix: ..." as though the fix phase had just run.
//
// 4: Inputs carry their change time and inode. A version-3 input has neither, and checked against a
// stat that does, every one would read as changed.
//
// 5: A run ends with a footer that is not in Output, and Summary carries what a replay renders it from.
// A version-4 manifest has the old phase account in Output and no Summary, and would replay with no
// footer at all.
const runCacheVersion = 5

// ErrRunCacheMiss is the one answer a check gives when it cannot prove a hit. Callers treat every
// error from Check as a miss and run normally; this exists so a test can tell a clean miss
// from an unexpected failure.
var ErrRunCacheMiss = errors.New("run cache miss")

// RunCacheKey hashes what identifies a run apart from its files. It is RunCacheKeyPartsOf's key.
func RunCacheKey(arguments []string, workingDirectory string, facts ...string) (string, error) {
	parts, runCacheKeyPartsOfError := RunCacheKeyPartsOf(arguments, workingDirectory, facts...)
	if runCacheKeyPartsOfError != nil {
		return "", runCacheKeyPartsOfError
	}
	return parts.Key(), nil
}

// RunCacheKeyPartsOf hashes each part of a run's identity.
func RunCacheKeyPartsOf(arguments []string, workingDirectory string, facts ...string) (RunCacheKeyParts, error) {
	executable, executableError := os.Executable()
	if executableError != nil {
		return RunCacheKeyParts{}, fmt.Errorf("locating the running binary: %w", executableError)
	}
	// Resolved, as the callers resolve the directory, so a binary reached through a link keys as itself (#547dhjz).
	if resolved, resolveSymlinksError := filepath.EvalSymlinks(executable); resolveSymlinksError == nil {
		executable = resolved
	}
	information, statError := os.Stat(executable)
	if statError != nil {
		return RunCacheKeyParts{}, fmt.Errorf("reading the running binary: %w", statError)
	}
	part := func(values ...string) string {
		hash := sha256.New()
		for _, value := range values {
			hash.Write([]byte(value))
			hash.Write([]byte{0})
		}
		return fmt.Sprintf("%x", hash.Sum(nil))
	}
	return RunCacheKeyParts{
		Binary: part(fmt.Sprintf("version %d", runCacheVersion), executable, fmt.Sprintf("%d", information.Size()),
			fmt.Sprintf("%d", information.ModTime().UnixNano()), runtime.GOOS, runtime.GOARCH),
		Directory: part(workingDirectory),
		Arguments: part(append([]string{fmt.Sprintf("%d", len(arguments))}, arguments...)...),
		Facts:     part(append([]string{fmt.Sprintf("%d facts", len(facts))}, facts...)...),
	}, nil
}

// RecordRunCache captures a run that just finished.
//
// files is every path the run depended on that existed, and directories among them are fine: whether
// an entry is a file or a directory is read from the disk here, never inferred from which list it
// arrived in. An earlier version stamped every entry of files as a file, so a directory the compiler
// had probed was recorded as one and every check then reported it "changed between file and
// directory". The cache never hit, which is the most deceptive way for a cache to fail: it runs, it
// misses, and it looks merely cold. The probe that found it was the untouched-tree control.
//
// The directory holding each file is added here too, so a caller cannot forget them and quietly ship
// a cache blind to added files. extraDirectories adds more, and absent lists paths the run looked for
// and did not find, which must stay absent for a hit. probed lists present paths the run only asked
// whether they exist; a directory among them that holds no file the run read and is not among
// extraDirectories is recorded ExistenceOnly.
//
// readSince is when the run began reading its inputs, as the filesystem stamps time. An input changed at or
// after it may have been read before the change, so the stat taken here would sign the new bytes against a
// verdict computed from the old ones, and the next run would replay that verdict over a tree it does not
// describe: a file saved in an editor while cohere runs is exactly this. Such a run is not recorded. The zero
// time checks nothing.
func RecordRunCache(
	key string,
	files []string,
	extraDirectories []string,
	absent []string,
	probed []string,
	output []byte,
	exitCode int,
	readSince time.Time,
) (*RunCache, error) {
	return RecordRunCacheWithin(nil, key, files, extraDirectories, absent, probed, output, exitCode, readSince, nil)
}

// RecordRunCacheWithin is RecordRunCache recording the directory holding a file only when that directory is one of
// roots or lies inside one, for a run whose inputs name every path outside them whose presence decides its verdict,
// as the Go engine's record does: a staticcheck.conf or a .gitignore above the module, the toolchain's files, the
// module cache's, the git index in a worktree's git directory, each there or absent. Such a file is decided by its
// own signature; a file added beside it changes nothing, and its directory would only move with whatever else lives
// there: a home directory, a repository's top, a git directory that every git command in the worktree writes a lock
// file into. Recorded, it would refuse or miss nearly every run and nothing would say why (the same trap as
// #r9jevk9's parent of the project's own directory). Nil roots record every holding directory, as RecordRunCache
// does.
//
// Such a run's extraDirectories are listings it read, each deciding which files exist, so each is held to readSince
// as a file is, whether or not it holds a file the run read: a package directory created under one mid-run moved its
// listing after the run read it, and recorded with its new signature it would replay a verdict that never saw it.
//
// trackedIgnored keys each git index it names, by its path among files, on what the index decides for the run rather
// than on its bytes (RunCacheInput.TrackedIgnored, #m1daxxk). Each digest must be one taken before this call, of the
// index the run read: the record refuses a run whose index changed after readSince, so the bytes it records are the
// bytes the digest read, and a match on either is a match on what the run saw. Such an index's holding directory is
// not recorded for it: that is the git directory, which every git command writes a lock file into, and nothing added
// beside the index changes what the index decides. An index named but absent, or a directory, takes no key.
func RecordRunCacheWithin(
	roots []string,
	key string,
	files []string,
	extraDirectories []string,
	absent []string,
	probed []string,
	output []byte,
	exitCode int,
	readSince time.Time,
	trackedIgnored map[string]TrackedIgnoredKey,
) (*RunCache, error) {
	cache := &RunCache{Version: runCacheVersion, Key: key, Output: output, ExitCode: exitCode}

	// One entry per path. A path can arrive as a file read, a directory listed, and the parent of
	// something else, and statting it three times would spend the budget on duplicates.
	seen := map[string]bool{}
	var present []string
	sortedFiles := append([]string(nil), files...)
	sort.Strings(sortedFiles)
	for _, file := range sortedFiles {
		if !seen[file] {
			seen[file] = true
			present = append(present, file)
		}
	}
	inputs, signaturesOfError := signaturesOf(present)
	if signaturesOfError != nil {
		return nil, signaturesOfError
	}

	// The directory holding each file, so a file added beside one the run read is noticed. Only a file's:
	// a directory the run listed is an input by its own signature, whose change time and inode already catch
	// it being replaced, so its parent adds nothing. Recording the parent of the project's own directory made
	// every replay depend on whatever else lives beside the project, ~/Projects on a developer's machine and
	// the benchmark's work directory, where something is created all day (#r9jevk9).
	directorySet := map[string]struct{}{}
	rootSet := map[string]bool{}
	for _, root := range roots {
		rootSet[filepath.Clean(root)] = true
	}
	for index, input := range inputs {
		if input.Directory {
			continue
		}
		if trackedIgnoredKey, keyed := trackedIgnored[input.Path]; keyed {
			inputs[index].TrackedIgnored = &trackedIgnoredKey
			continue
		}
		if roots == nil || withinAny(filepath.Dir(input.Path), rootSet) {
			directorySet[filepath.Dir(input.Path)] = struct{}{}
		}
	}
	for _, directory := range extraDirectories {
		directorySet[directory] = struct{}{}
	}
	directories := make([]string, 0, len(directorySet))
	for directory := range directorySet {
		if !seen[directory] {
			seen[directory] = true
			directories = append(directories, directory)
		}
	}
	sort.Strings(directories)
	directoryInputs, signaturesOfError := signaturesOf(directories)
	if signaturesOfError != nil {
		return nil, signaturesOfError
	}
	cache.Inputs = append(inputs, directoryInputs...)

	// A directory only probed, holding nothing the run read and named by no caller, is kept for its existence.
	probedSet := make(map[string]struct{}, len(probed))
	for _, path := range probed {
		probedSet[path] = struct{}{}
	}
	for index, input := range cache.Inputs {
		_, onlyProbed := probedSet[input.Path]
		_, needed := directorySet[input.Path]
		if input.Directory && onlyProbed && !needed {
			cache.Inputs[index].ExistenceOnly = true
		}
	}
	if !readSince.IsZero() {
		// Files, and the directories holding them, where a file added mid-run beside the ones read would be
		// missed. A directory only probed on the way to a node_modules or a package.json is left out: such
		// ancestors sit above the project, other processes move them all the time (a temporary directory
		// never stops), and checking them would keep most runs from ever being recorded.
		holdsARead := map[string]bool{}
		for _, input := range cache.Inputs {
			if !input.Directory {
				holdsARead[filepath.Dir(input.Path)] = true
			}
		}
		listingsRead := map[string]bool{}
		if roots != nil {
			for _, directory := range extraDirectories {
				listingsRead[directory] = true
			}
		}
		for _, input := range cache.Inputs {
			if input.ExistenceOnly || input.Directory && !holdsARead[input.Path] && !listingsRead[input.Path] {
				continue
			}
			// At or after, not only after: a filesystem that stamps from a coarse clock (Linux) gives a change made
			// in the same tick as the clock the clock's own time, and that change is one the run may not have seen.
			// Its cost is a run whose input was written in the tick just before it began, which is not recorded.
			if max(input.ModifiedNanoseconds, input.ChangedNanoseconds) >= readSince.UnixNano() {
				return nil, fmt.Errorf(
					"%s changed after the run began reading, so what the run saw of it is not known",
					input.Path,
				)
			}
		}
	}

	for _, path := range absent {
		if seen[path] {
			// Probed absent once and found present another time is a contradiction within one run.
			// The present reading wins, since the run did read it.
			continue
		}
		seen[path] = true
		cache.Inputs = append(cache.Inputs, RunCacheInput{Path: path, Exists: false})
	}

	return cache, nil
}

// withinAny reports whether directory is one of roots or lies inside one. It walks up from directory, so a set of
// thousands of roots, every directory a Go run listed, costs each file a few lookups rather than one per root.
func withinAny(directory string, roots map[string]bool) bool {
	for current := filepath.Clean(directory); ; current = filepath.Dir(current) {
		if roots[current] {
			return true
		}
		if filepath.Dir(current) == current {
			return false
		}
	}
}

// signaturesOf stats every path in parallel, as Check does, each into its own slot, so the order and the
// first error are the ones a serial pass would give. About 17,000 stats on ahra: 64 to 79ms one at a time,
// under 20ms across workers (#a66sfmh), on the path between the report printing and the process exiting.
func signaturesOf(paths []string) ([]RunCacheInput, error) {
	inputs := make([]RunCacheInput, len(paths))
	failures := make([]error, len(paths))
	workers := min(runtime.NumCPU(), 8)
	var group sync.WaitGroup
	for worker := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := worker; index < len(paths); index += workers {
				inputs[index], failures[index] = signatureOf(paths[index])
			}
		}()
	}
	group.Wait()
	for _, failure := range failures {
		if failure != nil {
			return nil, failure
		}
	}
	return inputs, nil
}

// signatureOf stats one input. A path that vanished between the run and the record is an error rather
// than an absent entry: recording it as absent would make the next run's absence look like a match,
// and the run that just finished did read it.
func signatureOf(path string) (RunCacheInput, error) {
	information, statError := os.Stat(path)
	if statError != nil {
		return RunCacheInput{}, fmt.Errorf("recording %s: %w", path, statError)
	}
	changed, inode := changeTimeAndInode(path, information)
	return RunCacheInput{
		Path:                path,
		Directory:           information.IsDir(),
		Exists:              true,
		Size:                information.Size(),
		ModifiedNanoseconds: information.ModTime().UnixNano(),
		ChangedNanoseconds:  changed,
		Inode:               inode,
	}, nil
}

// Check reports whether a stored run can be replayed, returning nil only on a provable hit.
//
// It returns the stored run only when the key matches and every input still has the signature it
// was recorded with. The first mismatch is the answer; there is no partial hit, because a run's
// verdict is a property of all its inputs together.
func (cache *RunCache) Check(key string) error {
	return cache.CheckNoting(key, nil)
}

// CheckNoting is Check, noting in snapshot what it found at every path it statted, so the build and the content
// pack can use stats this run already took rather than take each again (#kdee854). A nil snapshot notes nothing.
func (cache *RunCache) CheckNoting(key string, snapshot *StatSnapshot) error {
	if cache == nil {
		return fmt.Errorf("%w: no cache", ErrRunCacheMiss)
	}
	if cache.Version != runCacheVersion {
		return fmt.Errorf("%w: format version %d, this build reads %d", ErrRunCacheMiss, cache.Version, runCacheVersion)
	}
	if cache.Key != key {
		return fmt.Errorf("%w: the binary, flags or directory changed", ErrRunCacheMiss)
	}
	if len(cache.Inputs) == 0 {
		// A manifest with no inputs would match any tree. A run always depends on something, so
		// an empty list is a manifest that was written wrong, not a run that read nothing.
		return fmt.Errorf("%w: the manifest records no inputs", ErrRunCacheMiss)
	}
	return cache.changedInput(snapshot)
}

// ChangedInput reports the first recorded input whose signature no longer matches the disk, nil when every
// one still does. It is Check without the key, so --cache-dump can say which input keeps a run from
// replaying rather than leaving a miss silent (#r9jevk9).
//
// The input named is the earliest recorded one that moved, whichever worker found it, so a run that misses
// says the same thing every time rather than the first thing a worker happened to reach (#547dhjz).
func (cache *RunCache) ChangedInput() error {
	return cache.changedInput(nil)
}

// changedInput is ChangedInput, noting what each stat found in snapshot when it is not nil.
func (cache *RunCache) changedInput(snapshot *StatSnapshot) error {
	workers := min(runtime.NumCPU(), 8)
	var mutex sync.Mutex
	var mismatch error
	earliest := len(cache.Inputs)
	var waitGroup sync.WaitGroup
	next := make(chan int, 256)

	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for index := range next {
				if stillMatchesError := cache.Inputs[index].stillMatches(snapshot); stillMatchesError != nil {
					mutex.Lock()
					if index < earliest {
						earliest, mismatch = index, stillMatchesError
					}
					mutex.Unlock()
				}
			}
		}()
	}
	for index := range cache.Inputs {
		next <- index
	}
	close(next)
	waitGroup.Wait()

	return mismatch
}
