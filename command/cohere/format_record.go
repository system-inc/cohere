package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/formatfiles"
	"github.com/system-inc/cohere/internal/types/program"
)

// The format record: which bytes cohere has already seen formatted, so a run formats what changed
// since cohere last looked rather than what git says changed since the last commit.
//
// The scope used to be git's answer: working tree, staged and untracked, asked of every submodule in
// turn. That was a dozen subprocesses on ahra and the largest single cost outside the phases, and it
// answered a neighbouring question. A file committed unformatted was never in it, so it was never
// formatted and never reported, and a file edited and committed between two runs was never formatted
// either. The record answers the question the format phase actually has: are these bytes already known
// to be formatted?
//
// # What an entry proves
//
// An entry is the hash of a text the formatter returned unchanged, which is what formatTransform hands
// back: it formats until a pass changes nothing. So a file whose bytes hash to an entry is a fixed point
// of the formatter under the options it was recorded with, and formatting it again would change
// nothing. That holds whether or not the text was ever written. A `--no-fix` run over an unformatted
// file records the formatted text, the disk still holds the old bytes, the hashes differ, and the file
// stays in scope and keeps being reported. Nothing about the write has to be trusted.
//
// # What makes the record say nothing
//
// The record is keyed by cohere's own identity (the binary, the project root, the platform), so a
// rebuilt cohere, whose printers may print differently, starts with an empty record and formats every
// file once. Each entry carries the options its file formats with, so a config edit sends exactly the
// files it reaches back through the formatter. Any doubt, an unreadable record, a version it does not
// know, is an empty record: the cost is one run that formats everything, never a file skipped on a
// guess.
//
// # Reading it cheaply
//
// An entry also holds the size and modification time its file had when its bytes were last read and
// matched, so an unchanged file is a stat rather than a read. That is the same trade the run cache
// makes, and it is stated in the same words there (see program.RunCache): a file whose bytes change
// while its size and nanosecond modification time do not is invisible to it.

// formatRecordVersion is bumped whenever an entry's meaning changes.
const formatRecordVersion = 1

type formatRecord struct {
	path string

	// identity is cohere's own, empty when it could not be determined, in which case nothing is read or
	// written and every file is in scope.
	identity string

	// absent says why no earlier check is on record, and is empty when one was read.
	absent string

	mutex   sync.Mutex
	entries map[string]formatRecordEntry
}

type formatRecordEntry struct {
	// Sum is the SHA-256 of a text the formatter left unchanged.
	Sum string `json:"sum"`

	// Options is the fingerprint of the options the text was formatted with.
	Options string `json:"options"`

	// Size and ModifiedNanoseconds are the file's signature when its bytes last matched Sum. Zero until
	// a run has read them.
	Size                int64 `json:"size,omitempty"`
	ModifiedNanoseconds int64 `json:"modifiedNanoseconds,omitempty"`
}

type formatRecordFile struct {
	Version  int                          `json:"version"`
	Identity string                       `json:"identity"`
	Entries  map[string]formatRecordEntry `json:"entries"`
}

// loadFormatRecord reads the record for a project root, from the user cache. It never fails: a record
// that cannot be read is an empty one, and absent says why.
func loadFormatRecord(root string) *formatRecord {
	return loadFormatRecordAt(cacheFilePath("format", root), root)
}

// loadFormatRecordAt is loadFormatRecord with the file named, so a test can keep it out of the user
// cache.
func loadFormatRecordAt(path string, root string) *formatRecord {
	record := &formatRecord{path: path, entries: map[string]formatRecordEntry{}}
	identity, err := program.RunCacheKey(nil, root, "format-record")
	if err != nil {
		record.absent = fmt.Sprintf("cohere could not identify its own binary (%v)", firstLine(err.Error()))
		return record
	}
	record.identity = identity

	contents, err := os.ReadFile(record.path)
	if errors.Is(err, os.ErrNotExist) {
		record.absent = "no earlier check is on record"
		return record
	}
	if err != nil {
		record.absent = fmt.Sprintf("the record could not be read (%v)", firstLine(err.Error()))
		return record
	}
	var stored formatRecordFile
	if err := json.Unmarshal(contents, &stored); err != nil {
		record.absent = "the record could not be read"
		return record
	}
	if stored.Version != formatRecordVersion || stored.Identity != identity {
		record.absent = "cohere changed since the last check, so its record says nothing about this one"
		return record
	}
	if stored.Entries != nil {
		record.entries = stored.Entries
	}
	return record
}

// unformatted returns the files whose current bytes are not on record as formatted under the options
// they format with now, sorted.
func (record *formatRecord) unformatted(files []string, optionsFor func(fileName string) (string, error)) []string {
	confirmed := make([]bool, len(files))
	if record.identity != "" {
		next := make(chan int, 256)
		var workers sync.WaitGroup
		for range min(runtime.NumCPU(), 8) {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for index := range next {
					confirmed[index] = record.formatted(files[index], optionsFor)
				}
			}()
		}
		for index := range files {
			next <- index
		}
		close(next)
		workers.Wait()
	}

	unformatted := []string{}
	for index, fileName := range files {
		if !confirmed[index] {
			unformatted = append(unformatted, fileName)
		}
	}
	sort.Strings(unformatted)
	return unformatted
}

