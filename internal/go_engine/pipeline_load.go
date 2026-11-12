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
	// rootSyntaxOnly keeps syntax and type maps for the roots alone (load.PackagesWithRootSyntax), for the rename's
	// plan and platform loads, which never read a dependency's. It reads overlay, nil for none, and takes no caching.
	rootSyntaxOnly bool
}

// loadPackages is every load of the workspace the engine makes, the only caller of package load's loads in it (a test
// holds it to that), so the memory limit is the same story for each (loadMemory): the last load's late raise is
// retired, the limit goes back to the cap, and this load's raise is armed before it starts, then told its end after
// it (#44n0vap). Every load, the first, the long-line wrap's, the rename's and its proofs, a lint reload and an extra
// build's, runs under the cap, and the raise may lift the limit at most once after each.
func (engine *pipeline) loadPackages(request loadRequest) (*load.Loaded, error) {
	engine.loads.begin()
	workspace := engine.described.workspace
	// The workspace, unless this is the editor's save, which loads its file's package alone (stdinPackage).
	patterns, census := workspace.Patterns(), engine.described.readableOurs
	if engine.request.stdinPath != "" {
		patterns, census = engine.stdinPackage()
	}
	var loaded *load.Loaded
	var loadError error
	switch {
	case request.rootSyntaxOnly:
		loaded, loadError = load.PackagesWithRootSyntax(workspace.Root, workspace.Patterns(), request.environment,
			engine.described.readableOurs, request.overlay)
	case request.withOverlay:
		loaded, loadError = load.PackagesWithOverlay(workspace.Root, patterns, request.environment, census,
			request.overlay)
	case request.caching != nil:
		loaded, loadError = load.PackagesCached(workspace.Root, patterns, request.environment, census,
			engine.watchedCaching(*request.caching))
	default:
		loaded, loadError = load.Packages(workspace.Root, patterns, request.environment, census)
	}
	engine.loads.end()
	return loaded, loadError
}

// watchedCaching is caching as it is, unless a test watches inside its loads (engineMemory.duringLoad): then its held
// reader calls the watch before each lookup, which is during the load.
func (engine *pipeline) watchedCaching(caching load.Caching) load.Caching {
	// Every cached load of the run, the host's and every extra build's, takes its slots from the run's one pool.
	caching.Pool = engine.workPool()
	if engine.memory == nil || engine.memory.duringLoad == nil {
		return caching
	}
	held, during := caching.Held, engine.memory.duringLoad
	caching.Held = func(file string) ([]byte, bool) {
		during()
		if held == nil {
			return nil, false
		}
		return held(file)
	}
	return caching
}
