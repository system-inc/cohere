package go_engine

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// preludeWatch keeps the prelude's steps in the order they were told, closes each step's channel (first) at its first
// telling, and hands each of runFix's steps to onStep, which may hold runFix's goroutine there.
type preludeWatch struct {
	lock   sync.Mutex
	steps  []preludeStep
	firsts map[preludeStep]chan struct{}
	onStep func(step preludeStep)
}

// first is closed once step has been told.
func (watch *preludeWatch) first(step preludeStep) <-chan struct{} {
	watch.lock.Lock()
	defer watch.lock.Unlock()
	return watch.firstLocked(step)
}

// firstLocked is first, under the watch's lock.
func (watch *preludeWatch) firstLocked(step preludeStep) chan struct{} {
	if watch.firsts == nil {
		watch.firsts = map[preludeStep]chan struct{}{}
	}
	if _, made := watch.firsts[step]; !made {
		watch.firsts[step] = make(chan struct{})
	}
	return watch.firsts[step]
}

func (watch *preludeWatch) told(step preludeStep) {
	watch.lock.Lock()
	if !slices.Contains(watch.steps, step) {
		close(watch.firstLocked(step))
	}
	watch.steps = append(watch.steps, step)
	watch.lock.Unlock()
	// Outside the lock, since onStep may wait for a step another goroutine tells under it.
	if (step == preludeFixBegan || step == preludeFixReturned) && watch.onStep != nil {
		watch.onStep(step)
	}
}

// toldSteps is every step told, in order.
func (watch *preludeWatch) toldSteps() []preludeStep {
	watch.lock.Lock()
	defer watch.lock.Unlock()
	return slices.Clone(watch.steps)
}

// runWatchingPrelude runs the engine over a copy of fixture, under `--no-cache` so the lint pass runs beside the load,
// with extra after its root, telling watch of each step of the prelude.
func runWatchingPrelude(test *testing.T, fixture string, extra []string, watch *preludeWatch) {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", fixture))
	request, parseError := parseInvocation(append(
		[]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-cache"}, extra...))
	if parseError != nil {
		test.Fatal(parseError)
	}
	registered := rules.All()
	ruleSettings, settingsError := loadSettings(request, registered)
	if settingsError != nil {
		test.Fatal(settingsError)
	}
	if optionsError := configureRules(ruleSettings, registered); optionsError != nil {
		test.Fatal(optionsError)
	}
	described, describeError := describeProject(request)
	if describeError != nil {
		test.Fatal(describeError)
	}
	engine := newPipeline(request, described, ruleSettings, registered, io.Discard, io.Discard, nil)
	engine.watchPrelude = watch.told
	engine.run()
}

// Under `--no-fix` the first load begins before gofmt's check returns: told when runFix has returned, the watch holds
// runFix's goroutine until the load has begun, which a load started only after runFix never could, so that run waits
// out the 10s and fails.
func TestTheFirstLoadBeginsBesideGofmtsCheckUnderNoFix(test *testing.T) {
	test.Parallel()
	watch := &preludeWatch{}
	watch.onStep = func(step preludeStep) {
		if step != preludeFixReturned {
			return
		}
		select {
		case <-watch.first(preludeLoadBegan):
		case <-time.After(10 * time.Second):
			test.Error("runFix returned and no load began within 10s, so the load waited for gofmt's check")
		}
	}
	runWatchingPrelude(test, "unformatted", []string{"--no-fix"}, watch)
	if steps := watch.toldSteps(); !slices.Contains(steps, preludeLoadBegan) {
		test.Fatalf("the run told %v, want a load", steps)
	}
}

// A run that writes loads only after gofmt has written, since the load must read what gofmt wrote: told as runFix
// starts, the watch holds runFix's goroutine 2s for a load to begin, which only a load started beside runFix could, and
// then the load begins after runFix has returned.
func TestTheFirstLoadWaitsForGofmtInARunThatWrites(test *testing.T) {
	test.Parallel()
	watch := &preludeWatch{}
	watch.onStep = func(step preludeStep) {
		if step != preludeFixBegan {
			return
		}
		select {
		case <-watch.first(preludeLoadBegan):
			test.Error("a load began before runFix, in a run whose load must read what gofmt writes")
		case <-time.After(2 * time.Second):
		}
	}
	runWatchingPrelude(test, "unformatted", nil, watch)
	steps := watch.toldSteps()
	want := []preludeStep{preludeFixBegan, preludeFixReturned, preludeLoadBegan}
	if len(steps) < len(want) || !slices.Equal(steps[:len(want)], want) {
		test.Fatalf("the run told %v, want %v first", steps, want)
	}
}

// Under `--no-fix` a load with a type error stops the lint pass beside it as soon as it returns, as a load after gofmt
// does, not once gofmt's check has returned too: told when runFix has returned, the watch holds runFix's goroutine
// until the pass is abandoned, which a pass left to runLint, after the join, never could be, so that run waits out the
// 10s and fails. A pass left running for as long as gofmt outlasts the load starts packages main never lints.
func TestALoadWithATypeErrorStopsTheLintBesideItBeforeGofmtReturns(test *testing.T) {
	test.Parallel()
	watch := &preludeWatch{}
	watch.onStep = func(step preludeStep) {
		if step != preludeFixReturned {
			return
		}
		select {
		case <-watch.first(preludeLintAbandoned):
		case <-time.After(10 * time.Second):
			test.Error("runFix returned and the lint pass beside a load with a type error was not abandoned within 10s")
		}
	}
	runWatchingPrelude(test, "unformatted-type-error", []string{"--no-fix"}, watch)
	if steps := watch.toldSteps(); !slices.Contains(steps, preludeLintAbandoned) {
		test.Fatalf("the run told %v, want the lint pass abandoned", steps)
	}
}

// Only a check loads beside gofmt: a run that writes, and the editor's save, which formats its buffer into the bytes
// the load reads, load after it, a save even if it were asked under `--no-fix`, which the command line refuses.
func TestOnlyACheckLoadsBesideGofmt(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name    string
		request invocation
		beside  bool
	}{
		{name: "a check", request: invocation{switches: map[string]bool{"no-fix": true}}, beside: true},
		{name: "a run that writes", request: invocation{switches: map[string]bool{}}, beside: false},
		{
			name:    "a save",
			request: invocation{switches: map[string]bool{"fix": true}, stdinPath: "/project/saved.go"},
			beside:  false,
		},
		{
			name:    "a save under --no-fix",
			request: invocation{switches: map[string]bool{"no-fix": true}, stdinPath: "/project/saved.go"},
			beside:  false,
		},
	}
	for _, each := range cases {
		engine := &pipeline{request: each.request}
		if beside := engine.loadsBesideFix(); beside != each.beside {
			test.Errorf("%s loads beside gofmt %t, want %t", each.name, beside, each.beside)
		}
	}
}
