package program

import (
	"crypto/sha256"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// The types phase's cache: each project file's semantic diagnostics, replayed while the file's shape
// fingerprint is unchanged, so an edit re-checks only the edited file and the files its shape change
// reaches (#zqsdzbq, from test's benchmark e932bc1a: a one-file comment edit on ahra re-checked all 3,862
// files, 2.05s, nearly all of an edit run).
//
// # Why the shape fingerprint is the right key
//
// A file's semantic diagnostics are a function of its own text and of the types it can see: its import
// closure and everything global. SignatureFingerprints hashes exactly that: the file's own bytes, every
// file in its closure by its shape (its declaration output and its syntax with function bodies cut out),
// where its imports resolved, and the global component, bytes and all (installed declarations, scripts,
// anything that augments the global scope). An edit inside an imported function's body moves the
// importer's diagnostics only through the function's inferred type, which is part of its declaration
// output, so it moves the importer's key too. That is the same bar the compiler's own incremental builder
// holds a file's signature to.
//
// # What is never replayed
//
// A file whose diagnostics carry related information or a chain located in another file: a body edit there
// can move the position without moving the shape. A diagnostic whose text the compiler repopulates from
// program state. And everything, whenever the run that recorded the section saw a global diagnostic: those
// are produced as a side effect of checking files, so a run that skipped the files could not reproduce
// them. Syntactic and bind diagnostics are not cached at all; they are cheap and every run computes them.
//
// # What the fingerprint cannot see
//
// The compiler options. Turning on strict moves no file's text, shape or resolutions, and every file it
// newly fails would replay as clean (#hfv0ae3, proven against 3bef97f). So the section carries a key over
// what the caller knows the options come from, and a section under another key replays nothing.

// Version 2 added the key; a version 1 section, which has none, is never replayed.
const typesSectionVersion = 2

// TypesSection is the types phase's section of the cache table.
type TypesSection struct {
	Version int

	// Key is what the diagnostics depend on beyond the files the fingerprints cover: the tsconfig and
	// everything it extends. See NewTypeDiagnosticsReuse.
	Key [sha256.Size]byte

	// GlobalsClean is whether the run that recorded it saw no global diagnostics. Only then can a run that
	// skips files report what a run that checked them all would.
	GlobalsClean bool

	// Entries is each project file's semantic diagnostics, by file name, under the shape fingerprint they
	// were computed against. A file with none has an entry with none.
	Entries map[string]TypesEntry
}

// TypesEntry is one file's semantic diagnostics and the fingerprint they hold under.
type TypesEntry struct {
	Fingerprint [sha256.Size]byte
	Diagnostics []StoredDiagnostic
}

// TypeDiagnosticsReuse is one run's use of the section: what was stored, and what this run records.
type TypeDiagnosticsReuse struct {
	stored *TypesSection
	key    [sha256.Size]byte

	mutex    sync.Mutex
	recorded *TypesSection
	replayed int

	// replayedFiles is the files whose semantic diagnostics this run took from the section, by file name, so
	// a run scoped to some of the program can say how many of those it replayed (ReplayedAmong).
	replayedFiles map[string]struct{}
}

// NewTypeDiagnosticsReuse wraps what the table held, which may be nil, for a run whose compiler options
// hash to key. Only a section recorded under the same key is replayed, and what this run records carries it.
func NewTypeDiagnosticsReuse(stored *TypesSection, key [sha256.Size]byte) *TypeDiagnosticsReuse {
	return &TypeDiagnosticsReuse{stored: stored, key: key}
}

// Unchanged reports whether what this run recorded is what the table already holds: every file replayed
// under the same key, and the global verdict the same. Writing it back would rewrite the table for nothing.
func (reuse *TypeDiagnosticsReuse) Unchanged() bool {
	reuse.mutex.Lock()
	defer reuse.mutex.Unlock()
	if reuse.recorded == nil || reuse.stored == nil {
		return reuse.recorded == nil
	}
	return reuse.stored.Key == reuse.recorded.Key && reuse.stored.GlobalsClean == reuse.recorded.GlobalsClean &&
		reuse.replayed == len(reuse.recorded.Entries) && len(reuse.recorded.Entries) == len(reuse.stored.Entries)
}

// Replayed is how many files' semantic diagnostics this run took from the section.
func (reuse *TypeDiagnosticsReuse) Replayed() int {
	reuse.mutex.Lock()
	defer reuse.mutex.Unlock()
	return reuse.replayed
}

// ReplayedAmong is how many of files had their semantic diagnostics taken from the section. The check
// covers the whole program whatever the run's scope, so Replayed counts files a scoped run never reports
// on, and a line saying "3955 of 1 files" read as nonsense (#rxqptqp).
func (reuse *TypeDiagnosticsReuse) ReplayedAmong(files []*ast.SourceFile) int {
	reuse.mutex.Lock()
	defer reuse.mutex.Unlock()
	count := 0
	for _, sourceFile := range files {
		if _, replayed := reuse.replayedFiles[sourceFile.FileName().AsString()]; replayed {
			count++
		}
	}
	return count
}

// Recorded is the section this run leaves for the next, nil when it recorded nothing.
func (reuse *TypeDiagnosticsReuse) Recorded() *TypesSection {
	reuse.mutex.Lock()
	defer reuse.mutex.Unlock()
	return reuse.recorded
}

// FinishGlobals says whether this run saw any global diagnostic. Until it is told none, what it recorded
// may only be read as a full check: a run that saw one records nothing to replay.
func (reuse *TypeDiagnosticsReuse) FinishGlobals(clean bool) {
	reuse.mutex.Lock()
	defer reuse.mutex.Unlock()
	if reuse.recorded != nil {
		reuse.recorded.GlobalsClean = clean
	}
}

// typesEntryFor stores a file's semantic diagnostics, or reports false when any of them cannot be replayed
// faithfully: located in another file, or repopulated from program state.
func typesEntryFor(
	sourceFile *ast.SourceFile,
	fingerprint [sha256.Size]byte,
	diagnostics []*ast.Diagnostic,
) (TypesEntry, bool) {
	entry := TypesEntry{Fingerprint: fingerprint, Diagnostics: make([]StoredDiagnostic, 0, len(diagnostics))}
	for _, diagnostic := range diagnostics {
		stored, keepable := storeDiagnostic(diagnostic, sourceFile)
		if !keepable {
			return TypesEntry{}, false
		}
		entry.Diagnostics = append(entry.Diagnostics, stored)
	}
	return entry, true
}

func storeDiagnostic(diagnostic *ast.Diagnostic, sourceFile *ast.SourceFile) (StoredDiagnostic, bool) {
	if diagnostic.RepopulateInfo() != nil || (diagnostic.File() != nil && diagnostic.File() != sourceFile) {
		return StoredDiagnostic{}, false
	}
	stored := StoredDiagnostic{
		Pos:                diagnostic.Pos(),
		End:                diagnostic.End(),
		Code:               diagnostic.Code(),
		Category:           int64(diagnostic.Category()),
		MessageKey:         string(diagnostic.MessageKey()),
		MessageArgs:        diagnostic.MessageArgs(),
		Source:             diagnostic.Source(),
		MessageText:        diagnostic.MessageText(),
		ReportsUnnecessary: diagnostic.ReportsUnnecessary(),
		ReportsDeprecated:  diagnostic.ReportsDeprecated(),
		SkippedOnNoEmit:    diagnostic.SkippedOnNoEmit(),
		NoFile:             diagnostic.File() == nil,
	}
	// A diagnostic with neither a key nor external text is an ad hoc message, which nothing here can rebuild.
	if stored.MessageKey == "" && stored.MessageText == "" {
		return StoredDiagnostic{}, false
	}
	for _, part := range diagnostic.MessageChain() {
		chained, keepable := storeDiagnostic(part, sourceFile)
		if !keepable {
			return StoredDiagnostic{}, false
		}
		stored.Chain = append(stored.Chain, chained)
	}
	for _, part := range diagnostic.RelatedInformation() {
		related, keepable := storeDiagnostic(part, sourceFile)
		if !keepable {
			return StoredDiagnostic{}, false
		}
		stored.Related = append(stored.Related, related)
	}
	return stored, true
}
