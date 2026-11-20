package go_engine

import (
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// describedProject is what the run looks at, read once before the first record, since each part of it can
// refuse the run and a refusal must come before any record.
type describedProject struct {
	workspace project.Workspace

	// census is the workspace's census, which the builds a run of every build checks are read from (project.BuildsFor).
	census project.Census

	// readableOurs are the census's files that are ours and could be read, before the build constraints narrow
	// them: what the load accounts for. An unreadable file is left out, since the load
	// reads each file it accounts for and one it cannot open fails it whole; the file is already a record.
	readableOurs []string

	// goEnvironment is go env's answer the census read the build constraints by, which every load of the run takes
	// in place of asking again, nil when the census judged no file (#f53yra8).
	goEnvironment *load.GoEnvironment

	// children is the run's account of the go commands its loads start, and of those started once its first load has
	// begun, for the lint record (#sktb2y8). It is made here, since the census asks the run's first go env, and every
	// go list under that answer is recorded in it. Nil, as a test's hand-made project has it, records nothing.
	children *load.Children

	// started is the go commands describeProject started beside its census and its reads that may still run (see
	// earlyStart), which the pipeline joins before its first load (awaitStarted). Nil, as a test's hand-made project
	// has it, started none.
	started *earlyStart

	// systemCache is the run's system cache, opened before the go commands describeProject starts, which keep each
	// build's build context in it, and handed to the pipeline (newPipeline), with shardStamps, the run's stamps of its
	// stores' shard directories, which its store holds. Nil, as a test's hand-made project has it, for the pipeline to
	// open its own.
	systemCache *systemCache
	shardStamps *cache.ShardStamps

	// builds are what a run of every build checks, the host's first, read from the census before the first record
	// (readBuilds), so the project record names a file only another build compiles as checked by it, and the lint
	// phase checks these and reads them no second time (otherBuilds). Nil under `--host-build-only`, and when the
	// census judged no file. buildsError is why they could not be read, which the lint phase notes, leaving the run
	// incomplete, and leaves every file the host's build constraints exclude a gap.
	builds      []project.Build
	buildsError error

	// unbuiltReasons are why each file no build compiles is left so, by path, from the build selection (readBuilds),
	// which the project record names in place of the host's constraint reason (#ntamp9e).
	unbuiltReasons map[string]string

	// manifest is the root's go.work, or its go.mod for a module root: where a diagnostic that names no file
	// is placed, since the front door refuses a finding with no position.
	manifest string

	// record is the project record.
	record contract.ProjectRecord

	// inScope are the files that are ours, not excluded, and inside the paths, sorted; unreadable ones too.
	inScope    []string
	inScopeSet map[string]bool

	// constrainedInScope are the readable files that are ours and inside the paths but that the build constraints
	// leave out on this platform: no phase here reads them, and a rename proven under the platform that builds one
	// may write it.
	constrainedInScope map[string]bool

	// contents are the in-scope files that could be read, as they were read, the build constraints' too
	// (constrainedInScope), for the other builds that compile them. A writer replaces a file's bytes here and never
	// writes into the ones held: the run's suppression indexes (suppressionIndexes) take the same slice to mean the
	// same bytes, so a write into one in place would leave a stale index and a directive covering the wrong line.
	contents map[string][]byte

	// unreadable are the in-scope files that could not be read, each written before the first phase.
	unreadable []contract.UnreadableRecord
}

// awaitStarted joins every go command describeProject started beside its census and its reads (#q9ezwvw), so none of
// them starts after the run's first load begins, and none outlives the run.
func (described *describedProject) awaitStarted() {
	if described.started != nil {
		described.started.wait()
	}
}

// openedSystemCache is the run's system cache and the shard stamps its store holds: the ones describeProject opened,
// or new ones for a test's project, nil or made by hand, which opened none.
func (described *describedProject) openedSystemCache(request invocation) (*systemCache, *cache.ShardStamps) {
	if described != nil && described.systemCache != nil {
		return described.systemCache, described.shardStamps
	}
	shardStamps := cache.NewShardStamps(nil)
	return openSystemCache(request, shardStamps), shardStamps
}

// readable are the in-scope files that could be read, in order: the files a phase that reads source is given.
func (described *describedProject) readable() []string {
	files := make([]string, 0, len(described.contents))
	for _, file := range described.inScope {
		if _, read := described.contents[file]; read {
			files = append(files, file)
		}
	}
	return files
}

// hasFileInScope reports whether a package to analyze holds a file in scope, under this build or another
// (constrainedInScope), which is what makes it one the rules analyze. A package with none, every file of it ignored
// by git or outside the paths, is loaded for what imports it and its facts, and never linted, so a file the project
// record names as excluded is never walked or counted as checked; its findings were dropped already.
func (described *describedProject) hasFileInScope(analyzed *packages.Package) bool {
	return slices.ContainsFunc(analyzed.GoFiles, func(file string) bool {
		return described.inScopeSet[file] || described.constrainedInScope[file]
	})
}

// countedAsWalked reports whether the lint record counts a file the rules were given as walked: one in scope, or one
// that is not the project's at all, as a cgo file's translation in the build cache, which stands for the file it was
// translated from. A package with a file in scope can hold files of the project out of it, one git ignores or one
// outside the paths, which the rules still read as the package's and which the run did not check.
func (described *describedProject) countedAsWalked(file string) bool {
	// The census's files are sorted (project.TakeCensus).
	_, ofProject := slices.BinarySearch(described.census.Files, file)
	return described.inScopeSet[file] || !ofProject
}

// container is what the root is called in the run's words: a workspace when it holds a go.work, and otherwise a
// module.
func (described *describedProject) container() string {
	if described.workspace.FromWorkspace {
		return "workspace"
	}
	return "module"
}

// scopeWords says what the fix phase formatted, for its record's formatScope.
func (described *describedProject) scopeWords() string {
	if described.record.ScopeDescription != "" {
		return described.record.ScopeDescription
	}
	if described.workspace.FromWorkspace {
		return "every Go file in the workspace"
	}
	return "every Go file in the module"
}