// formatted reports whether a file's bytes are on record as formatted under its current options.
func (record *formatRecord) formatted(fileName string, optionsFor func(fileName string) (string, error)) bool {
	record.mutex.Lock()
	entry, present := record.entries[fileName]
	record.mutex.Unlock()
	if !present {
		return false
	}
	options, err := optionsFor(fileName)
	if err != nil || options != entry.Options {
		return false
	}

	// Stat before reading. A write after the stat moves the modification time past the one about to be
	// recorded, so the next run sees a mismatch and reads again; the other order could record a
	// signature taken after bytes it never read.
	information, err := os.Stat(fileName)
	if err != nil || !information.Mode().IsRegular() {
		return false
	}
	if entry.ModifiedNanoseconds != 0 &&
		information.Size() == entry.Size && information.ModTime().UnixNano() == entry.ModifiedNanoseconds {
		return true
	}

	contents, err := os.ReadFile(fileName)
	if err != nil || formatRecordSum(contents) != entry.Sum {
		return false
	}

	// A file written within the last second is not given a signature, so a second write inside the
	// same tick of a coarse clock cannot hide behind it. It is read again next run instead.
	if time.Since(information.ModTime()) > time.Second {
		entry.Size = information.Size()
		entry.ModifiedNanoseconds = information.ModTime().UnixNano()
		record.mutex.Lock()
		record.entries[fileName] = entry
		record.mutex.Unlock()
	}
	return true
}

// observe wraps a format transform so every text it returns is recorded as formatted. A nil record or
// transform is handed back unchanged.
func (record *formatRecord) observe(inner edit.Transform, optionsFor func(fileName string) (string, error)) edit.Transform {
	if record == nil || inner == nil || record.identity == "" {
		return inner
	}
	return func(fileName string, text string) (string, error) {
		formatted, err := inner(fileName, text)
		if err != nil {
			return formatted, err
		}
		if options, optionsError := optionsFor(fileName); optionsError == nil {
			record.mutex.Lock()
			record.entries[fileName] = formatRecordEntry{Sum: formatRecordSum([]byte(formatted)), Options: options}
			record.mutex.Unlock()
		}
		return formatted, nil
	}
}

// save writes the record, atomically. universe, when not nil, is every file a default-scope run could
// have formatted, and entries for anything outside it are dropped so the record does not keep files
// that were deleted or are now ignored. A run with a narrower scope passes nil and drops nothing.
func (record *formatRecord) save(universe []string) error {
	if record == nil || record.identity == "" {
		return nil
	}
	record.mutex.Lock()
	entries := record.entries
	if universe != nil {
		inUniverse := make(map[string]struct{}, len(universe))
		for _, fileName := range universe {
			inUniverse[fileName] = struct{}{}
		}
		kept := make(map[string]formatRecordEntry, len(entries))
		for fileName, entry := range entries {
			if _, present := inUniverse[fileName]; present {
				kept[fileName] = entry
			}
		}
		entries = kept
	}
	encoded, err := json.Marshal(formatRecordFile{Version: formatRecordVersion, Identity: record.identity, Entries: entries})
	record.mutex.Unlock()
	if err != nil {
		return fmt.Errorf("encoding the format record: %w", err)
	}
	return writeFileAtomically(record.path, encoded)
}

