package go_language

import (
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/load"
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
// builds whose stores hold nothing of their own, every pack and every alias table missing: the cold read itself says
// the run of the host alone wrote no build's table, since one held would keep the host warm (buildPacksMissing). The
// host reads cold, which the record says, checks every package from source and keeps each for the builds, so each
// build restores from it rather than check its whole graph, and the host writes nothing it held: every file of the
// store before the run is there after with the same bytes, never written again. The files the run adds are the
// builds', their entries written through and their packs.
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

// identifyingAs has a run share the system cache at systemRoot and judge its stamps as packingLater does, under the
// executable identity named, in place of the test binary's (pipeline.identifiesAs).
func identifyingAs(systemRoot string, identity string) func(*pipeline) {
	packing := packingLater(systemRoot)
	return func(pipeline *pipeline) {
		packing(pipeline)
		pipeline.identifiesAs = identity
	}
}

// Two executables taking turns over one root keep their hosts' hits (#s8e6z0n): the first pack write of each run
// removes every other executable's packs, so A's run after B's finds every build's pack gone, though its builds'
// entries and alias tables are all still there. The tables tell it its builds have run, so its host reads its store as
// always and restores it whole, rather than read cold on every run while its builds restore from their own warm store.
func TestTwoExecutablesTakingTurnsKeepTheirHostsHits(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	systemRoot := test.TempDir()
	_, first := runOverConfigured(test, root, exitFindings, identifyingAs(systemRoot, "executable A"))
	if reason := lintRecordOf(test, first).Cache.HostReadCold; reason != contract.HostReadColdBuildPacksMissing {
		test.Fatalf("A's first run, over an empty store, read cold for %q, want %q", reason,
			contract.HostReadColdBuildPacksMissing)
	}
	_, second := runOverConfigured(test, root, exitFindings, identifyingAs(systemRoot, "executable B"))
	if reason := lintRecordOf(test, second).Cache.HostReadCold; reason != contract.HostReadColdBuildPacksMissing {
		test.Fatalf("B's first run, its store's entries none of its own, read cold for %q, want %q", reason,
			contract.HostReadColdBuildPacksMissing)
	}
	// The premise: B's run removed every pack of A's.
	identityOfA := cache.PackIdentity("executable A").String()
	for _, pack := range packFiles(test, projectStoreDirectory(root)) {
		if filepath.Base(filepath.Dir(pack)) == identityOfA {
			test.Fatalf("B's run left A's pack %s", pack)
		}
	}
	_, third := runOverConfigured(test, root, exitFindings, identifyingAs(systemRoot, "executable A"))
	after := lintRecordOf(test, third).Cache
	if after.HostReadCold != "" || after.Hit == 0 || after.Missed != 0 {
		test.Errorf("want A's run after B's to read its store as always and restore its host whole, got %q with %d "+
			"hit and %d missed", after.HostReadCold, after.Hit, after.Missed)
	}
}

// A run that asks for the types alone over the cliff's store, the host's warm and no build's pack written, restores
// the host's packages from it (#s8e6z0n): it checks no extra build, so a cold read would only type-check every
// package from source for nothing.
func TestATypesOnlyRunOverTheCliffRestoresItsHost(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	sharing := packingLater(test.TempDir())
	runOverConfigured(test, root, exitClean, sharing, "--host-build-only")
	var typed *load.Loaded
	_, stream := runOverConfigured(test, root, exitClean, func(pipeline *pipeline) {
		sharing(pipeline)
		pipeline.typesLoaded = func(loaded *load.Loaded) { typed = loaded }
	}, "--types")
	if typed == nil || len(typed.Packages) == 0 {
		test.Fatalf("want the types phase to read a load of the host's packages, got %v:\n%s", typed, stream)
	}
	for _, analyzed := range typed.Packages {
		if analyzed.TypesInfo != nil {
			test.Errorf("the types-only run checked %s from source, which the host's store holds", analyzed.ID)
		}
	}
}

// The cliff reads as a cold run, and settles (#s8e6z0n). Its stream, after the suite's normalization, is the one a
// fully cold run of the same tree writes: normalize already leaves out the counts that differ by construction, the
// host's written, every entry on the cold run and none on the cliff's, which held them all, and the system cache's
// counts, both runs reading one warmed by a run of another copy first so each reads it alike. Hit, the host's 0 on
// both, and hostReadCold, read cold on both, stay. The builds' stores hold as many bodies of their own on both: a
// build writing a package through from the cold host names the host's record, which holds its body, as on a cold
// run, rather than hold the body itself. And the run after the cliff reads as always: its host restores every package
// from its store, and each build reads its pack.
func TestTheCliffReadsAsAColdRunAndSettles(test *testing.T) {
	test.Parallel()

	sharing := packingLater(test.TempDir())
	runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "other-builds")), exitFindings, sharing)
	cliff := copyFixture(test, filepath.Join("testdata", "other-builds"))
	runOverConfigured(test, cliff, exitClean, sharing, "--host-build-only")
	_, fromCliff := runOverConfigured(test, cliff, exitFindings, sharing)
	coldRoot := copyFixture(test, filepath.Join("testdata", "other-builds"))
	_, fromCold := runOverConfigured(test, coldRoot, exitFindings, sharing)
	if lintRecordOf(test, fromCliff).Cache.HostReadCold != contract.HostReadColdBuildPacksMissing {
		test.Fatalf("the premise: the cliff's run read cold:\n%s", fromCliff)
	}
	if normalizedCliff, normalizedCold := normalize(fromCliff, cliff), normalize(fromCold, coldRoot); normalizedCliff !=
		normalizedCold {
		test.Errorf("the cliff's stream is not a cold run's:\n%s\nwant\n%s", normalizedCliff, normalizedCold)
	}
	if cliffBodies, coldBodies := len(bodiesIn(test, cliff)), len(bodiesIn(test, coldRoot)); cliffBodies != coldBodies {
		test.Errorf("the cliff's store holds %d bodies, a cold run's %d: its builds hold bodies the host's "+
			"records hold", cliffBodies, coldBodies)
	}
	_, settled := runOverConfigured(test, cliff, exitFindings, sharing)
	lint := lintRecordOf(test, settled)
	if lint.Cache.HostReadCold != "" || lint.Cache.Hit == 0 || lint.Cache.Missed != 0 {
		test.Errorf("want the run after the cliff to read as always and restore its host whole, got %q with %d hit "+
			"and %d missed", lint.Cache.HostReadCold, lint.Cache.Hit, lint.Cache.Missed)
	}
	if len(lint.Builds) < 2 {
		test.Fatalf("want the run after the cliff to check its extra builds, got %+v", lint.Builds)
	}
	for _, build := range lint.Builds[1:] {
		if build.Packs == nil || build.Packs.Read == 0 || build.Packs.Missing != 0 {
			test.Errorf("want %s to read its pack after the cliff, got %+v", build.Target, build.Packs)
		}
	}
}

