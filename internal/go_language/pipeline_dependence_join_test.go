package go_language

import (
	"io"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/project"
)

// No extra build starts before the host's dependence sets are in its memo (#ybjbqm2): the first set the host's pass
// asks is held until either the run joins the pass (HostRun.DependenceComputed) or a build ends, and it must be the
// join that lets it go. A run that started its builds without the join would compute the held set again in a build's
// walk, beside the pass, and its builds would end with the set still held.
func TestTheBuildsStartOnlyOnceTheHostsSetsAreComputed(test *testing.T) {
	test.Parallel()

	var lock sync.Mutex
	held := false
	letGoBy := ""
	hold := make(chan struct{})
	var letGo sync.Once
	release := func(by string) {
		letGo.Do(func() {
			lock.Lock()
			letGoBy = by
			lock.Unlock()
			close(hold)
		})
	}
	// A failure below still lets the held set go, so the run's pass ends.
	defer release("the test's end")
	runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "restore-from-host")), exitFindings,
		func(engine *pipeline) {
			engine.keptDependence = func(*packages.Package, check.Restoring) {
				lock.Lock()
				first := !held
				held = true
				lock.Unlock()
				if first {
					<-hold
				}
			}
			engine.dependenceJoining = func() { release("the join") }
			engine.extraBuildEnds = func(project.Build, io.Writer) error {
				release("a build's end")
				return nil
			}
		}, "--no-cache")
	lock.Lock()
	defer lock.Unlock()
	if !held {
		test.Fatalf("the host's pass asked no set, so nothing was held")
	}
	if letGoBy != "the join" {
		test.Errorf("the held set was let go by %s, want the join, before any build started", letGoBy)
	}
}
