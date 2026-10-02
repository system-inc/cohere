package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/lint/registry"
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
	path     string
	key      string
	recorder *program.InputRecorder

	// scope is the format scope computed for the key, reused by the fix phase so a miss does not ask
	// git the same question twice.
	scope      formatScope
	scopeError error

	// declared is set once the build succeeds and the inputs are known. A session that never declares
	// is never recorded.
	declared   bool
	extraFiles []string

	// declined is the reason this run must not be recorded, empty while it may be.
	declined string

	// findings is the findings cache this run consults and records, the run cache's second layer, and
	// findingsPath where it is kept. Nil when the graph never reached a walk.
	findings     *program.FindingsReuse
	findingsPath string

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
func runCacheEligible() bool {
	switch {
	case len(os.Args) == 1:
	case len(os.Args) == 2 && os.Args[1] == "--no-fix":
	default:
		return false
	}
	// An escape hatch, for measuring a cold run and for anyone who suspects the cache. It has to be
	// reachable without a flag, because any flag already makes the run ineligible.
	return os.Getenv("COHERE_RUN_CACHE") != "off"
}

// beginRunCache replays a recorded run and exits if every input is unchanged, and otherwise starts
// recording this one. It returns the recorder the graph build should report its inputs to, or nil
// when this run is not eligible.
func beginRunCache(location projectLocation) *program.InputRecorder {
	if !runCacheEligible() {
		return nil
	}
	// The format scope is a fact the output depends on that no build input reveals: a commit makes a
	// changed file unchanged and moves no source file's mtime, and a new untracked file outside the
	// program lives in a directory the build never read. So the scope itself is recomputed here with
	// the same function the fix phase calls, and hashed into the key.
	scope, scopeError := changedFilesScope(location.Root)

	key, err := program.RunCacheKey(os.Args[1:], location.Root,
		"root="+location.Root,
		"tsconfig="+location.ConfigFileName,
		"lint-config="+location.LintConfigFileName,
		"scope="+scopeFact(scope, scopeError),
	)
	if err != nil {
		return nil
	}

	path := runCachePath(location.Root, os.Args[1:])
	if stored, err := program.ReadRunCache(path); err == nil && stored.Check(key) == nil {
		replayRunCache(stored)
	}

	session := &runCacheSession{
		path:       path,
		key:        key,
		recorder:   program.NewInputRecorder(),
		scope:      scope,
		scopeError: scopeError,
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

// scopeFact renders a format scope as a fact. Every field that can reach the output is in it.
func scopeFact(scope formatScope, scopeError error) string {
	if scopeError != nil {
		return "error: " + scopeError.Error()
	}
	names := append([]string(nil), scope.FileNames...)
	sort.Strings(names)
	unreadable := append([]string(nil), scope.UnreadableSubmodules...)
	sort.Strings(unreadable)
	return fmt.Sprintf("everything=%v|description=%q|files=%q|unreadable=%q",
		scope.Everything, scope.Description, names, unreadable)
}

// runCachePath keeps the manifest out of the project: one per project root and invocation, in the user
// cache. The invocation is part of the name because a bare run and `--no-fix` print different reports
// under different keys: sharing one file, each overwrote the other's, and alternating between them was
// a miss every time.
func runCachePath(root string, arguments []string) string {
	return cacheFilePath("run", root+"\x00"+strings.Join(arguments, "\x00"))
}

// findingsCachePath is one per project root, shared by every eligible invocation. Each walks the same
// rules over the same files under the same configuration, so their findings are the same findings.
func findingsCachePath(root string) string {
	return cacheFilePath("lint", root)
}

func cacheFilePath(prefix string, identity string) string {
	directory, err := os.UserCacheDir()
	if err != nil {
		directory = os.TempDir()
	}
	sum := sha256.Sum256([]byte(identity))
	return filepath.Join(directory, "cohere", fmt.Sprintf("%s-%x.json", prefix, sum[:8]))
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
	os.Exit(stored.ExitCode)
}

// runCacheScope hands the fix phase the scope the key was built from, when there is one, so a miss
// does not ask git twice. Without a session it asks git itself, exactly as before.
func runCacheScope(root string) (formatScope, error) {
	if activeRunCache != nil && activeRunCache.recorder != nil {
		return activeRunCache.scope, activeRunCache.scopeError
	}
	return changedFilesScope(root)
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
		if session.declared && session.declined == "" {
			session.record(exitCode)
		}
		// Saved even when the run itself was declined. Its entries are keyed on each file's bytes, so a
		// file the fix phase rewrote left an entry for bytes that no longer exist, which can never match.
		if session.findings != nil {
			if err := program.WriteLintCache(session.findingsPath, session.findings.Recorded()); err != nil {
				fmt.Fprintf(os.Stderr, "note: the findings cache could not be written: %v\n", firstLine(err.Error()))
			}
		}
	}
	os.Exit(exitCode)
}

func (session *runCacheSession) record(exitCode int) {
	present, absent := session.recorder.Inputs()
	files := append(present, session.extraFiles...)
	cache, err := program.RecordRunCache(session.key, files, nil, absent,
		session.stdout.buffer.Bytes(), exitCode)
	if err != nil {
		// Not recording is always safe. Said on stderr because a cache that silently never records is
		// a saving that quietly never appears.
		fmt.Fprintf(os.Stderr, "note: the run cache did not record this run: %v\n", firstLine(err.Error()))
		return
	}
	cache.Errors = session.stderr.buffer.Bytes()
	cache.RecordedUnixNanoseconds = time.Now().UnixNano()
	if err := program.WriteRunCache(session.path, cache); err != nil {
		fmt.Fprintf(os.Stderr, "note: the run cache could not be written: %v\n", firstLine(err.Error()))
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
	session.findingsPath = findingsCachePath(location.Root)
	previous, _ := program.ReadLintCache(session.findingsPath)
	session.findings = program.NewFindingsReuse(key, previous)
	graph.FindingsReuse = session.findings
}

// findingsCacheKey covers everything a cacheable rule's answer depends on beyond a file's own bytes:
// the binary, the project root, the lint config's bytes, every file in the tsconfig's extends chain,
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
	if err := fileFact("lint-config", location.LintConfigFileName); err != nil {
		return [sha256.Size]byte{}, err
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
