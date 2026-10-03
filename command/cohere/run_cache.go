package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/system-inc/cohere/internal/format/formatfiles"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/internal/types/program"
)

// The run cache's command half: decide whether this run can be replayed, replay it if so, and
// otherwise record it on the way out.
//
// The package half (internal/types/program/run_cache.go) proves inputs unchanged. This half decides
// which runs are eligible and which exits are verdicts, and it is deliberately narrow on both.
//
// # Which runs
//
// A bare `cohere`, or `cohere --no-fix`, and nothing else. Those are the runs people repeat, and every
// other flag widens the set of things the output could depend on. The key covers arguments anyway, so
// admitting more later is a change to one condition rather than to the proof.
//
// # Which exits
//
// A run is recorded only when it ends in a verdict about the tree: clean, findings, or the type bail.
// A run that fails as a tool (an error out of run) is never recorded, since replaying it would replay a
// failure of the tool as if it were a fact about the code. And a run is recorded only if it declared
// its inputs, so any path that never reaches the declaration (Swift, listings, rename, stdin) cannot be
// recorded by construction rather than by a list someone has to keep complete.
//
// # What a replay prints
//
// The recorded report, framed. That report carries the recorded run's durations, and printed bare
// they would claim this run took seconds and built a graph it did not build. So a replay says when the
// report is from before it, and what this run actually cost after it.

// activeRunCache is the recording in progress, nil when this run is not being recorded.
var activeRunCache *runCacheSession

type runCacheSession struct {
	// tablePath is where this root's cache table lives, invocation the name this run's record goes under
	// in it, and table what was read from it when the run began.
	tablePath  string
	invocation string
	table      *program.CacheTable

	// tableSignature is the table file's size and time just before it was read, so the write at the end can
	// tell whether anything has replaced it since. See write.
	tableSignature fileSignature

	key      string
	recorder *program.InputRecorder

	// declared is set once the build succeeds and the inputs are known. A session that never declares
	// is never recorded.
	declared   bool
	extraFiles []string

	// declined is the reason this run must not be recorded, empty while it may be.
	declined string

	// findings is the findings cache this run consults and records, the run cache's second layer. Nil
	// when the graph never reached a walk.
	findings *program.FindingsReuse

	// shapes is every project file's shape this run, recorded so the next computes only what changed. Nil
	// when no rule is keyed on shapes, since then nothing reads them and computing them would be waste.
	shapes map[string]program.SignatureEntry

	stdout, stderr teeStream
}

// teeStream copies a standard stream to its real destination and to a buffer.
type teeStream struct {
	real   *os.File
	writer *os.File
	buffer bytes.Buffer
	done   chan struct{}
}

func (t *teeStream) start(target **os.File) error {
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	t.real = *target
	t.writer = writer
	t.done = make(chan struct{})
	go func() {
		defer close(t.done)
		defer reader.Close()
		// Line by line, so a line tagged as describing this invocation reaches the terminal untagged
		// and never reaches the replay. The tag travels in the stream itself because the tee is a pipe:
		// noting the buffer's length at the moment of printing would read it before the pipe delivered.
		lines := bufio.NewReader(reader)
		for {
			line, err := lines.ReadBytes('\n')
			if len(line) > 0 {
				if rest, tagged := bytes.CutPrefix(line, invocationTag); tagged {
					t.real.Write(rest)
				} else if rest, tagged := bytes.CutPrefix(line, provenanceTag); tagged {
					// Kept tagged in the recording, so a replay knows to say where the line came from.
					t.real.Write(rest)
					t.buffer.Write(line)
				} else {
					t.real.Write(line)
					t.buffer.Write(line)
				}
			}
			if err != nil {
				return
			}
		}
	}()
	*target = writer
	return nil
}

// stop restores the stream and waits for everything written so far to reach both destinations.
func (t *teeStream) stop(target **os.File) {
	if t.writer == nil {
		return
	}
	*target = t.real
	t.writer.Close()
	<-t.done
	t.writer = nil
}

