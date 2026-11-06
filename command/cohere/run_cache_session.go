package main

import (
	"fmt"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

type runCacheSession struct {
	// directory is where this root's cache table lives, invocation the name this run's record goes under
	// in it, and table what was read from it when the run began: this invocation's run and the sections a
	// walk reuses.
	directory  string
	invocation string
	table      *program.CacheTable

	key      string
	recorder *program.InputRecorder

	// anchor is the project root, which the table names paths relative to and the recording names its
	// output's root by, so every spelling of the root is one cache (#547dhjz). See program.PathAnchor.
	anchor program.PathAnchor

	// keyParts is key's parts, recorded with the run so a later one under another key can say which moved.
	keyParts program.RunCacheKeyParts

	// missed is why this run was not replayed whole, and findingsMissed why its files' findings were not, when
	// the findings section as a whole was missing or under another key. Each is said in --verbose and --json, so
	// a run that could have replayed and did not explains itself (#547dhjz).
	missed         string
	findingsMissed string

	// findingsKeyParts is the findings key's parts, recorded with the section. See keyParts.
	findingsKeyParts program.RunCacheKeyParts

	// readSince is when the run began reading its inputs. An input changed after it keeps the run from
	// being recorded.
	readSince time.Time

	// declared is set once the build succeeds and the inputs are known. A session that never declares
	// is never recorded.
	declared   bool
	extraFiles []string

	// extraDirectories is every directory a format walk listed, so a file added, removed or renamed in one
	// moves an input. See declareFormatWalk.
	extraDirectories []string

	// extraAbsent is every file a format walk looked for and did not find, an ignore file a directory
	// does not have, so one appearing moves an input. See declareFormatWalk.
	extraAbsent []string

	// declaredAbove is the root, as given and resolved, of a run whose declared inputs name every path above it
	// that its verdict depends on, so no directory above it is recorded for holding one of them (see
	// program.RecordRunCacheBelow). A Go run's, whose engine declares them; nil for a TypeScript run, whose
	// recorder observes its reads and whose holding directories are all kept.
	declaredAbove []string

	// declined is the reason this run must not be recorded, empty while it may be.
	declined string

	// findings is the findings cache this run consults and records, the run cache's second layer. Nil
	// when the graph never reached a walk.
	findings *program.FindingsReuse

	// shapes is every project file's shape this run, recorded so the next computes only what changed. Nil
	// when no rule is keyed on shapes, since then nothing reads them and computing them would be waste.
	shapes map[string]program.SignatureEntry
	// shapesGraph is the graph shapes describe, the one rebuilt after a fix rewrite when there was one, kept so
	// the shapes still keyed on content can graduate after the verdict. See graduateSignatures.
	shapesGraph *program.Graph

	// types is the types phase's section, what the table held and what this run records. Nil when the
	// graph never reached a walk.
	types *program.TypeDiagnosticsReuse

	// summary is the run's summary, encoded, which a replay renders its footer from. See recordRunSummary.
	summary []byte

	stdout, stderr teeStream
}

// record captures this run, or returns nil when it cannot.
func (session *runCacheSession) record(exitCode int) *program.RunCache {
	// A replay renders its footer from the summary, so a run that recorded none would replay a verdict with
	// no footer. Refused here, once, rather than resting on every verdict path remembering writeRunEnd.
	if session.summary == nil {
		session.note("a run that recorded no summary is not recorded: its replay would print a verdict with no footer")
		return nil
	}
	present, absent, probed := session.recorder.Inputs()
	files := append(present, session.extraFiles...)
	absent = append(absent, session.extraAbsent...)
	cache, recordRunCacheError := program.RecordRunCacheBelow(session.declaredAbove, session.key, files,
		session.extraDirectories, absent, probed, recordedOutput(session.stdout.buffer.Bytes(), session.anchor),
		exitCode, session.readSince)
	if recordRunCacheError != nil {
		// Not recording is always safe. Said on stderr because a cache that silently never records is
		// a saving that quietly never appears.
		session.note(fmt.Sprintf("the run cache did not record this run: %v", firstLine(recordRunCacheError.Error())))
		return nil
	}
	cache.Errors = recordedOutput(session.stderr.buffer.Bytes(), session.anchor)
	cache.KeyParts = session.keyParts
	cache.RecordedUnixNanoseconds = time.Now().UnixNano()
	cache.Summary = recordedOutput(session.summary, session.anchor)
	return cache
}

// write puts this run's record, findings, shapes and types into the table, each into its own file, once.
//
// Only this run's files are written: its own invocation's run, and the sections it produced. Another
// invocation's run is another file, so a bare run and `--no-fix` started together never erase each other's
// record, and nothing has to be read back first to keep it. Two runs writing the same section is a race the
// later one wins, which costs a miss; nothing here can make a stale entry match, since every entry carries
// its own proof.
//
// The findings are saved even when the run itself was declined. Their entries are keyed on each file's
// bytes, so a file the fix phase rewrote left an entry for bytes that no longer exist, which can never
// match.
func (session *runCacheSession) write(recorded *program.RunCache) {
	table := program.NewCacheTable()
	var sections program.CacheTableSections
	if recorded != nil {
		table.Runs[session.invocation] = recorded
		sections.Runs = []string{session.invocation}
	}
	if session.findings != nil {
		table.Findings = session.findings.Recorded()
		table.Findings.KeyParts = session.findingsKeyParts
		sections.Findings = true
	}
	if session.shapes != nil {
		table.Signatures = session.shapes
		sections.Signatures = true
	}
	if session.types != nil {
		if recorded := session.types.Recorded(); recorded != nil {
			table.Types = recorded
			sections.Types = true
		}
	}
	if writeCacheTableError := program.WriteCacheTable(
		session.directory,
		table,
		cacheTableIdentity(),
		sections,
		session.anchor,
	); writeCacheTableError != nil {
		session.note(fmt.Sprintf("the cache table could not be written: %v", firstLine(writeCacheTableError.Error())))
	}
}

// note says something about recording this run. Before the caller has its verdict it goes to stderr, as
// it always did; after, the caller has moved on and its terminal may hold someone else's prompt, so it is
// kept beside the table for the next run to say instead (printPreviousNotes).
func (session *runCacheSession) note(text string) {
	cacheNote(session.directory, text)
}