func formatRecordSum(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

// writeFileAtomically writes through a temporary in the destination directory and a rename, so two runs
// sharing a tree never read half of what the other wrote.
func writeFileAtomically(path string, contents []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("creating a temporary in %s: %w", directory, err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return fmt.Errorf("writing %s: %w", temporaryName, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", temporaryName, err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("renaming %s into place: %w", path, err)
	}
	return nil
}

// formatUniverse is every file the default format scope is drawn from: what the formatter handles
// under the root, and under each submodule the root declares, at every depth.
//
// Submodules are walked because an edit inside one is an edit the person running cohere made, and
// git's scope reached them the same way, one repository at a time. Only declared ones, read from each
// repository's own `.gitmodules`: the walk also finds repositories cloned into ignored directories
// (ahra holds six under `projects/`), and those belong to nobody running here. A repository the root
// never declared is still skipped and still named, as the whole-tree walk names it.
type formatUniverse struct {
	// root is the root's own walk, its nested repositories less the submodules walked below.
	root formatfiles.Enumeration

	// submodules is each submodule's walk, outermost first, and submoduleNames its path from the root.
	submodules     []formatfiles.Enumeration
	submoduleNames []string

	files []string
}

func enumerateFormatUniverse(engine formatEngine, root string, structureIgnorePath string) (formatUniverse, error) {
	rootEnumeration, err := engine.Enumerate(root, structureIgnorePath)
	if err != nil {
		return formatUniverse{}, err
	}
	universe := formatUniverse{root: rootEnumeration, files: append([]string(nil), rootEnumeration.Files...)}
	skipped, err := universe.descend(engine, root, root, rootEnumeration, structureIgnorePath)
	if err != nil {
		return formatUniverse{}, err
	}
	universe.root.NestedRepositories = skipped
	return universe, nil
}

// descend walks the submodules a repository declares among the nested repositories its walk found,
// and returns the nested repositories it did not walk.
//
// Structure's ignore defaults apply inside a submodule too: they are the run's layer, not the
// repository's, and for Structure itself they are its own file.
func (universe *formatUniverse) descend(
	engine formatEngine,
	root string,
	directory string,
	enumeration formatfiles.Enumeration,
	structureIgnorePath string,
) ([]string, error) {
	declared, err := declaredSubmodules(directory)
	if err != nil {
		return nil, err
	}
	skipped := []string{}
	for _, nested := range enumeration.NestedRepositories {
		if _, isSubmodule := declared[filepath.ToSlash(nested)]; !isSubmodule {
			skipped = append(skipped, nested)
			continue
		}
		submodule := filepath.Join(directory, nested)
		inner, err := engine.Enumerate(submodule, structureIgnorePath)
		if err != nil {
			return nil, err
		}
		name, _ := filepath.Rel(root, submodule)
		index := len(universe.submodules)
		universe.submodules = append(universe.submodules, inner)
		universe.submoduleNames = append(universe.submoduleNames, name)
		universe.files = append(universe.files, inner.Files...)
		innerSkipped, err := universe.descend(engine, root, submodule, inner, structureIgnorePath)
		if err != nil {
			return nil, err
		}
		universe.submodules[index].NestedRepositories = innerSkipped
	}
	return skipped, nil
}

// declaredSubmodules reads the submodule paths a repository's `.gitmodules` declares. A repository with
// no `.gitmodules` declares none; one that cannot be read is an error, because reading it as empty would
// silently drop every submodule from the scope.
//
// Only the `path` key is read. The file is git's config format, and a value can be quoted; anything
// stranger than that names a path no walk would find, which costs a submodule its formatting and is
// visible in the scope line as a skipped repository.
func declaredSubmodules(directory string) (map[string]struct{}, error) {
	declared := map[string]struct{}{}
	file, err := os.Open(filepath.Join(directory, ".gitmodules"))
	if errors.Is(err, os.ErrNotExist) {
		return declared, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the submodules %s declares: %w", directory, err)
	}
	defer file.Close()
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		key, value, found := strings.Cut(strings.TrimSpace(lines.Text()), "=")
		if !found || strings.TrimSpace(key) != "path" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		declared[strings.TrimSuffix(value, "/")] = struct{}{}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading the submodules %s declares: %w", directory, err)
	}
	return declared, nil
}

// describe says what the universe walked, in the words describeEnumeration uses for one walk, with each
// submodule after the root.
func (universe formatUniverse) describe() string {
	description := describeEnumeration(universe.root, len(universe.root.Files))
	for index, submodule := range universe.submodules {
		description += fmt.Sprintf("; submodule %s: %s",
			universe.submoduleNames[index], describeEnumeration(submodule, len(submodule.Files)))
	}
	return description
}

// unformattedScope is the default format scope: every file in the universe whose bytes are not on record
// as formatted. It returns the universe's files too, so the record can drop entries outside it.
func unformattedScope(engine formatEngine, record *formatRecord, root string, structureIgnorePath string) (formatScope, []string) {
	universe, err := enumerateFormatUniverse(engine, root, structureIgnorePath)
	if err != nil {
		// A failed walk withholds formatting and says why, rather than falling back to a universe that
		// would format the wrong set. Fixing still runs. No universe means nothing is pruned.
		return formatScope{
			index:       map[string]struct{}{},
			Description: fmt.Sprintf("nothing (could not enumerate the tree: %v)", err),
		}, nil
	}

	unformatted := record.unformatted(universe.files, engine.OptionsFingerprint)
	index := make(map[string]struct{}, len(unformatted))
	for _, fileName := range unformatted {
		index[fileName] = struct{}{}
	}

	description := fmt.Sprintf("%d of %d files not on record as formatted at their current bytes",
		len(unformatted), len(universe.files))
	if record.absent != "" {
		description = fmt.Sprintf("all %d files, because %s", len(universe.files), record.absent)
	}
	return formatScope{
		FileNames:   unformatted,
		index:       index,
		Description: description + " · " + universe.describe(),
	}, universe.files
}

// optionsFingerprintOf is a formatter's options fingerprint, or, with no formatter, a function that
// refuses: there are no options to fingerprint, and nothing is recorded without one.
func optionsFingerprintOf(engine formatEngine) func(fileName string) (string, error) {
	if engine == nil {
		return func(string) (string, error) { return "", errors.New("no formatter was configured") }
	}
	return engine.OptionsFingerprint
}
