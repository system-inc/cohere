package checker

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
)

// sharedPoolChildVariable, set to anything in a child test process's environment, has
// TestAWaitingWorkerHoldsNoSlotAnotherBuildNeeds run its two builds there rather than start the child.
const sharedPoolChildVariable = "COHERE_TEST_CHECKER_SHARED_POOL"

// A worker gives its slot of the run's pool back before it waits, so two builds side by side over one pool never hold
// each other up (#ybjbqm2). Two graphs share a pool of one slot, each on one worker, two workers for the slot. The
// first build's early package takes the slot and runs until the second build's worker waits for it; its late package
// is not ready, so its worker then waits with nothing queued; and late is made ready only once the second build has
// finished. A worker that kept its slot while it waited would hold the second build waiting for it forever, and
// itself waiting on the second build. Both builds finish, and every slot is free after.
//
// The builds run in a child test process, as the bounded package's panic test does, so that hang is the child's
// timeout, this test's failure, rather than a stalled run.
func TestAWaitingWorkerHoldsNoSlotAnotherBuildNeeds(test *testing.T) {
	test.Parallel()
	if os.Getenv(sharedPoolChildVariable) != "" {
		runTwoBuildsOverOneSlot(test)
		return
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	command := exec.Command(
		executable,
		"-test.run=^TestAWaitingWorkerHoldsNoSlotAnotherBuildNeeds$",
		"-test.v",
		"-test.count=1",
		"-test.timeout=60s",
	)
	command.Env = append(os.Environ(), sharedPoolChildVariable+"=1")
	output, outputError := command.CombinedOutput()
	// The pass line proves the child ran the builds and not an empty match.
	passed := "--- PASS: TestAWaitingWorkerHoldsNoSlotAnotherBuildNeeds"
	if outputError != nil || !strings.Contains(string(output), passed) {
		test.Fatalf("the child running two builds over one slot failed (%v):\n%s", outputError, output)
	}
}

// runTwoBuildsOverOneSlot is TestAWaitingWorkerHoldsNoSlotAnotherBuildNeeds's two builds, run in the child.
func runTwoBuildsOverOneSlot(test *testing.T) {
	first := writeModule(test, "first", map[string]string{
		"early/early.go": packageOf("early", 0),
		"late/late.go":   packageOf("late", 0),
	}, "./...")
	second := writeModule(test, "second", map[string]string{"other/other.go": packageOf("other", 0)}, "./...")
	pool := bounded.NewPool(1)
	// held is the slots held now, which every one given back brings to 0 once both builds have finished.
	var held atomic.Int64
	pool.Observe(func(change int) { held.Add(int64(change)) })
	byName, ready := gates(first)
	close(byName["early"])
	// earlyHolds is closed once early runs, under the pool's one slot.
	earlyHolds := make(chan struct{})
	var signalEarly sync.Once
	holding := &analysis.Analyzer{
		Name: "holding",
		Doc:  "holding keeps early's slot until the second build's worker waits for it",
		Run: func(pass *analysis.Pass) (any, error) {
			if pass.Pkg.Name() != "early" {
				return nil, nil
			}
			signalEarly.Do(func() { close(earlyHolds) })
			// The second build's worker has taken its action and asked for the slot, so early's worker finds nothing
			// queued once it finishes, with the second build's work waiting on the slot it holds.
			for pool.Waiters() < 1 {
				time.Sleep(time.Millisecond)
			}
			return nil, nil
		},
	}
	firstDone := make(chan error, 1)
	go func() {
		_, analyzeError := Analyze([]*analysis.Analyzer{holding}, first, &Options{Pool: pool, Ready: ready, workers: 1})
		firstDone <- analyzeError
	}()
	<-earlyHolds
	_, secondError := Analyze([]*analysis.Analyzer{quietAnalyzer}, second, &Options{Pool: pool, workers: 1})
	if secondError != nil {
		test.Fatalf("analyzing the second build: %v", secondError)
	}
	// Late is ready only once the second build has finished, so the first build's worker waits on the second build
	// while it has nothing queued.
	close(byName["late"])
	if analyzeError := <-firstDone; analyzeError != nil {
		test.Fatalf("analyzing the first build: %v", analyzeError)
	}
	if held.Load() != 0 || pool.Waiters() != 0 {
		test.Errorf("%d slots held and %d waiting once both builds finished, want 0 and 0", held.Load(),
			pool.Waiters())
	}
}
