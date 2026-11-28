package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/command_line/streams"
)

// runnerCohereCacheVariable is Loom's runner's name for the folder it keeps cohere's own cache in across integrate jobs
// on Shackler (#0g9pbgt): `LOOM_CACHE_<NAME>_SHARED` for the cache it names `cohere`, as runnerSharedCacheVariable is
// the Go cache's and release's SwiftEngineSharedVariable cohere-swift's binaries'. The self-check runs cohere at the
// laid tree's root, which keeps its cache at `<tree>/.cache/cohere` (cache_directory.Of), and the lay cleans the tree,
// so with none declared every self-check starts cold: about 229 s a job, where a Mac measured 222 s cold, most of it
// building cohere-swift into the cache's swift-build, and 1 s warm. Keeping it is safe: two builds of cohere from the
// same source are the same bytes, and the run cache keys on the binary, so a change to cohere's own code runs cold.
const runnerCohereCacheVariable = "LOOM_CACHE_COHERE_SHARED"

// linkCohereCache lays the cohere cache the self-check runs on, just before it runs, and says which it is. With
// runnerCohereCacheVariable naming a folder, `<tree>/.cache/cohere` is made a link to it, the folder made when
// missing, so the self-check reads and writes what earlier integrations left there. A link already there, an earlier
// job's, is laid anew, which removes the link alone and never what it names; anything else there is the tree's own,
// committed by what is judged, and refused rather than replaced. Unset or empty, nothing is made, and the self-check
// runs on the tree's own cache, empty since the lay cleaned it. It returns the exit code.
func (job *integrateJobRun) linkCohereCache() int {
	place := cache_directory.Of(job.tree)
	declared := strings.TrimSpace(os.Getenv(runnerCohereCacheVariable))
	if declared == "" {
		job.say("cohere cache the tree's own, %s, emptied by the lay, so the self-check runs cold", place)
		return 0
	}
	if !filepath.IsAbs(declared) {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s=%q is no folder's whole path, so the self-check has no cohere "+
			"cache to keep\n", runnerCohereCacheVariable, declared)
		return 1
	}
	information, statError := os.Lstat(place)
	switch {
	case statError == nil && information.Mode()&fs.ModeSymlink != 0:
		if removeError := os.Remove(place); removeError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: removing an earlier job's link at %s: %v\n", place, removeError)
			return 1
		}
	case statError == nil:
		fmt.Fprintf(streams.Errors, "cohere-dev: %s is the tree's own, not a link, so the self-check won't replace it "+
			"with the runner's cohere cache %s (%s)\n", place, declared, runnerCohereCacheVariable)
		return 1
	case !errors.Is(statError, fs.ErrNotExist):
		fmt.Fprintf(streams.Errors, "cohere-dev: reading %s before linking the runner's cohere cache: %v\n", place,
			statError)
		return 1
	}
	for _, directory := range []string{declared, filepath.Dir(place)} {
		if makeError := os.MkdirAll(directory, 0o755); makeError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: making %s for the self-check's cohere cache: %v\n", directory,
				makeError)
			return 1
		}
	}
	if linkError := os.Symlink(declared, place); linkError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: linking %s to the runner's cohere cache %s: %v\n", place, declared,
			linkError)
		return 1
	}
	job.say("cohere cache the runner's kept cohere cache, %s, linked at %s and kept between integrations", declared,
		place)
	return 0
}
