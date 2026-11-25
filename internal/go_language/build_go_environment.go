package go_language

import (
	"sync"

	"github.com/system-inc/cohere/internal/go_language/load"
)

// buildGoEnvironment is an extra build's go env as the run starts it before its first load (buildStarts), read with its
// lister state once the goroutine that reads them has ended (answer): a build predicted to fork nothing after its go
// env is waited for before the first load only until its go env has started, and its go env runs on beside the host's
// listing, so the build waits for it where it first reads it (extraBuildRun, #sfjj2rh).
type buildGoEnvironment struct {
	// ended ends once goEnvironment is written, nil where go env failed, its lister state fetched.
	ended         sync.WaitGroup
	goEnvironment *load.GoEnvironment

	// forksNothingAfter is whether the build was predicted, before its go env started, to fork nothing after it: its
	// build list the host's and its build context in the system cache. Written before its go env starts.
	forksNothingAfter bool
}

// answer is the build's go env once read, its lister state fetched, nil where go env failed or for a build the run
// started nothing for.
func (build *buildGoEnvironment) answer() *load.GoEnvironment {
	if build == nil {
		return nil
	}
	build.ended.Wait()
	return build.goEnvironment
}
