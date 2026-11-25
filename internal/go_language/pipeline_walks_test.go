package go_language

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rules"
)

// walksHanded are the walks each load of the host's was handed as it began, in order, and begun, when not nil, what
// is called as each begins, with how many have, that one counted.
type walksHanded struct {
	walks []*load.PackageWalks
	begun func(loads int)
}

// watching is the prelude watch that keeps the walks each of engine's host loads is handed as it begins. A load begins
// on the pipeline's goroutine, the only one that appends.
func (handed *walksHanded) watching(engine *pipeline) func(step preludeStep) {
	return func(step preludeStep) {
		if step != preludeLoadBegan {
			return
		}
		handed.walks = append(handed.walks, engine.walks)
		if handed.begun != nil {
			handed.begun(len(handed.walks))
		}
	}
}

// buildLoads is how many loads runs' extra builds made, every run after the host's: one each, and one more for each
// reload.
func buildLoads(runs []buildRun) int64 {
	loads := int64(0)
	for _, run := range runs[1:] {
		loads += int64(max(len(run.checkedFromSourceRounds), 1))
	}
	return loads
}

// A run walks each pattern once, in the host's load, and no build walks (#vnb7btx): under `--no-fix` nothing changes
// the bytes the run holds, so the walks the host's load was handed are the ones every extra build is, and the
// fixture's one pattern is asked for by every load of the run and walked by the first to ask. The host's load ends
// before any build starts, so that one walk is the host's.
func TestARunWalksEachPatternOnce(test *testing.T) {
	test.Parallel()
	engine, _ := runOver(test, copyFixture(test, filepath.Join("testdata", "other-builds")), exitFindings, "--no-fix")
	if len(engine.buildRuns) != 3 {
		test.Fatalf("the run checked %d builds, want 3", len(engine.buildRuns))
	}
	loads := int64(engine.hostLoads.Load()) + buildLoads(engine.buildRuns)
	patterns, walked, asked := engine.walks.Walked()
	if patterns != 1 || walked != 1 {
		test.Errorf("the run's walks hold %d patterns, walked %d times, want the module's one, walked once", patterns,
			walked)
	}
	if asked != loads {
		test.Errorf("the run's walks were asked %d times, want once by each of the run's %d loads, the host's "+
			"among them", asked, loads)
	}
}

// The builds take the walks read after the wrap wrote, never the first load's (#vnb7btx): the wrap's write changes the
// bytes the run holds, which starts the walks over, so the load of what it wrote is handed walks of its own, and the
// builds those. As that second load begins the test adds a package directory to the tree, which the first load's
// walk never saw, so each extra build's load names the file in it only when its listing walked after the write, or
// took the walk of a load that did.
func TestTheBuildsTakeTheWalksReadAfterTheWrapWrote(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	file := filepath.Join(root, "internal", "label", "describe.go")
	if writeError := os.WriteFile(file, []byte(describeFile), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	added := filepath.Join(root, "internal", "added", "added.go")
	handed := &walksHanded{begun: func(loads int) {
		if loads != 2 {
			return
		}
		if directoryError := os.Mkdir(filepath.Dir(added), 0o755); directoryError != nil {
			test.Error(directoryError)
		}
		if writeError := os.WriteFile(added, []byte("package added\n"), 0o644); writeError != nil {
			test.Error(writeError)
		}
	}}
	engine, stream := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.watchPrelude = handed.watching(engine)
	}, "--no-cache")
	if !strings.Contains(stream, `"fixedBy":{"cohere-go/consistency-no-long-line":1}`) {
		test.Fatalf("want the wrap to break the long line of %s, got\n%s", file, stream)
	}
	if len(handed.walks) < 2 || len(engine.buildRuns) != 3 {
		test.Fatalf("the host loaded %d times and the run checked %d builds, want a load of what the wrap wrote and "+
			"3 builds", len(handed.walks), len(engine.buildRuns))
	}
	last := handed.walks[len(handed.walks)-1]
	if handed.walks[0] == last || engine.walks != last {
		test.Errorf("the first load was handed the walks %p, the last %p, and the builds read %p, want the last's "+
			"for the builds and others for the first", handed.walks[0], last, engine.walks)
	}
	for _, run := range engine.buildRuns[1:] {
		if !slices.Contains(run.inputs().Files, added) {
			test.Errorf("%s's load does not name %s, so its listing took a walk made before the wrap wrote",
				run.build, added)
		}
	}
	// The host's loads after the write and every build's asked for the one pattern, and the first of them walked it.
	loads := int64(len(handed.walks)-1) + buildLoads(engine.buildRuns)
	if patterns, walked, asked := last.Walked(); patterns != 1 || walked != 1 || asked != loads {
		test.Errorf("the walks read after the wrap wrote hold %d patterns, walked %d times and asked %d, want one, "+
			"walked once and asked by each of the %d loads after the write", patterns, walked, asked, loads)
	}
}

