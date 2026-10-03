package program

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/replace"
)

// The cache table: everything cohere keeps between runs for one project root, in one file.
//
// It holds the run cache's recorded runs, one per invocation, and the findings cache's per-file entries.
// Each section keeps its own key and its own proof, exactly as when they were two JSON files; what
// changed is that they share one file, one encoding, and one header that decides whether the file may
// be read at all.
//
// # Why gob
//
// The two JSON files were 7.9 MB and 1.45 MB on ahra, decoding in 14ms and 3ms and encoding in 7ms and
// 25ms. Measured on #6tfmk0b: decode was about 6% of a replay, so speed was not the reason. The reason is
// that gob is written by reflection over the structs themselves. The lint cache's history is an encoder
// that forgot a field, shipped four times across a hand-written binary layout and a JSON one, and a
// format nobody writes by hand cannot forget a field.
//
// # The one way gob can lie, and the guard
//
// gob matches fields by name and skips what it does not recognize, in both directions. A field renamed,
// added or removed decodes without error into a zero value, which is the silent-default failure this
// format was chosen to escape, arriving by another door. So the header carries cacheTableVersion, and
// TestCacheTableShapeIsPinnedToItsVersion fails whenever the shape of what is encoded changes and the
// version does not. A changed shape always arrives as a discard, never as a partial read.
//
// # Discard, never repair
//
// The header names the format, the cohere commit, the compiler commit, the Go toolchain and the platform
// that wrote the file. Any of them differing, a decode error, or a byte left over after the body is a
// discard of the whole file: the run starts cold, says so once on stderr, and writes a fresh table. A
// discard costs one ordinary run. A repaired cache that is wrong costs a green run over a broken tree.
//
// One exception, and it is narrow. A table written by another cohere commit, with the same format, compiler,
// toolchain and platform, keeps its format record and drops everything else. The format record is keyed by
// the formatter's own identity (FormatSection.Key), which moves only when the formatter can print
// differently, so a cohere commit that never touched the formatter need not reformat every file. The
// encoding is still guaranteed by the format version, so nothing here reads a field from the wrong place.
type CacheTable struct {
	// Runs is one recorded run per invocation, keyed by CacheTableInvocation. A bare run and `--no-fix`
	// print different reports under different keys, and kept apart they never overwrite each other.
	Runs map[string]*RunCache

	// Findings is the per-file findings cache, nil until a run has walked.
	Findings *LintCache

	// Signatures is each project file's shape, recorded so the next run computes only the files whose
	// bytes changed. Nil until a run has needed shapes.
	Signatures map[string]SignatureEntry

	// Formatted is the format record: which bytes cohere has already seen formatted, so a run formats
	// what changed since cohere last looked. Nil until a run has formatted. Its meaning is the format
	// phase's (command/cohere/format_record.go); this file only keeps it.
	Formatted *FormatSection

	// Types is each project file's semantic diagnostics under its shape fingerprint, for the types phase to
	// replay. Nil until a run has recorded them. See TypesSection.
	Types *TypesSection
}

// FormatSection is the format record's section of the table. Key names the binary and root its entries
// were recorded under; a section under any other key says nothing, since a rebuilt formatter may print
// differently.
type FormatSection struct {
	Key     string
	Entries map[string]FormatEntry
}

// FormatEntry is one file's format status: Sum is the SHA-256 of a text the formatter left unchanged
// under the options Options fingerprints, and Size and ModifiedNanoseconds are the file's signature
// when its bytes last matched Sum, so an unchanged file is a stat rather than a read.
type FormatEntry struct {
	Sum                 string
	Options             string
	Size                int64
	ModifiedNanoseconds int64
}

// CacheTableIdentity is what built the binary that writes or reads a table. It goes in the header, so a
// table written by any other build is discarded before a byte of its body is trusted.
type CacheTableIdentity struct {
	SelfCommit     string
	CompilerCommit string
	GoToolchain    string
	Platform       string
}

