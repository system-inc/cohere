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
// build's pack of the project's store is missing (buildPacksMissing): a build with no pack has read nothing of its own
// store, so it restores what it can from what the host keeps, and a host that restores from its store keeps nearly
// nothing. Read cold, the host checks every package from source and keeps each for the builds, which then restore from
// it as on a cold run.
func (pipeline *pipeline) hostReadColdReason() string {
	// The vacuous case: a run with no extra build to check keeps nothing for one, so its host reads as always, whatever
	// the store's packs.
	if !pipeline.keepsForBuilds() {
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

// buildPacksMissing reports whether every extra build's pack of the project's store is missing, each by a stat, never a
// read (cache.Store.HasPack). Each pack is named as the build's session names it (check.ProjectPackPlace), by the
// context lintCache gives that session (projectCacheContext) under the build's own go env, read here as the build's run
// reads it, once it has answered (buildGoEnvironment.answer), under each loader the build may load under: which one is
// known only once its own listing has run, after this, so a pack under either is the build's. A pack that is there but
// damaged is not missing. Nothing unknown reads as missing: a build whose go env failed, cohere-go's identity unread,
// or a store holding no shard stamps, through which no build keeps a pack (openPacks), each answers false.
func (pipeline *pipeline) buildPacksMissing() bool {
	_, extras, buildsError := pipeline.buildsBeside()
	identity, identityError := cache.ExecutableIdentity()
	if buildsError != nil || identityError != nil || pipeline.shardStamps == nil {
		return false
	}
	// The store each build's session opens (lintCache): the project's, at the same root, under the run's stamps.
	store := cache.Open(pipeline.described.workspace.Root, pipeline.shardStamps)
	enabled, _ := pipeline.enabledRules()
	for _, extra := range extras {
		goEnvironment := pipeline.buildGoEnvironments[extra.String()].answer()
		if goEnvironment == nil {
			return false
		}
		values := goEnvironment.Values()
		context := projectCacheContext(identity, values, enabled)
		// Missing under every loader the build could run, since its loader is known only after its own listing.
		for _, loader := range []string{load.LoaderSource, load.LoaderCgoTranslated} {
			if store.HasPack(check.ProjectPackPlace(context, cgoToolchain(values), loader)) {
				return false
			}
		}
	}
	return true
}