// runCacheEligible reports whether this invocation may use the run cache at all.
//
// A bare run, and `--no-fix` alone. `--no-fix` runs every phase and writes nothing, so it is a pure
// report, which suits a cache better than the bare run does; it is what a real-tree measurement uses,
// so the probe asks to write nothing as well as being sandboxed. The key covers the arguments, so the
// two never replay each other.
//
// `--no-cache` is refused by name rather than left to the argument shape. Any flag makes a run
// ineligible today, but the promise that flag makes, nothing read and nothing written, should not rest
// on which arguments happen to be admitted next.
func runCacheEligible() bool {
	if cacheOff {
		return false
	}
	switch {
	case len(os.Args) == 1:
	case len(os.Args) == 2 && os.Args[1] == "--no-fix":
	default:
		return false
	}
	return true
}

// beginRunCache replays a recorded run and exits if every input is unchanged, and otherwise starts
// recording this one. It returns the recorder the graph build should report its inputs to, or nil
// when this run is not eligible.
func beginRunCache(location projectLocation) *program.InputRecorder {
	if !runCacheEligible() {
		return nil
	}
	// No format scope is in the key, and that is only sound because an eligible run never formats: with
	// no formatter its scope is the constant "formatting was not requested", so nothing it prints depends
	// on which files changed. A run that formats is never eligible.
	key, err := program.RunCacheKey(os.Args[1:], location.Root,
		"root="+location.Root,
		"tsconfig="+location.ConfigFileName,
		"lint-config="+location.LintConfigFileName,
	)
	if err != nil {
		return nil
	}

	prepareCacheDirectory(location.Root)
	tablePath := cacheTablePath(location.Root)
	// The run before this one may still be writing the table after answering its caller, and reading
	// before its rename would miss what it recorded. See table_lock.go.
	waitForTableWriter(tablePath, tableWriterWait)
	// Statted before it is read, never after: a table replaced between the two then reads as replaced at the
	// end, and is read again, where the other order could keep a stale copy.
	printPreviousNotes(tablePath)
	tableSignature := signatureOfFile(tablePath)
	table, err := program.ReadCacheTable(tablePath, cacheTableIdentity())
	if errors.Is(err, program.ErrCacheTableUnreadable) || errors.Is(err, program.ErrCacheTablePartlyKept) {
		// Said once, before the recording starts, so it reaches the terminal and never a replay. A table
		// thrown away on every run would otherwise look like a cache that is merely cold.
		fmt.Fprintf(os.Stderr, "note: %v; this run starts cold and writes a new one\n", err)
	}
	invocation := program.CacheTableInvocation(os.Args[1:])
	if stored := table.Runs[invocation]; stored.Check(key) == nil {
		replayRunCache(stored)
	}

	session := &runCacheSession{
		tablePath:      tablePath,
		invocation:     invocation,
		table:          table,
		tableSignature: tableSignature,
		key:            key,
		recorder:       program.NewInputRecorder(),
	}
	if err := session.stdout.start(&os.Stdout); err != nil {
		return nil
	}
	if err := session.stderr.start(&os.Stderr); err != nil {
		session.stdout.stop(&os.Stdout)
		return nil
	}
	activeRunCache = session
	return session.recorder
}

// cacheTablePath is the project's cache table, in the project: `<root>/.cache/cohere/table.gob`.
//
// It lived in the user cache under a hash of the root path, which nothing ever reclaimed. A moved or
// deleted project left its table behind for good, and 38 of them had piled up by 2026-10-02. In the
// project it is one folder per project that deleting is always a correct answer for, and a worktree has
// its own because it is its own root. Every eligible invocation shares it; their recorded runs sit side
// by side under their own invocation, and their findings are the same findings.
func cacheTablePath(root string) string {
	return filepath.Join(cacheDirectory(root), "table.gob")
}

// cacheDirectory is where cohere keeps what it caches for one project.
func cacheDirectory(root string) string {
	return filepath.Join(root, ".cache", "cohere")
}

// prepareCacheDirectory creates the project's cache directory before anything is read or recorded, and
// warns once when the project's .gitignore does not ignore it.
//
// Created first because the run cache records the project root's directory as an input, at modification
// time. Creating `.cache` moves the root's time, and doing it after the inputs were recorded would make
// the first replay of every project a miss that looks merely cold. Writes inside `.cache/cohere` move only
// that directory's time, which no tsconfig enumerates, since a wildcard never matches a dot directory.
//
// The warning is a warning rather than a refusal: the cache is still the project's to keep, and the line
// to add is named so it is one edit away. What it must never be is a silent write into tracked space.
var prepareCacheDirectoryOnce sync.Once