// cacheTableVersion is the format, and it moves whenever the encoded shape or its meaning does. The shape
// half is enforced by TestCacheTableShapeIsPinnedToItsVersion; the meaning half is the reason each
// section also keeps its own version.
//
// 5: the table holds the types phase's section.
//
// 4: run-cache inputs carry their change time and inode.
//
// 3: findings entries carry design-system rules and their fingerprint, and the findings section carries the
// design system's key.
//
// 2: findings entries carry shape-keyed rules and their fingerprint, and the table holds Signatures.
const cacheTableVersion = 5

// cacheTableMagic opens every table, so a file that is not one is refused on its first field.
const cacheTableMagic = "cohere cache table"

// ErrCacheTableUnreadable means the file is not a table this build may use. Never a reason to fail a run:
// the answer is to run cold and write a new one.
var ErrCacheTableUnreadable = errors.New("cache table discarded")

// ErrCacheTablePartlyKept means the table was written by another cohere commit: its format record is kept,
// for its own key to decide, and its runs and findings are dropped. Reported like a discard, since most of
// what the table held is gone.
var ErrCacheTablePartlyKept = errors.New("cache table written by another cohere commit")

type cacheTableHeader struct {
	Magic    string
	Version  int
	Identity CacheTableIdentity
}

// cacheTableBody is what follows the header.
type cacheTableBody struct {
	Runs       map[string]*RunCache
	Findings   *lintCacheWire
	Signatures map[string]SignatureEntry
	Formatted  *FormatSection
	Types      *TypesSection
}

// lintCacheWire is the findings section as encoded. Rule lists are stored once and referenced by index:
// on ahra 3,605 entries share 4 distinct rule lists and 24 listening lists, and writing each out in full
// once made the file 53 MB.
type lintCacheWire struct {
	Version int
	Key     [sha256.Size]byte
	Lists   [][]string
	Entries []lintCacheWireEntry

	DesignSystem *DesignSystemKey
}

type lintCacheWireEntry struct {
	Path            string
	ContentHash     [sha256.Size]byte
	Rules           int
	TypedRules      int
	TypeFingerprint [sha256.Size]byte
	Listening       int
	VisitedNodes    int
	Findings        []LintCacheFinding

	ShapedRules      int
	ShapeFingerprint [sha256.Size]byte

	DesignRules       int
	DesignFingerprint [sha256.Size]byte
}

// NewCacheTable is an empty table, what a first run and every discard start from.
func NewCacheTable() *CacheTable {
	return &CacheTable{Runs: map[string]*RunCache{}}
}

// CacheTableInvocation names an invocation within a table.
func CacheTableInvocation(arguments []string) string {
	return strings.Join(arguments, "\x00")
}

// EncodeCacheTable writes a table, header first.
func EncodeCacheTable(table *CacheTable, identity CacheTableIdentity) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	if err := encoder.Encode(cacheTableHeader{Magic: cacheTableMagic, Version: cacheTableVersion, Identity: identity}); err != nil {
		return nil, fmt.Errorf("encoding the cache table's header: %w", err)
	}
	body := cacheTableBody{Runs: table.Runs, Signatures: table.Signatures, Formatted: table.Formatted, Types: table.Types}
	if table.Findings != nil {
		body.Findings = table.Findings.wire()
	}
	if err := encoder.Encode(body); err != nil {
		return nil, fmt.Errorf("encoding the cache table: %w", err)
	}
	return buffer.Bytes(), nil
}