// After a rename wrote, the builds walk for themselves (#vnb7btx): the rename's write starts the walks over, and the
// load of what it wrote shares nothing (runRename), so no load of the host's asks the walks the builds are handed.
// They are not the first load's, the builds' loads alone asked for the pattern, and one of them walked it.
func TestAfterARenameTheBuildsWalkForThemselves(test *testing.T) {
	test.Parallel()
	handed := &walksHanded{}
	engine, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "rename-platforms")), exitClean,
		func(engine *pipeline) { engine.watchPrelude = handed.watching(engine) }, "--no-cache")
	if !engine.renamed || len(engine.buildRuns) < 2 || len(handed.walks) == 0 {
		test.Fatalf("the run renamed %t, checked %d builds and began %d loads through the host's caching, want a "+
			"rename, an extra build and a first load", engine.renamed, len(engine.buildRuns), len(handed.walks))
	}
	if engine.walks == handed.walks[0] {
		test.Errorf("the builds read the walks %p, the first load's, made before the rename wrote", engine.walks)
	}
	loads := buildLoads(engine.buildRuns)
	if patterns, walked, asked := engine.walks.Walked(); patterns != 1 || walked != 1 || asked != loads {
		test.Errorf("the builds' walks hold %d patterns, walked %d times and asked %d, want one, walked once and "+
			"asked by the builds' %d loads alone", patterns, walked, asked, loads)
	}
}

// A host whose listing was kept walks nothing, and the builds walk among themselves (#vnb7btx): a first run of the
// host's build alone keeps the host's listing in the store, so in a second run, of every build, the host's load reads
// that listing and asks for no walk, while each extra build, with no listing kept, lists. The pattern is asked for by
// the builds' loads alone, and one of them walks it: no build waits on a walk the host never made.
func TestAHostWhoseListingWasKeptLeavesTheBuildsToWalk(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	runOverWith(test, root, exitClean, "--no-fix", "--host-build-only")
	engine, _ := runOverWith(test, root, exitFindings, "--no-fix")
	if len(engine.buildRuns) != 3 {
		test.Fatalf("the run checked %d builds, want 3", len(engine.buildRuns))
	}
	loads := buildLoads(engine.buildRuns)
	if patterns, walked, asked := engine.walks.Walked(); patterns != 1 || walked != 1 || asked != loads {
		test.Errorf("the run's walks hold %d patterns, walked %d times and asked %d, want one, walked once and asked "+
			"by the builds' %d loads alone, the host's listing kept", patterns, walked, asked, loads)
	}
}

// treeShape is every directory below root and every go.mod in one, by path, sorted, a directory whose name starts
// with a dot and everything below it left out: what a walk of package directories, a listing's directory entries and
// a module root's walk read of where things are.
func treeShape(test *testing.T, root string) []string {
	test.Helper()
	var shape []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if entry.IsDir() && path != root && strings.HasPrefix(entry.Name(), ".") {
			return filepath.SkipDir
		}
		if entry.IsDir() || entry.Name() == "go.mod" {
			shape = append(shape, path)
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	slices.Sort(shape)
	return shape
}

// A fixing run leaves its directories and module files as they were (#vnb7btx): gofmt's write, the long-line wrap's
// and a rename's each replace existing files' bytes and make, remove or rename no directory and no go.mod, outside
// the dot directories the findings store writes in. That is the invariant the run's shared directory reads, its
// pattern walks and its module root walks rest on, each of which a load before the write may fill (pipeline.reads).
func TestAFixingRunLeavesItsDirectoriesAndModuleFilesAsTheyWere(test *testing.T) {
	test.Parallel()
	for _, fixture := range []struct {
		name    string
		renames bool
	}{
		{name: "unformatted"},
		{name: "long-lines"},
		{name: "rename", renames: true},
		{name: "rename-platforms", renames: true},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			test.Parallel()
			root := copyFixture(test, filepath.Join("testdata", fixture.name))
			before := treeShape(test, root)
			engine, _, _, _ := runOverExiting(test, root, rules.All(), nil)
			if engine.fix == nil || engine.fix.record.FilesRewritten == 0 || engine.renamed != fixture.renames {
				test.Fatalf("the run rewrote no file, or renamed %t where the fixture renames %t, so it holds nothing "+
					"to the invariant", engine.renamed, fixture.renames)
			}
			if after := treeShape(test, root); !slices.Equal(after, before) {
				test.Errorf("the run left the directories and go.mod files\n%s\nwant them as they were\n%s",
					strings.Join(after, "\n"), strings.Join(before, "\n"))
			}
		})
	}
}