func prepareCacheDirectory(root string) {
	prepareCacheDirectoryOnce.Do(func() {
		if err := os.MkdirAll(cacheDirectory(root), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "note: the cache directory %s could not be created: %v\n", cacheDirectory(root), firstLine(err.Error()))
			return
		}
		ignoreFile := filepath.Join(root, ".gitignore")
		if line, _, err := formatfiles.IgnoringLine(ignoreFile, filepath.Join(".cache", "cohere", "table.gob")); err == nil && line == 0 {
			fmt.Fprintf(os.Stderr, "note: %s does not ignore .cache/, so cohere's cache in %s would be tracked: add the line `.cache/` to it\n",
				ignoreFile, cacheDirectory(root))
		}
	})
}

// readFormatSection is the format record's section of this root's table, nil when there is none or the
// table was discarded. The caller decides whether its key still holds.
func readFormatSection(root string) *program.FormatSection {
	if cacheOff {
		return nil
	}
	waitForTableWriter(cacheTablePath(root), tableWriterWait)
	table, _ := program.ReadCacheTable(cacheTablePath(root), cacheTableIdentity())
	return table.Formatted
}

// writeFormatSection replaces the format record's section and keeps every other section as it is on disk.
// It stands alone because a run that formats is never a recorded run, so no session is there to carry it.
func writeFormatSection(root string, section *program.FormatSection) error {
	if cacheOff {
		return nil
	}
	prepareCacheDirectory(root)
	path := cacheTablePath(root)
	identity := cacheTableIdentity()
	// Held across the read and the write, so a run still writing in the background after answering its
	// caller cannot land its table between the two and have this write drop what it recorded.
	release := holdTableLock(path)
	defer release()
	table, _ := program.ReadCacheTable(path, identity)
	table.Formatted = section
	return program.WriteCacheTable(path, table, identity)
}

// dumpCacheTable is `--cache-dump`: what this project's table holds, read by the same build that would use
// it, so a table this binary would discard says so rather than printing as though it were in use.
func dumpCacheTable(location projectLocation) error {
	path := cacheTablePath(location.Root)
	waitForTableWriter(path, tableWriterWait)
	table, err := program.ReadCacheTable(path, cacheTableIdentity())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Printf("no cache table for %s (looked at %s)\n", location.Root, path)
			return nil
		}
		fmt.Printf("%v\n", err)
		if !errors.Is(err, program.ErrCacheTablePartlyKept) {
			return nil
		}
	}
	program.DumpCacheTable(os.Stdout, path, table, cacheTableIdentity())
	return nil
}

// cacheTableIdentity is the build a table must have been written by to be read. Anything about the binary
// that can change what a cached answer means is in it, and the keys inside the table cover the rest.
func cacheTableIdentity() program.CacheTableIdentity {
	provenance := release.Current()
	return program.CacheTableIdentity{
		SelfCommit:     provenance.SelfCommit,
		CompilerCommit: provenance.CompilerCommit,
		GoToolchain:    provenance.GoToolchain,
		Platform:       provenance.Platform,
	}
}

// replayRunCache prints a recorded run, framed so its durations cannot be read as this run's, and
// exits with its exit code.
func replayRunCache(stored *program.RunCache) {
	recorded := time.Unix(0, stored.RecordedUnixNanoseconds).Format("15:04:05")
	fmt.Fprintf(os.Stdout,
		"cached: no input has changed since the run at %s, so graph, types and lint did not run; its verdict follows\n",
		recorded)
	os.Stdout.Write(replayLines(stored.Output, recorded))
	os.Stderr.Write(stored.Errors)
	fmt.Fprintf(os.Stdout, "phases: replayed the run at %s · fix, types and lint did not run\n", recorded)
	fmt.Fprintf(os.Stdout, "  this run: %s, after checking %d inputs\n", round(time.Since(processStart)), len(stored.Inputs))
	fmt.Fprintf(os.Stdout, "  %s\n", activeMemoryPolicy.line())
	exitProcess(stored.ExitCode)
}

