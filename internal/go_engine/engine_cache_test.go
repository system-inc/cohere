package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// cacheFieldPattern is the lint record's cache counts, which are all a cached run may change in the stream once the
// elapsed times are normalized.
var cacheFieldPattern = regexp.MustCompile(`,"cache":\{[^}]*\}`)

// cachedRun is one engine run over root with extra after the contract and root, its stream normalized, its stream
// without the cache counts, and its lint record's cache counts.
type cachedRun struct {
	stream        string
	withoutCounts string
	counts        contract.LintCache
}

// runCachedEngine runs the engine over root, in process, and fails the test unless it exits with the vet-finding case's
// exit 1 and writes a lint record.
func runCachedEngine(test *testing.T, root string, extra ...string) cachedRun {
	test.Helper()
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-fix"}, extra...)
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	if exitCode := run(arguments, &standardOutput, &standardError, rules.All()); exitCode != exitFindings {
		test.Fatalf("exit %d, want %d; stderr:\n%s", exitCode, exitFindings, standardError.String())
	}
	if standardError.Len() > 0 {
		test.Fatalf("want nothing on stderr, got:\n%s", standardError.String())
	}
	stream := elapsedPattern.ReplaceAllString(standardOutput.String(), `"elapsedMilliseconds":0`)
	ran := cachedRun{stream: stream, withoutCounts: cacheFieldPattern.ReplaceAllString(stream, "")}
	for line := range strings.Lines(stream) {
		if !strings.HasPrefix(line, `{"kind":"lint"`) {
			continue
		}
		var record struct {
			Cache contract.LintCache `json:"cache"`
		}
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatalf("reading the lint record %s: %v", line, decodeError)
		}
		ran.counts = record.Cache
		return ran
	}
	test.Fatalf("no lint record in\n%s", stream)
	return ran
}

// entriesIn is each entry in root's store by name, with its modification time, so a run that wrote or rewrote one
// shows.
func entriesIn(test *testing.T, root string) map[string]string {
	test.Helper()
	directory := filepath.Join(root, ".cache", "cohere", "go")
	listed, listError := os.ReadDir(directory)
	if os.IsNotExist(listError) {
		return map[string]string{}
	}
	if listError != nil {
		test.Fatalf("listing %s: %v", directory, listError)
	}
	entries := map[string]string{}
	for _, entry := range listed {
		information, informationError := entry.Info()
		if informationError != nil {
			test.Fatalf("reading %s: %v", entry.Name(), informationError)
		}
		entries[entry.Name()] = information.ModTime().String()
	}
	return entries
}

// A warm run writes the cold run's stream but for the cache counts, restoring every package the cold run wrote and
// running nothing on them. After an edit to the project's one file only that package misses, and printf's finding in
// it, which needs the fact fmt.Sprintf has, comes back from fmt's restored facts. `--no-cache` reads nothing and
// writes nothing: it hits nothing, and leaves the store as it was.
func TestTheFindingsCacheChangesNoRecord(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "vet-finding"))
	cold := runCachedEngine(test, root)
	if !cold.counts.On || cold.counts.Hit != 0 || cold.counts.Written == 0 ||
		cold.counts.Written+cold.counts.Unstorable > cold.counts.Missed {
		test.Fatalf("want a cold run that hit nothing and wrote what it missed, got %+v", cold.counts)
	}
	written := entriesIn(test, root)
	if len(written) != cold.counts.Written {
		test.Fatalf("want the %d entries written in the store, found %d", cold.counts.Written, len(written))
	}

	warm := runCachedEngine(test, root)
	if warm.counts.Hit != cold.counts.Written || warm.counts.Missed != cold.counts.Missed-cold.counts.Written {
		test.Fatalf("want every package written cold hit warm and the rest missed, cold %+v, warm %+v", cold.counts,
			warm.counts)
	}
	if warm.withoutCounts != cold.withoutCounts {
		test.Fatalf("warm\n%s\ncold\n%s", warm.withoutCounts, cold.withoutCounts)
	}

	file := filepath.Join(root, "vet.go")
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatalf("reading %s: %v", file, readError)
	}
	if writeError := os.WriteFile(file, append(contents, []byte("\n// Edited, so its package misses.\n")...),
		0o644); writeError != nil {
		test.Fatalf("editing %s: %v", file, writeError)
	}
	edited := runCachedEngine(test, root)
	if edited.counts.Hit != cold.counts.Written-1 || edited.counts.Written != 1 {
		test.Fatalf("want only the edited package missed and written, cold %+v, edited %+v", cold.counts,
			edited.counts)
	}
	if !strings.Contains(edited.stream, `"rule":"cohere-go/printf"`) {
		test.Fatalf("want printf's finding from fmt's restored facts, got\n%s", edited.stream)
	}

	before := entriesIn(test, root)
	uncached := runCachedEngine(test, root, "--no-cache")
	if uncached.counts != (contract.LintCache{}) {
		test.Fatalf("want a run under --no-cache to count nothing, got %+v", uncached.counts)
	}
	if uncached.withoutCounts != edited.withoutCounts {
		test.Fatalf("under --no-cache\n%s\ncached\n%s", uncached.withoutCounts, edited.withoutCounts)
	}
	after := entriesIn(test, root)
	if fmt.Sprint(after) != fmt.Sprint(before) {
		test.Fatalf("want the store untouched under --no-cache, before %v, after %v", before, after)
	}

	fresh := copyFixture(test, filepath.Join("testdata", "vet-finding"))
	runCachedEngine(test, fresh, "--no-cache")
	if _, statError := os.Stat(filepath.Join(fresh, ".cache", "cohere", "go")); !os.IsNotExist(statError) {
		test.Fatalf("want no store made under --no-cache, got %v", statError)
	}
}
