package go_language

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/format"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules"
	"github.com/system-inc/cohere/internal/go_language/settings"
)

// Each run's pipeline holds fingerprints of its own (#qqyq61s): what the extra builds share of their kept listings'
// fingerprints is held for one run alone, since any file can move between runs, and newPipeline, made once a run, is
// where that is decided. Two pipelines, as two runs of one process make, never share it.
func TestEachRunsPipelineHoldsFingerprintsOfItsOwn(test *testing.T) {
	test.Parallel()
	first := newPipeline(invocation{}, nil, settings.Settings{}, nil, io.Discard, io.Discard, nil)
	second := newPipeline(invocation{}, nil, settings.Settings{}, nil, io.Discard, io.Discard, nil)
	if first.fingerprints == nil || first.fingerprints == second.fingerprints {
		test.Errorf("two runs' pipelines hold fingerprints %p and %p, want one of its own each", first.fingerprints,
			second.fingerprints)
	}
}

// A change to the bytes the run holds starts its fingerprints over (#t7h2sm4): a fingerprint is of the bytes the run
// held when a load read it, and the host's load and the builds share them, so a file the fix held in memory, or wrote
// and the run read again, must be fingerprinted again by the load after it, never matched against a kept listing by
// the fingerprint of the bytes before it.
func TestAChangeToTheHeldBytesStartsTheFingerprintsOver(test *testing.T) {
	test.Parallel()
	file := filepath.Join(test.TempDir(), "held.go")
	described := &describedProject{contents: map[string][]byte{file: []byte("package held\n")}}
	pipeline := newPipeline(invocation{}, described, settings.Settings{}, nil, io.Discard, io.Discard, nil)

	// A save's fix holds its output in memory.
	before := pipeline.fingerprints
	pipeline.hold(format.ChangedFile{File: file, Output: []byte("package held\n\n// Held is held.\nconst Held = 1\n")})
	if pipeline.fingerprints == nil || pipeline.fingerprints == before {
		test.Errorf("holding a fix's output kept the fingerprints %p, want new ones", before)
	}

	// A run's fix writes the file, and the run reads it again.
	written := []byte("package held\n\n// Written is written.\nconst Written = 2\n")
	if writeError := os.WriteFile(file, written, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	before = pipeline.fingerprints
	pipeline.reread(file)
	// A process that refuses writes, the editor's save's (format.RefuseWrites), reads nothing again and holds the bytes
	// it had, so its fingerprints stand. The refusal is the process's, and no test of this package sets it, so this
	// reads the reread; it is read here rather than assumed, so the test holds in a process that refuses.
	refused := format.WritesRefused()
	if reread := bytes.Equal(pipeline.described.contents[file], written); reread == refused {
		test.Fatalf("with writes refused %t, the run holds %q after the reread", refused,
			pipeline.described.contents[file])
	}
	if changed := pipeline.fingerprints != nil && pipeline.fingerprints != before; changed == refused {
		test.Errorf("with writes refused %t, the reread left the fingerprints %p as %p", refused, before,
			pipeline.fingerprints)
	}
}

// The host's load fills the fingerprints the extra builds read (#t7h2sm4): with the findings cache's store on, a
// first run keeps the host's listing, and in a second run, warm, the host's load checks that listing against every
// directory of the module, each fingerprinted once, into the run's own fingerprints, so the builds after it hash none
// of them again.
func TestTheHostsWarmLoadFillsTheFingerprintsTheBuildsRead(test *testing.T) {
	test.Parallel()
	root := fingerprintsModule(test)
	hostLoad(test, root, "the first run")
	warm, _ := hostLoad(test, root, "the second run")
	directories, computed := warm.fingerprints.DirectoriesFingerprinted()
	if directories != 3 || computed != int64(directories) {
		test.Errorf("the warm host load fingerprinted %d directories %d times, want the module's 3, once each",
			directories, computed)
	}
}

// The findings store's own directory leaves the host's listing kept (#78whs52): on a fresh tree the first run keeps
// its listing, and the store's first write makes `.cache` in the root after that listing's fingerprint was read. go's
// walk never reads a dot directory, so the second run reads the listing it kept, with neither go list nor the
// in-process lister asked again: the second run is the first warm one, not the third.
func TestTheStoresOwnDirectoryLeavesTheHostsListingKept(test *testing.T) {
	test.Parallel()
	root := fingerprintsModule(test)
	storeDirectory := filepath.Join(root, ".cache")
	if _, statError := os.Stat(storeDirectory); !errors.Is(statError, fs.ErrNotExist) {
		test.Fatalf("want a fresh tree without %s, got %v", storeDirectory, statError)
	}
	_, cold := hostLoad(test, root, "the first run")
	if cold.ListingKept {
		test.Fatalf("the first run on a fresh tree read a kept listing, want it listed")
	}
	if information, statError := os.Stat(storeDirectory); statError != nil || !information.IsDir() {
		test.Fatalf("want the first run's store to have made %s, got %v", storeDirectory, statError)
	}
	_, warm := hostLoad(test, root, "the second run")
	if !warm.ListingKept || warm.ListedInProcess || warm.GoListReason != "" {
		test.Errorf("the second run's listing kept %t, listed in process %t, go list's reason %q, want it kept "+
			"and listed by neither", warm.ListingKept, warm.ListedInProcess, warm.GoListReason)
	}
}

// fingerprintsModule is a fresh module in a directory of its own, with three directories the walk reads: the module's
// own, which holds no package, and its two packages'.
func fingerprintsModule(test *testing.T) string {
	test.Helper()
	root := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod":         "module example.com/fingerprints\n\ngo 1.21\n",
		"first/first.go": "package first\n\n// First is one.\nconst First = 1\n",
		"second/second.go": "package second\n\nimport \"example.com/fingerprints/first\"\n\n" +
			"// Second is two.\nconst Second = first.First + 1\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if directoryError := os.MkdirAll(filepath.Dir(path), 0o755); directoryError != nil {
			test.Fatal(directoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return root
}

// hostLoad is one run's host load of root, through a findings cache whose store is the project's, under root, and the
// pipeline it ran in.
func hostLoad(test *testing.T, root string, run string) (*pipeline, *load.Loaded) {
	test.Helper()
	request, parseError := parseInvocation([]string{"--contract", fmt.Sprint(contract.Version), "--root", root})
	if parseError != nil {
		test.Fatal(parseError)
	}
	registered := rules.All()
	ruleSettings, settingsError := loadSettings(request, registered)
	if settingsError != nil {
		test.Fatal(settingsError)
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	// What the description started beside its reads ends before the test and its directory do.
	test.Cleanup(described.awaitStarted)
	pipeline := newPipeline(request, described, ruleSettings, registered, io.Discard, io.Discard, nil)
	isolatePipeline(test, pipeline)
	// The load alone is asked, so no lint starts beside it.
	pipeline.overlaps = false
	pipeline.session = pipeline.cacheSession(pipeline.described.workspace.Environment(),
		pipeline.described.goEnvironment, cache.FileDigests{}, pipeline.standardError)
	if pipeline.session == nil || !pipeline.session.HasStore() {
		test.Fatalf("%s has no findings cache with a store, so its load keeps no listing", run)
	}
	// The first load waits for what the description started, as the pipeline's does (startChildrenFirst).
	described.awaitForked()
	loaded, loadError := pipeline.loadWorkspacePackages()
	if loadError != nil {
		test.Fatalf("%s: %v", run, loadError)
	}
	return pipeline, loaded
}