// DecodeCacheTable reads a table written by EncodeCacheTable under the same identity. Anything else is
// ErrCacheTableUnreadable, with the reason.
func DecodeCacheTable(buffer []byte, identity CacheTableIdentity) (*CacheTable, error) {
	reader := bytes.NewReader(buffer)
	decoder := gob.NewDecoder(reader)

	var header cacheTableHeader
	if err := decoder.Decode(&header); err != nil {
		return nil, fmt.Errorf("%w: unreadable header: %v", ErrCacheTableUnreadable, err)
	}
	if header.Magic != cacheTableMagic {
		return nil, fmt.Errorf("%w: not a cache table", ErrCacheTableUnreadable)
	}
	if header.Version != cacheTableVersion {
		return nil, fmt.Errorf("%w: format version %d, this build reads %d", ErrCacheTableUnreadable, header.Version, cacheTableVersion)
	}
	onlySelfCommitDiffers := false
	if header.Identity != identity {
		otherCommit := header.Identity
		otherCommit.SelfCommit = identity.SelfCommit
		if otherCommit != identity {
			return nil, fmt.Errorf("%w: written by %s, this is %s", ErrCacheTableUnreadable, header.Identity, identity)
		}
		onlySelfCommitDiffers = true
	}

	var body cacheTableBody
	if err := decoder.Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: unreadable body: %v", ErrCacheTableUnreadable, err)
	}
	// gob stops at the end of the value it was asked for, so bytes after it would otherwise go unread. A
	// file longer than its contents is a file something else wrote into.
	if reader.Len() != 0 {
		return nil, fmt.Errorf("%w: %d bytes after the body", ErrCacheTableUnreadable, reader.Len())
	}

	if onlySelfCommitDiffers {
		table := NewCacheTable()
		table.Formatted = body.Formatted
		return table, fmt.Errorf("%w (%s, this is %s): its runs and findings are dropped, and its format record is kept for its own key to decide",
			ErrCacheTablePartlyKept, orUnknown(header.Identity.SelfCommit), orUnknown(identity.SelfCommit))
	}

	table := &CacheTable{Runs: body.Runs, Signatures: body.Signatures, Formatted: body.Formatted, Types: body.Types}
	if table.Runs == nil {
		table.Runs = map[string]*RunCache{}
	}
	if body.Findings != nil {
		findings, err := body.Findings.cache()
		if err != nil {
			return nil, fmt.Errorf("%w: findings: %v", ErrCacheTableUnreadable, err)
		}
		table.Findings = findings
	}
	return table, nil
}

func (identity CacheTableIdentity) String() string {
	return fmt.Sprintf("cohere %s, compiler %s, %s, %s",
		orUnknown(identity.SelfCommit), orUnknown(identity.CompilerCommit), identity.GoToolchain, identity.Platform)
}

func orUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

// wire interns the findings cache's rule lists. It is stamped with this build's lintCacheVersion: a cache in
// memory always means what this build means.
func (c *LintCache) wire() *lintCacheWire {
	wire := &lintCacheWire{Version: lintCacheVersion, Key: c.Key, DesignSystem: c.DesignSystem}
	// Interned by a cheap key, the length and the two ends, and confirmed by comparing names, never by joining
	// a list into one string: ahra's entries hold about 18,000 lists of up to ~250 names, and joining each cost
	// about 25ms of every recording run to find 31 distinct ones (#a66sfmh).
	type listKey struct {
		length      int
		first, last string
	}
	positions := map[listKey][]int{}
	intern := func(list []string) int {
		key := listKey{length: len(list)}
		if len(list) > 0 {
			key.first, key.last = list[0], list[len(list)-1]
		}
		for _, position := range positions[key] {
			if equalStrings(wire.Lists[position], list) {
				return position
			}
		}
		positions[key] = append(positions[key], len(wire.Lists))
		wire.Lists = append(wire.Lists, append([]string{}, list...))
		return len(wire.Lists) - 1
	}
	for _, entry := range c.Entries {
		wire.Entries = append(wire.Entries, lintCacheWireEntry{
			Path:            entry.Path,
			ContentHash:     entry.ContentHash,
			Rules:           intern(entry.Rules),
			TypedRules:      intern(entry.TypedRules),
			TypeFingerprint: entry.TypeFingerprint,
			Listening:       intern(entry.Listening),
			VisitedNodes:    entry.VisitedNodes,
			Findings:        entry.Findings,

			ShapedRules:      intern(entry.ShapedRules),
			ShapeFingerprint: entry.ShapeFingerprint,

			DesignRules:       intern(entry.DesignRules),
			DesignFingerprint: entry.DesignFingerprint,
		})
	}
	return wire
}