// declareRunCacheInputs marks the build as having succeeded, and adds inputs the command reads itself.
func declareRunCacheInputs(files ...string) {
	if activeRunCache == nil {
		return
	}
	activeRunCache.declared = true
	activeRunCache.extraFiles = append(activeRunCache.extraFiles, files...)
}

// declineRunCache records why this run must not be replayed. The first reason stands.
func declineRunCache(reason string) {
	if activeRunCache != nil && activeRunCache.declined == "" {
		activeRunCache.declined = reason
	}
}

// abandonRunCache stops recording without writing anything, for a run that failed as a tool. It
// restores the real streams first, so the error about to be printed reaches the terminal.
func abandonRunCache() {
	session := activeRunCache
	if session == nil {
		return
	}
	activeRunCache = nil
	session.stdout.stop(&os.Stdout)
	session.stderr.stop(&os.Stderr)
}

// finishRunCache ends a run whose exit is a verdict about the tree: it records the run if it may, then
// exits with the code. Every verdict exit goes through here, or the cache would record a run with the
// wrong exit code, and a failing tree replayed as exit 0 passes CI while printing a failure.
func finishRunCache(exitCode int) {
	session := activeRunCache
	if session != nil {
		activeRunCache = nil
		session.stdout.stop(&os.Stdout)
		session.stderr.stop(&os.Stderr)
		// The report is out, so the caller can have its answer while the run is recorded. See sendVerdict.
		// The table's lock is taken first and held until the table is in place, so a run the caller starts
		// next waits for this write rather than reading around it. See table_lock.go.
		release := holdTableLock(session.tablePath)
		sendVerdict(exitCode)
		var recorded *program.RunCache
		if session.declared && session.declined == "" {
			recorded = session.record(exitCode)
		}
		session.write(recorded)
		release()
	}
	exitProcess(exitCode)
}

// record captures this run, or returns nil when it cannot.
func (session *runCacheSession) record(exitCode int) *program.RunCache {
	present, absent := session.recorder.Inputs()
	files := append(present, session.extraFiles...)
	cache, err := program.RecordRunCache(session.key, files, nil, absent,
		session.stdout.buffer.Bytes(), exitCode)
	if err != nil {
		// Not recording is always safe. Said on stderr because a cache that silently never records is
		// a saving that quietly never appears.
		session.note(fmt.Sprintf("the run cache did not record this run: %v", firstLine(err.Error())))
		return nil
	}
	cache.Errors = session.stderr.buffer.Bytes()
	cache.RecordedUnixNanoseconds = time.Now().UnixNano()
	return cache
}

// write puts this run's record and findings into the table, once.
//
// The table is read again here when anything has replaced it since the run began, because another
// invocation can have recorded into it: a bare run and `--no-fix` started together each own their own entry,
// and writing back the copy read seconds ago would erase the other's. Every writer renames a new file into
// place, which moves the size or the time, so an unchanged signature means the copy read at the start is
// what is on disk, and decoding the same bytes again would only cost the time (about 13ms on ahra,
// #a66sfmh). What is lost to a race is a record, which costs a miss; nothing here can make a stale entry
// match, since every entry carries its own proof.
//
// The findings are saved even when the run itself was declined. Their entries are keyed on each file's
// bytes, so a file the fix phase rewrote left an entry for bytes that no longer exist, which can never
// match.
func (session *runCacheSession) write(recorded *program.RunCache) {
	if recorded == nil && session.findings == nil && session.shapes == nil {
		return
	}
	identity := cacheTableIdentity()
	table := session.table
	if current := signatureOfFile(session.tablePath); !current.exists || current != session.tableSignature {
		table, _ = program.ReadCacheTable(session.tablePath, identity)
	}
	if recorded != nil {
		table.Runs[session.invocation] = recorded
	}
	if session.findings != nil {
		table.Findings = session.findings.Recorded()
	}
	if session.shapes != nil {
		table.Signatures = session.shapes
	}
	if err := program.WriteCacheTable(session.tablePath, table, identity); err != nil {
		session.note(fmt.Sprintf("the cache table could not be written: %v", firstLine(err.Error())))
	}
}

