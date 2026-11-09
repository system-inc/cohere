package go_engine

import "github.com/system-inc/cohere/internal/go_engine/load"

// loadRequest is what differs between the engine's loads of the workspace: every one reads the workspace's root and
// patterns, and counts its readable files of ours (loadPackages).
type loadRequest struct {
	environment []string
	// caching, when set, is the load's findings cache and held bytes (load.PackagesCached); a load without it is
	// load.Packages.
	caching *load.Caching
	// overlay, when withOverlay is set, is read in place of the disk (load.PackagesWithOverlay).
	overlay     map[string][]byte
	withOverlay bool
}

// loadPackages is every load of the workspace the engine makes, the only caller of package load's loads in it (a test
// holds it to that), so the memory limit is the same story for each: the last load's late raise is retired, the limit
// goes back to the cap, and this load's raise is armed before it starts, then told its end after it (#44n0vap). Every
// load, the first, the long-line wrap's, the rename's and its proofs, a lint reload and an extra build's, runs under
// the cap, and the raise may lift the limit at most once after each.
func (engine *pipeline) loadPackages(request loadRequest) (*load.Loaded, error) {
	engine.beginLoad()
	workspace := engine.described.workspace
	var loaded *load.Loaded
	var loadError error
	switch {
	case request.withOverlay:
		loaded, loadError = load.PackagesWithOverlay(workspace.Root, workspace.Patterns(), request.environment,
			engine.described.readableOurs, request.overlay)
	case request.caching != nil:
		loaded, loadError = load.PackagesCached(workspace.Root, workspace.Patterns(), request.environment,
			engine.described.readableOurs, *request.caching)
	default:
		loaded, loadError = load.Packages(workspace.Root, workspace.Patterns(), request.environment,
			engine.described.readableOurs)
	}
	engine.lateRaise.endLoad()
	return loaded, loadError
}

// beginLoad is a load's start, for the memory limit: the last load's raise is retired first, so none of its sentinels
// can lift the limit during this load, then armLateRaise sets the limit back to the cap and arms this load's raise.
func (engine *pipeline) beginLoad() {
	engine.lateRaise.retire()
	engine.lateRaise = engine.armLateRaise()
}