// Nothing unknown reads as missing (#s8e6z0n): over the cliff's store, every build's pack and table missing, a run
// whose one build's go env the host's run could not read, so the build reads its own as it starts
// (extraBuildRun), never reads cold, since that build's pack cannot be named.
func TestABuildWhoseGoEnvironmentIsUnreadNeverReadsCold(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	sharing := packingLater(test.TempDir())
	runOverConfigured(test, root, exitClean, sharing, "--host-build-only")
	_, stream := runOverConfigured(test, root, exitFindings, func(pipeline *pipeline) {
		sharing(pipeline)
		started := pipeline.described.started
		if started == nil {
			test.Fatal("the description started no build's go env")
		}
		if _, _, buildsError := started.awaitBuilds(); buildsError != nil {
			test.Fatal(buildsError)
		}
		if len(started.buildGoEnvironments) < 2 {
			test.Fatalf("want two extra builds started, got %d", len(started.buildGoEnvironments))
		}
		// A go env that ended with no answer, as one that failed does (buildGoEnvironment.answer), in place of the
		// first build's, which still runs and is joined as the description's.
		started.buildGoEnvironments[0] = &buildGoEnvironment{}
	})
	if reason := lintRecordOf(test, stream).Cache.HostReadCold; reason != "" {
		test.Errorf("a run with a build whose go env was unread read cold, for %q", reason)
	}
}