// fileSignature is what a stat says about a file: whether it is there, its size and its modification time.
type fileSignature struct {
	exists              bool
	size                int64
	modifiedNanoseconds int64
}

func signatureOfFile(path string) fileSignature {
	information, err := os.Stat(path)
	if err != nil {
		return fileSignature{}
	}
	return fileSignature{exists: true, size: information.Size(), modifiedNanoseconds: information.ModTime().UnixNano()}
}

// note says something about recording this run. Before the caller has its verdict it goes to stderr, as
// it always did; after, the caller has moved on and its terminal may hold someone else's prompt, so it is
// kept beside the table for the next run to say instead (printPreviousNotes).
func (session *runCacheSession) note(text string) {
	cacheNote(session.tablePath, text)
}

// cacheNote says something about the cache beside tablePath: on stderr while the caller is still waiting,
// and to the next run once it has its verdict.
func cacheNote(tablePath string, text string) {
	if !verdictSent {
		fmt.Fprintf(os.Stderr, "note: %s\n", text)
		return
	}
	file, err := os.OpenFile(previousNotesPath(tablePath), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	fmt.Fprintln(file, text)
	file.Close()
}

// previousNotesPath is where a run that answered its caller early keeps what it had to say afterward.
func previousNotesPath(tablePath string) string {
	return filepath.Join(filepath.Dir(tablePath), "notes-after-return.txt")
}

// printPreviousNotes says, once, what the last run recorded after it had already returned, and clears it.
// A cache write that failed in the background otherwise fails silently forever, the saving quietly gone.
func printPreviousNotes(tablePath string) {
	path := previousNotesPath(tablePath)
	contents, err := os.ReadFile(path)
	if err != nil {
		return
	}
	os.Remove(path)
	for line := range strings.SplitSeq(strings.TrimSpace(string(contents)), "\n") {
		if line != "" {
			fmt.Fprintf(os.Stderr, "note: after the last run returned, %s\n", line)
		}
	}
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

// invocationTag marks a line that is true of this invocation and false of any replay of it.
//
// A recorded report says "graph built in 436ms" and "types ran in 409ms". Replayed, those lines claim
// a graph was built that was not, and durations this run did not spend. So each line describing the
// invocation rather than the tree is written through invocationOutput, which tags it; the tee strips
// the tag before the terminal and keeps the line out of what is replayed. The tag is a record
// separator, which no line of cohere's output contains.
var invocationTag = []byte("\x1ecohere-invocation\x1e")

// invocationOutput returns where to write a line that describes this invocation rather than the tree.
// Without a recording it is out itself, so a run that is not recorded prints exactly what it always did.
func invocationOutput(out io.Writer) io.Writer {
	if activeRunCache == nil {
		return out
	}
	return &taggingWriter{out: out, tag: invocationTag, atLineStart: true}
}

// taggingWriter puts its tag at the start of every line it writes.
type taggingWriter struct {
	out         io.Writer
	tag         []byte
	atLineStart bool
}

func (w *taggingWriter) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		if w.atLineStart {
			if _, err := w.out.Write(w.tag); err != nil {
				return written, err
			}
			w.atLineStart = false
		}
		end := bytes.IndexByte(data, '\n')
		chunk := data
		if end >= 0 {
			chunk = data[:end+1]
			w.atLineStart = true
		}
		n, err := w.out.Write(chunk)
		written += n
		if err != nil {
			return written, err
		}
		data = data[len(chunk):]
	}
	return written, nil
}

// provenanceTag marks a line that is true of the tree but was produced by a phase, so a replay prints it
// with where it came from.
//
// "fix: 0 of 5 files rewritten, 5 not formatted" is true of an unchanged tree and its not-formatted count
// is actionable, so a replay keeps it. Printed bare it would claim the fix phase ran on a run where
// nothing ran, so it is replayed as "fix (from the cached run at 03:41): ...". Unlike invocationTag the
// line stays in the recording, tagged, so the replay can find it without matching on its text.
var provenanceTag = []byte("\x1ecohere-provenance\x1e")

// provenanceOutput returns where to write such a line. Without a recording it is out itself.
func provenanceOutput(out io.Writer) io.Writer {
	if activeRunCache == nil {
		return out
	}
	return &taggingWriter{out: out, tag: provenanceTag, atLineStart: true}
}

