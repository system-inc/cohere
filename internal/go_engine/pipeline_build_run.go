package go_engine

import (
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// buildRun is what one build of the run found, the host's or an extra one's (#72n9mq0), which the merge of findings
// across builds reads: a build has a say on a file only where it compiled the file and analyzed its package, and every
// build analyzes every package it loaded, so a file a build does not compile takes the others' findings as they are.
type buildRun struct {
	build project.Build
	// compiled is every file of ours the build compiles, by path.
	compiled map[string]bool
	// analyzed is the load IDs the build analyzed: every package its load holds, none for an extra build whose load
	// found a type error.
	analyzed map[string]bool
	// said is every file of ours the build compiled into a package it analyzed, by path: the files it has a say on,
	// which the merge reads, made where the load is known.
	said map[string]bool
	// result is check.Run's over the analyzed packages: their findings before suppression and scope, the rules that
	// ran, and every crash.
	result *check.Result
	// typeErrors are the load's compiler diagnostics for every package of the build, since a build type-checks the
	// whole workspace, and a call that type-checks on one platform only shows here.
	typeErrors []load.Diagnostic
	// inputs are what the build's load read to decide what it returned (load.Inputs), the host's load's for the host's
	// build, so a run's replay key can stand for every build it checked.
	inputs load.Inputs
	// files are the digests of every file the build's findings cache keyed, those the host's gave an extra build's
	// included, and none for a build without one, kept so a test reads which files each build's keys hashed.
	files cache.FileDigests
	// reads are the host's reads of other packages, whole (wholeReads), on the host's run alone, which an extra build
	// restores a package by (#b2t9xmf).
	reads map[*packages.Package]*build_reads.Reads
	// loading and linting are the build's wall time loading the workspace and running the rules over it, over every
	// load and pass, its reloads' included.
	loading time.Duration
	linting time.Duration
	// waited is the host's alone: how long the extra builds waited, after its lint, for its walks of what each package
	// read of others to end (wholeReads), before any could start.
	waited time.Duration
	// checkedFromSource and packagesLoaded are an extra build's alone: the packages of its whole graph its first load
	// checked from source, and every package that load loaded, whose ratio weighs its lint at the gate (lintWeight).
	// lintWait is how long its lint waited for that weight, and lintWaited whether it waited at all, so a wait under a
	// millisecond still reads as one.
	checkedFromSource int
	packagesLoaded    int
	lintWait          time.Duration
	lintWaited        bool
	// heldAtLoadEnd and heldAtLintPeak are an extra build's alone, measured only under `--timing`: what it held beyond
	// the host's live heap as its load ended, and the most it held while its rules ran (heldWhileLinting).
	heldAtLoadEnd  uint64
	heldAtLintPeak uint64
}
