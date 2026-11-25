package go_language

import (
	"maps"
	"slices"

	"github.com/system-inc/cohere/internal/go_language/load"
)

// loadRequest is what differs between cohere-go's loads of the workspace: every one reads the workspace's root and
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
	// patterns, when set, are loaded in place of the workspace's, with no census of ours to account for, for a load of
	// other packages than the workspace's: the standard library's side load under `--build-dependence` (#ss4t5g6).
	patterns []string
}

// loadPackages is every load of the workspace cohere-go makes, the only caller of package load's loads in it (a test
// holds it to that), so each is the same story: every go command started from its start is late (#sktb2y8), a cached
// load takes the run's pool, account and walks (runCaching), and a go.work.sum it ran go list beside unguarded is the
// run's to say (unguardedWorkspaceSums). Every load is one: the first, the long-line wrap's, the rename's and its
// proofs, a lint reload and an extra build's.
func (pipeline *pipeline) loadPackages(request loadRequest) (*load.Loaded, error) {
	// Every go command started from here on is late (#sktb2y8).
	pipeline.described.children.LoadBegins()
	workspace := pipeline.described.workspace
	// The workspace, unless this is the editor's save, which loads its file's package alone (stdinPackage).
	patterns, census := workspace.Patterns(), pipeline.described.readableOurs
	if pipeline.request.stdinPath != "" {
		patterns, census = pipeline.stdinPackage()
	}
	if request.patterns != nil {
		patterns, census = request.patterns, nil
	}
	var loaded *load.Loaded
	var loadError error
	switch {
	case request.rootSyntaxOnly:
		loaded, loadError = load.PackagesWithRootSyntax(workspace.Root, workspace.Patterns(), request.environment,
			pipeline.described.readableOurs, request.overlay)
	case request.withOverlay:
		loaded, loadError = load.PackagesWithOverlay(workspace.Root, patterns, request.environment, census,
			request.overlay)
	case request.caching != nil:
		loaded, loadError = load.PackagesCached(workspace.Root, patterns, request.environment, census,
			pipeline.runCaching(*request.caching))
	default:
		loaded, loadError = load.Packages(workspace.Root, patterns, request.environment, census)
	}
	if loaded != nil && loaded.UnguardedWorkspaceSum != "" {
		pipeline.unguardedSumsLock.Lock()
		if pipeline.unguardedSums == nil {
			pipeline.unguardedSums = map[string]bool{}
		}
		pipeline.unguardedSums[loaded.UnguardedWorkspaceSum] = true
		pipeline.unguardedSumsLock.Unlock()
	}
	return loaded, loadError
}

// unguardedWorkspaceSums are the go.work.sum files any load of the run ran go list beside with no side copy of the
// go.work, sorted, nil for none, for the summary.
func (pipeline *pipeline) unguardedWorkspaceSums() []string {
	pipeline.unguardedSumsLock.Lock()
	defer pipeline.unguardedSumsLock.Unlock()
	if len(pipeline.unguardedSums) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(pipeline.unguardedSums))
}

// runCaching is caching with what every cached load of the run shares set on it.
func (pipeline *pipeline) runCaching(caching load.Caching) load.Caching {
	// Every cached load of the run, the host's and every extra build's, takes its slots from the run's one pool,
	// records the go commands it starts in the run's account, and names the directories of its roots from the run's
	// walks of them.
	caching.Pool = pipeline.workPool()
	caching.Children = pipeline.described.children
	caching.ModuleRootWalks = pipeline.moduleRootWalks
	// Each load collects its inputs beside it only in a run whose findings cache reads a store, the run's own fact
	// (withFindingsStore, the host's session), the same for every load of the run: warm, beside wins, and cold, under
	// `--no-cache`, every core is busy to the end and inline wins (load.Caching.InputsBeside).
	caching.InputsBeside = pipeline.withFindingsStore()
	return caching
}
