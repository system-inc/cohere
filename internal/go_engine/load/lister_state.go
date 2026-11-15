package load

import (
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// listerCommands are the two go commands the lister needs: the build list, and the build context go list reads files
// under.
var listerCommands = [][]string{
	{"list", "-m", "-json", "all"},
	{"list", "-e", "-f", contextTemplate, "unsafe"},
}

// listerState is what listerCommands printed under one GoEnvironment, each command's output and failure by its index,
// fetched once for every load under it (#sktb2y8).
type listerState struct {
	once     sync.Once
	outputs  [][]byte
	failures []error
}

// fetch is what each of listerCommands printed in goEnvironment's workspace under its whole environment, beside its
// guard's copy of the go.work, and how each failed, run at once on the first call, each output shared by every caller
// and only read. What they spend is added to collector, the first call's.
func (state *listerState) fetch(goEnvironment *GoEnvironment, collector *timing.Collector) ([][]byte, []error) {
	state.once.Do(func() {
		state.outputs = make([][]byte, len(listerCommands))
		state.failures = make([]error, len(listerCommands))
		// The two run at once, so their children's CPU is read once around both, which a read around each would count
		// twice.
		measureGoCommands(collector, func() {
			var group sync.WaitGroup
			for index, arguments := range listerCommands {
				group.Go(func() {
					measureListing(collector, func() {
						state.outputs[index], state.failures[index] = goOutput(goEnvironment.guard,
							goEnvironment.workspace, goEnvironment.fullEnvironment, arguments)
					})
				})
			}
			group.Wait()
		})
	})
	return state.outputs, state.failures
}
