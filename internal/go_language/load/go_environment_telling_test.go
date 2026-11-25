package load

import (
	"path/filepath"
	"sync/atomic"
	"testing"
)

// ReadGoEnvironmentTelling tells its caller once, as go env's child has started and before the answer is read, and
// once too when the child cannot start, as in a directory that is not there, so a caller waiting on it never waits
// forever; and the run's account holds no go command running once it returns.
func TestReadingTheGoEnvironmentTellsOnceItsChildHasStarted(test *testing.T) {
	test.Parallel()
	workspace, environment := writeSharedBuildList(test, sharedBuildListFiles)
	for _, each := range []struct {
		name      string
		directory string
		fails     bool
	}{
		{name: "a workspace", directory: workspace},
		{name: "a directory that is not there", directory: filepath.Join(workspace, "gone"), fails: true},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			children := &Children{}
			var told atomic.Int64
			goEnvironment, readError := ReadGoEnvironmentTelling(each.directory, environment, nil, children,
				func() { told.Add(1) })
			if (readError != nil) != each.fails {
				test.Fatalf("reading go env failed: %v, want a failure: %t", readError, each.fails)
			}
			if count := told.Load(); count != 1 {
				test.Errorf("the caller was told %d times, want once", count)
			}
			if !each.fails && goEnvironment == nil {
				test.Error("go env answered nothing")
			}
			if running := children.Running(); running != 0 {
				test.Errorf("%d go commands are running once go env has answered, want none", running)
			}
		})
	}
}
