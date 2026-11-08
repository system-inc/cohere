package go_engine

import (
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/platform"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// buildRun is what one build of the run found, the host's or an extra one's (#72n9mq0), which the merge of findings
// across builds reads: a build has a say on a file only where it compiled the file and analyzed its package, and an
// extra build analyzes only its closure (platform.Closure), so a file of a package it left out takes the host's
// findings as they are.
type buildRun struct {
	build project.Build
	// compiled is every file of ours the build compiles, by path.
	compiled map[string]bool
	// analyzed is the load IDs the build analyzed: every root for the host's build, the closure's members for an
	// extra one.
	analyzed map[string]bool
	// said is every file of ours the build compiled into a package it analyzed, by path: the files it has a say on.
	// A file sits in p and in "p [p.test]" alike, and a closure may analyze one variant and not the other, so this is
	// the join of compiled and analyzed the merge reads, made where the load is known.
	said map[string]bool
	// result is check.Run's over the analyzed packages: their findings before suppression and scope, the rules that
	// ran, and every crash.
	result *check.Result
	// typeErrors are the load's compiler diagnostics for every package of the build, analyzed or not, since a build
	// type-checks the whole workspace and what no digest can see, a call that type-checks on one platform only,
	// shows here.
	typeErrors []load.Diagnostic
	// closure is an extra build's closure (platform.Closure), each member with why it is there, which the lint record
	// counts by reason, and nil for the host's build, which analyzes every root.
	closure []platform.Member
}