// cache resolves the interned lists. An index past its table is an error rather than an empty list: an
// index trusted past its table would hand an entry another file's rule list.
//
// A findings section of another version decodes to nil rather than failing the table. The runs beside it
// are proven by their own keys, and a findings cache is only ever a miss when it is absent.
func (wire *lintCacheWire) cache() (*LintCache, error) {
	if wire.Version != lintCacheVersion {
		return nil, nil
	}
	cache := &LintCache{Version: wire.Version, Key: wire.Key, Entries: make([]LintCacheEntry, 0, len(wire.Entries)), DesignSystem: wire.DesignSystem}
	list := func(index int) ([]string, error) {
		if index < 0 || index >= len(wire.Lists) {
			return nil, fmt.Errorf("list %d of %d", index, len(wire.Lists))
		}
		return wire.Lists[index], nil
	}
	for position, stored := range wire.Entries {
		entry := LintCacheEntry{
			Path:            stored.Path,
			ContentHash:     stored.ContentHash,
			TypeFingerprint: stored.TypeFingerprint,
			VisitedNodes:    stored.VisitedNodes,
			Findings:        stored.Findings,

			ShapeFingerprint: stored.ShapeFingerprint,

			DesignFingerprint: stored.DesignFingerprint,
		}
		var err error
		if entry.Rules, err = list(stored.Rules); err != nil {
			return nil, fmt.Errorf("entry %d rules: %v", position, err)
		}
		if entry.TypedRules, err = list(stored.TypedRules); err != nil {
			return nil, fmt.Errorf("entry %d typed rules: %v", position, err)
		}
		if entry.Listening, err = list(stored.Listening); err != nil {
			return nil, fmt.Errorf("entry %d listening: %v", position, err)
		}
		if entry.ShapedRules, err = list(stored.ShapedRules); err != nil {
			return nil, fmt.Errorf("entry %d shaped rules: %v", position, err)
		}
		if entry.DesignRules, err = list(stored.DesignRules); err != nil {
			return nil, fmt.Errorf("entry %d design-system rules: %v", position, err)
		}
		cache.Entries = append(cache.Entries, entry)
	}
	return cache, nil
}

// ReadCacheTable loads a table. It always returns one: the table on disk when it is readable, what may be
// kept of it when another cohere commit wrote it, and an empty one otherwise. The error says why it is not
// whole. A missing file wraps os.ErrNotExist, which is a first run and nothing to report. Anything else
// wraps ErrCacheTableUnreadable or ErrCacheTablePartlyKept, which a caller reports, because a cache that
// is silently thrown away every run is a saving that quietly never appears.
func ReadCacheTable(path string, identity CacheTableIdentity) (*CacheTable, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewCacheTable(), err
		}
		return NewCacheTable(), fmt.Errorf("%w: %v", ErrCacheTableUnreadable, err)
	}
	table, err := DecodeCacheTable(contents, identity)
	if errors.Is(err, ErrCacheTablePartlyKept) {
		return table, err
	}
	if err != nil {
		return NewCacheTable(), err
	}
	return table, nil
}

// beforeCacheTableRename, when set, runs between writing the temporary and renaming it into place. Only a
// test sets it, to stop a writer at the one moment a kill could matter.
var beforeCacheTableRename func(temporaryName string)

// WriteCacheTable persists a table atomically: a temporary in the destination directory, then a rename.
// Two runs can share a tree, and a reader must never see half a table. A run that returned to its caller
// early writes after the caller has moved on, where nothing stops a kill landing mid-write; the rename is
// what keeps the old table whole if one does (TestAWriterKilledBeforeItsRenameLeavesTheOldTable).
func WriteCacheTable(path string, table *CacheTable, identity CacheTableIdentity) error {
	encoded, err := EncodeCacheTable(table, identity)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, ".cachetable-*")
	if err != nil {
		return fmt.Errorf("creating a temporary in %s: %w", directory, err)
	}
	temporaryName := temporary.Name()
	defer func() {
		if temporaryName != "" {
			os.Remove(temporaryName)
		}
	}()
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return fmt.Errorf("writing %s: %w", temporaryName, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", temporaryName, err)
	}
	if beforeCacheTableRename != nil {
		beforeCacheTableRename(temporaryName)
	}
	// replace.File, which retries through Windows refusing the rename while another run reads the table.
	if err := replace.File(temporaryName, path); err != nil {
		return fmt.Errorf("renaming the cache table into place: %w", err)
	}
	temporaryName = ""
	return nil
}

