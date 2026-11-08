package go_engine

import (
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
}
