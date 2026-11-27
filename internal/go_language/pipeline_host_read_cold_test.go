package go_language

import (
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_language/contract"
)

// storedFileState is what a file of a store holds and when it was last written, so a run that leaves the store as it
// was is told from one that writes the same bytes again.
type storedFileState struct {
	size     int64
	modified time.Time
	digest   [sha256.Size]byte
}

// storedFileStates are the regular files below directory, by path, each with its size, modification time and digest.
func storedFileStates(test *testing.T, directory string) map[string]storedFileState {
	test.Helper()
	states := map[string]storedFileState{}
	walkError := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil || !entry.Type().IsRegular() {
			return walkError
		}
		information, informationError := entry.Info()
		if informationError != nil {
			return informationError
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		states[path] = storedFileState{size: information.Size(), modified: information.ModTime(),
			digest: sha256.Sum256(contents)}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return states
}

// A run with no extra build to check never reads its store cold (#s8e6z0n), even when the store's packs directory
// holds no pack at all: it has no build whose pack could be missing, and none to keep anything for.
func TestARunWithNoExtraBuildNeverReadsCold(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "clean"))
	if makeError := os.MkdirAll(filepath.Join(projectStoreDirectory(root), "packs"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	_, stream := runOverConfigured(test, root, exitClean, packingLater(test.TempDir()))
	lint := lintRecordOf(test, stream)
	if !lint.Cache.On || len(lint.Builds) != 0 {
		test.Fatalf("want a run of one build with its store on, got the cache %+v over %d builds", lint.Cache,
			len(lint.Builds))
	}
	if lint.Cache.HostReadCold != "" {
		test.Errorf("a run with no extra build read its store cold, for %q", lint.Cache.HostReadCold)
	}
}

// A settled tree keeps the host's hits (#s8e6z0n): once a run checking every build has written each build's pack, the
// next run finds them, so its host reads its store as always and restores from it. The first run, over an empty store,
// read it cold and still filled it, since the store held none of what it wrote.
func TestASettledTreeKeepsTheHostsHits(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	sharing := packingLater(test.TempDir())
	_, cold := runOverConfigured(test, root, exitFindings, sharing)
	if reason := lintRecordOf(test, cold).Cache.HostReadCold; reason != contract.HostReadColdBuildPacksMissing {
		test.Errorf("the first run, every build's pack missing, read cold for %q, want %q", reason,
			contract.HostReadColdBuildPacksMissing)
	}
	_, settled := runOverConfigured(test, root, exitFindings, sharing)
	lint := lintRecordOf(test, settled)
	if lint.Cache.HostReadCold != "" {
		test.Errorf("the run after every build's pack was written read cold, for %q", lint.Cache.HostReadCold)
	}
	if lint.Cache.Hit == 0 || lint.Cache.Missed != 0 {
		test.Errorf("want the settled run's host to restore every package from its store, got %d hit and %d missed",
			lint.Cache.Hit, lint.Cache.Missed)
	}
}

// A fully cold start fills the host's store (#s8e6z0n): over an empty store, a run checking every build finds every
// build's pack missing and reads cold, yet writes each entry its host checked, since the store held none of them, so
// the run after it, its builds' packs written, reads as always and restores its host from that store rather than
// checking every package from source a second time.
func TestAColdStartFillsTheHostsStore(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	sharing := packingLater(test.TempDir())
	_, cold := runOverConfigured(test, root, exitFindings, sharing)
	first := lintRecordOf(test, cold).Cache
	if first.HostReadCold != contract.HostReadColdBuildPacksMissing || first.Written == 0 ||
		first.Written != first.Missed {
		test.Errorf("want the cold start read cold for %q and every miss written, got %q with %d of %d written",
			contract.HostReadColdBuildPacksMissing, first.HostReadCold, first.Written, first.Missed)
	}
	_, second := runOverConfigured(test, root, exitFindings, sharing)
	after := lintRecordOf(test, second).Cache
	if after.HostReadCold != "" || after.Hit == 0 || after.Missed != 0 {
		test.Errorf("want the run after the cold start to read as always and restore its host whole, got %q with %d "+
			"hit and %d missed", after.HostReadCold, after.Hit, after.Missed)
	}
}

// The cliff (#s8e6z0n): a host whose store holds everything, filled by a run of the host's build alone, beside extra
// builds whose stores hold nothing of their own, every pack missing. The host reads cold, which the record says,
// checks every package from source and keeps each for the builds, so each build restores from it rather than check its
// whole graph, and the host writes nothing it held: every file of the store before the run is there after with the same
// bytes, never written again. The files the run adds are the builds', their entries written through and their packs.
func TestTheHostReadsColdWhenEveryBuildsPackIsMissing(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	sharing := packingLater(test.TempDir())
	runOverConfigured(test, root, exitClean, sharing, "--host-build-only")
	store := projectStoreDirectory(root)
	before := storedFileStates(test, store)
	if len(before) == 0 {
		test.Fatal("the run of the host's build alone left its store empty")
	}
	for path := range before {
		if strings.Contains(path, string(filepath.Separator)+"packs"+string(filepath.Separator)) {
			test.Fatalf("the run of the host's build alone wrote a pack, %s", path)
		}
	}
	_, stream := runOverConfigured(test, root, exitFindings, sharing)
	lint := lintRecordOf(test, stream)
	if lint.Cache.HostReadCold != contract.HostReadColdBuildPacksMissing {
		test.Errorf("the host read cold for %q, want %q", lint.Cache.HostReadCold,
			contract.HostReadColdBuildPacksMissing)
	}
	if lint.Cache.Hit != 0 || lint.Cache.Missed == 0 || lint.Cache.Written != 0 {
		test.Errorf("want the host to hit nothing, miss every package and write none, got %d hit, %d missed and "+
			"%d written", lint.Cache.Hit, lint.Cache.Missed, lint.Cache.Written)
	}
	after := storedFileStates(test, store)
	for path, was := range before {
		if is, kept := after[path]; !kept || is != was {
			test.Errorf("the run changed %s, which its host's store held: %+v, then %+v (kept %t)", path, was, is,
				kept)
		}
	}
	if len(lint.Builds) < 2 || lint.Builds[0].EntriesKept == 0 {
		test.Fatalf("want the host to keep entries for its extra builds, got the builds %+v", lint.Builds)
	}
	for _, build := range lint.Builds[1:] {
		if build.RestoredFromHost+build.Untouched == 0 || build.CheckedFromSource >= build.PackagesLoaded {
			test.Errorf("want %s to restore from the host and check less than its %d packages from source, got %d "+
				"restored, %d untouched and %d checked from source", build.Target, build.PackagesLoaded,
				build.RestoredFromHost, build.Untouched, build.CheckedFromSource)
		}
	}
}

// A tree whose every extra build fails to type-check takes the cold read on its first run alone (#s8e6z0n): such a
// build runs no rule and never saves, so without its packs written all the same every later run's host would take it
// for a build that has never run, and read its store cold forever, as a project that builds only on its own platform
// would.
func TestATreeWhoseEveryBuildIsBrokenReadsColdOnce(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "rename-platforms"))
	if breakError := breakUnderWindows(root); breakError != nil {
		test.Fatal(breakError)
	}
	brokenUnderFreebsd := "//go:build freebsd\n\npackage limits\n\n" +
		"// extra is what freebsd adds to a read, mistyped.\nfunc extra() int {\n\treturn maxBytes + \"quarter\"\n}\n"
	freebsdFile := filepath.Join(root, "internal", "limits", "limits_bsd.go")
	if writeError := os.WriteFile(freebsdFile, []byte(brokenUnderFreebsd), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	sharing := packingLater(test.TempDir())
	_, first := runOverConfigured(test, root, exitFindings, sharing)
	lint := lintRecordOf(test, first)
	if lint.Cache.HostReadCold != contract.HostReadColdBuildPacksMissing {
		test.Errorf("the first run read cold for %q, want %q", lint.Cache.HostReadCold,
			contract.HostReadColdBuildPacksMissing)
	}
	// The premise: every extra build of the run found a type error, so none of them ran its rules.
	for _, build := range lint.Builds[1:] {
		if build.AnalyzedPackages != 0 {
			test.Fatalf("want every extra build broken, got %s analyzing %d packages", build.Target,
				build.AnalyzedPackages)
		}
	}
	for run := range 2 {
		_, later := runOverConfigured(test, root, exitFindings, sharing)
		if reason := lintRecordOf(test, later).Cache.HostReadCold; reason != "" {
			test.Errorf("run %d after the first read cold for %q, want its store read as always", run+2, reason)
		}
	}
}