// DumpCacheTable prints a table for a person: every run and every file entry, one line each, so a cache
// that looks wrong can be read rather than guessed at.
func DumpCacheTable(out io.Writer, path string, table *CacheTable, identity CacheTableIdentity) {
	fmt.Fprintf(out, "cache table %s\n", path)
	fmt.Fprintf(out, "  format %d, written by %s\n", cacheTableVersion, identity)

	invocations := make([]string, 0, len(table.Runs))
	for invocation := range table.Runs {
		invocations = append(invocations, invocation)
	}
	sort.Strings(invocations)
	fmt.Fprintf(out, "runs: %d\n", len(invocations))
	for _, invocation := range invocations {
		run := table.Runs[invocation]
		name := strings.ReplaceAll(invocation, "\x00", " ")
		if name == "" {
			name = "(bare)"
		}
		files, directories, absent := 0, 0, 0
		for _, input := range run.Inputs {
			switch {
			case !input.Exists:
				absent++
			case input.Directory:
				directories++
			default:
				files++
			}
		}
		fmt.Fprintf(out, "  %s: recorded %s, exit %d, key %.12s, %d inputs (%d files, %d directories, %d absent), %d bytes of output, %d of errors\n",
			name, time.Unix(0, run.RecordedUnixNanoseconds).Format(time.DateTime), run.ExitCode, run.Key,
			len(run.Inputs), files, directories, absent, len(run.Output), len(run.Errors))
		if changed := run.ChangedInput(); changed != nil {
			fmt.Fprintf(out, "    now: would not replay, %v\n", changed)
		} else {
			fmt.Fprintln(out, "    now: every input as recorded, so it replays if the binary, flags and directory match")
		}
	}

	fmt.Fprintf(out, "signatures: %d files\n", len(table.Signatures))

	if table.Formatted == nil {
		fmt.Fprintln(out, "formatted: none")
	} else {
		fmt.Fprintf(out, "formatted: %d files, key %.12s\n", len(table.Formatted.Entries), table.Formatted.Key)
	}

	if table.Findings == nil {
		fmt.Fprintln(out, "findings: none")
		return
	}
	findingCount := 0
	for _, entry := range table.Findings.Entries {
		findingCount += len(entry.Findings)
	}
	fmt.Fprintf(out, "findings: %d files, %d findings, key %x\n", len(table.Findings.Entries), findingCount, table.Findings.Key[:6])
	if design := table.Findings.DesignSystem; design == nil {
		fmt.Fprintln(out, "  design system: none recorded")
	} else {
		fmt.Fprintf(out, "  design system: %x, %d paths read\n", design.Fingerprint[:6], len(design.Reads))
		for _, read := range design.Reads {
			state := "absent"
			switch {
			case read.Directory:
				state = fmt.Sprintf("directory %x", read.Hash[:6])
			case read.Present:
				state = fmt.Sprintf("file %x", read.Hash[:6])
			}
			fmt.Fprintf(out, "    %s  %s\n", read.Path, state)
		}
	}
	for _, entry := range table.Findings.Entries {
		fmt.Fprintf(out, "  %s  content %x  types %x  shapes %x  design %x  %d pure + %d type-aware + %d shape-keyed + %d design-system rules, %d listening, %d nodes, %d findings\n",
			entry.Path, entry.ContentHash[:6], entry.TypeFingerprint[:6], entry.ShapeFingerprint[:6], entry.DesignFingerprint[:6], len(entry.Rules),
			len(entry.TypedRules), len(entry.ShapedRules), len(entry.DesignRules), len(entry.Listening), entry.VisitedNodes, len(entry.Findings))
		for _, finding := range entry.Findings {
			fmt.Fprintf(out, "    %d-%d %s/%s: %s\n", finding.Start, finding.End, finding.RuleName, finding.MessageId, finding.MessageDescription)
		}
	}
}