// replayLines is a recording made printable: each provenance-tagged line "label: rest" becomes
// "label (from the cached run at recorded): rest", and no tag survives.
func replayLines(output []byte, recorded string) []byte {
	var replayed bytes.Buffer
	for _, line := range bytes.SplitAfter(output, []byte("\n")) {
		rest, tagged := bytes.CutPrefix(line, provenanceTag)
		if !tagged {
			replayed.Write(line)
			continue
		}
		label, detail, found := bytes.Cut(rest, []byte(": "))
		if !found {
			// A line with no label still must not claim to be current, so the source goes in front.
			fmt.Fprintf(&replayed, "(from the cached run at %s) %s", recorded, rest)
			continue
		}
		fmt.Fprintf(&replayed, "%s (from the cached run at %s): %s", label, recorded, detail)
	}
	return replayed.Bytes()
}

// attachFindingsCache gives this run's graph the findings cache, the run cache's second layer: when
// the run as a whole cannot be replayed, files whose bytes are unchanged replay their cacheable rules'
// findings and coverage, and only their uncacheable rules are walked. See program.FindingsReuse.
//
// Only a run being recorded gets one, so it is exactly as narrow as the run cache.
func attachFindingsCache(graph *program.Graph, location projectLocation) {
	session := activeRunCache
	if session == nil || graph == nil {
		return
	}
	key, err := findingsCacheKey(graph, location)
	if err != nil {
		// Without a key nothing can be proven unchanged, so nothing is replayed or recorded.
		return
	}
	session.findings = program.NewFindingsReuse(key, session.table.Findings)

	// Shapes cost a declaration emit for each file whose bytes changed, so they are computed only when some
	// rule is keyed on them. They start from the table's, then from the compiler's own buildinfo, so only
	// what changed since either is computed.
	if anyRuleKeyedOnShapes(registry.All()) {
		shapes, _ := graph.Signatures(context.Background(), graph.SeedSignatures(session.table.Signatures))
		graph.Shapes = shapes
		session.shapes = shapes
	}
	graph.FindingsReuse = session.findings
}

// anyRuleKeyedOnShapes reports whether any rule declares rule.TypeReachShapes.
func anyRuleKeyedOnShapes(rules []rule.Rule) bool {
	for _, subject := range rules {
		if subject.TypeReach == rule.TypeReachShapes {
			return true
		}
	}
	return false
}

// findingsCacheKey covers everything a cacheable rule's answer depends on beyond a file's own bytes:
// the binary, the project root, the bytes of every file in the lint config's and the tsconfig's extends chains,
// and the rule set in order. Any change to one makes every entry a miss at once.
//
// The tsconfig chain is in it because it decides how a file parses, and a cacheable rule walks the
// parse. Rule options come from the lint config and the root, both already here.
func findingsCacheKey(graph *program.Graph, location projectLocation) ([sha256.Size]byte, error) {
	facts := []string{}
	fileFact := func(label string, path string) error {
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(contents)
		facts = append(facts, fmt.Sprintf("%s %s=%x", label, path, sum))
		return nil
	}
	// Every file in the lint config's extends chain, as the tsconfig's chain is below: a base edited
	// alone changes which rules run and with what options.
	lintConfigFiles, err := configuration.SourcesOf(location.LintConfigFileName)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	for _, lintConfigFile := range lintConfigFiles {
		if err := fileFact("lint-config", lintConfigFile); err != nil {
			return [sha256.Size]byte{}, err
		}
	}
	if err := fileFact("tsconfig", location.ConfigFileName); err != nil {
		return [sha256.Size]byte{}, err
	}
	if graph.Config != nil {
		for _, extended := range graph.Config.ExtendedSourceFiles() {
			if err := fileFact("extends", extended); err != nil {
				return [sha256.Size]byte{}, err
			}
		}
	}
	names := make([]string, 0, len(registry.All()))
	for _, registered := range registry.All() {
		names = append(names, registered.Name)
	}
	facts = append(facts, "rules="+strings.Join(names, "\x00"))

	key, err := program.RunCacheKey(nil, location.Root, facts...)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256([]byte(key)), nil
}
