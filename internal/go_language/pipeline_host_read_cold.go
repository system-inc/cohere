package go_language

import (
	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/load"
)

// hostReadColdReason is why the host's session reads the project's store cold this run (check.CacheSession's
// ReadColdWhen, #s8e6z0n), asked once, at the host's first restore, and empty for a run whose host reads it as always.
// It reads cold when the run checks at least one extra build, every one restoring from the host's run, and every extra
// build is missing from the project's store, its pack and its alias table both (buildPacksMissing): a build with no
// pack has read nothing of its own store, so it restores what it can from what the host keeps, and a host that
// restores from its store keeps nearly nothing. Read cold, the host checks every package from source and keeps each
// for the builds, which then restore from it as on a cold run.
func (pipeline *pipeline) hostReadColdReason() string {
	// The vacuous case: a run with no extra build to check keeps nothing for one, so its host reads as always, whatever
	// the store's packs.
	if !pipeline.keepsForBuilds() {
		return ""
	}
	// Extra builds run only within the lint phase (runLint), so a run that asks for the types alone, or loads only for
	// the long-line wrap, checks none, and a cold host there would type-check every package from source for no build.
	// Such a run reads no builds today (describeProject's readsBuilds), so keepsForBuilds answers false first; this
	// says it here too, where the cold read is decided, rather than lean on how the description reads builds.
	if !pipeline.request.requests(contract.PhaseLint) {
		return ""
	}
	// Asked only where the builds restore from the host's run: builds that restore nothing from it, under a test's
	// keepsHostRestoreOff or the `--build-dependence` facts census, gain nothing from what it keeps, so a cold host
	// there would only be slower. A host with no store reads nothing of one.
	if pipeline.keepsHostRestoreOff || pipeline.censusKeepsHostRestoreOff || !pipeline.session.HasStore() {
		return ""
	}
	if !pipeline.buildPacksMissing() {
		return ""
	}
	return string(contract.HostReadColdBuildPacksMissing)
}

// buildPacksMissing reports whether every extra build is missing from the project's store, each by a stat, never a
// read: its pack under either loader (cache.Store.HasPack), and its alias table (check.BuildAliasTableKey). The pack
// alone would not tell: a run's first pack write removes every other executable's packs (cache.Store's
// RemoveOtherIdentitiesPacks) and leaves their entries, so with two executables over one root, the installed cohere
// and one built in a worktree, each would find every build's pack gone and read cold on every run while its builds
// restore from their own warm store. The table is named by the executable, the build and the rules, is written by
// every build that ever wrote an entry or wrote one through, and is never swept, so a build that has run under this
// executable keeps it. Each is named as the build's session names it (check.ProjectPackPlace,
// check.BuildAliasTableKey), by the context lintCache gives that session (projectCacheContext) under the build's own
// go env, read here as the build's run reads it, once it has answered (buildGoEnvironment.answer), under each loader
// the build may load under: which one is known only once its own listing has run, after this, so a pack under either
// is the build's. A pack that is there but damaged is not missing, nor is a table. Nothing unknown reads as missing: a
// build whose go env failed, cohere-go's identity unread, or a store holding no shard
// stamps, through which no build keeps a pack (openPacks), each answers false.
func (pipeline *pipeline) buildPacksMissing() bool {
	_, extras, buildsError := pipeline.buildsBeside()
	identity, identityError := pipeline.executableIdentity()
	if buildsError != nil || identityError != nil || pipeline.shardStamps == nil {
		return false
	}
	// The rules the settings turn off are named for the lint record alone; no key holds them.
	enabled, _ := pipeline.enabledRules()
	// The store each build's session opens (lintCache): the project's, at the same root, under the run's stamps.
	store := cache.Open(pipeline.described.workspace.Root, pipeline.shardStamps)
	for _, extra := range extras {
		goEnvironment := pipeline.buildGoEnvironments[extra.String()].answer()
		if goEnvironment == nil {
			return false
		}
		values := goEnvironment.Values()
		context := projectCacheContext(identity, values, enabled)
		// A build whose table is there has run under this executable, whatever became of its packs.
		if store.Holds(check.BuildAliasTableKey(context)) {
			return false
		}
		// Missing under every loader the build could run, since its loader is known only after its own listing.
		for _, loader := range []string{load.LoaderSource, load.LoaderCgoTranslated} {
			if store.HasPack(check.ProjectPackPlace(context, cgoToolchain(values), loader)) {
				return false
			}
		}
	}
	return true
}
