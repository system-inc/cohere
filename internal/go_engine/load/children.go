package load

import (
	"slices"
	"strings"
	"sync"
)

// childNameArguments is how many of a go command's arguments its name keeps, enough to tell go env from each go list
// a load runs: `go list -m -json`, `go list -e -f`, `go list -e -deps`, `go list -e -compiled`.
const childNameArguments = 3

// Children is a run's account of the go commands its loads start, and of those started once its first load has begun
// (#sktb2y8). On darwin go starts a child process with fork, which marks the whole address space copy-on-write, so
// each child started once the engine's heap has grown makes every resident page fault again on its next write: the
// run starts every go command it needs before the host's first load, while the heap is small, and one started after
// it is counted and named here, so a late one shows on the lint record. It is the run's own, never package-level
// state, since tests run many pipelines in one process. A nil Children records nothing.
type Children struct {
	// loading is set once the run's first load has begun, late names each go command started after, and running counts
	// those started and not yet ended, each under lock, since extra builds load at once.
	lock    sync.Mutex
	loading bool
	late    []string
	running int

	// watchStarted is a test's look at each go command started, nil outside a test (WatchStarted), under lock.
	watchStarted func(name string)

	// Answers are what go env answered the test process before, which the run's go env reads in place of starting a
	// child when one was kept under the same key (GoAnswers), nil in every run but a test's. Set before the run's first
	// go command, and only read after.
	Answers *GoAnswers
}

// LoadBegins marks the run's first load as begun, so every go command started after it is late. A later call changes
// nothing.
func (children *Children) LoadBegins() {
	if children == nil {
		return
	}
	children.lock.Lock()
	defer children.lock.Unlock()
	children.loading = true
}

// Late is each go command started after the run's first load began, named by childName, sorted, nil for none. Sorted,
// since commands started at once, as the lister's two are, start in either order, and a record must read the same on
// every run.
func (children *Children) Late() []string {
	if children == nil {
		return nil
	}
	children.lock.Lock()
	defer children.lock.Unlock()
	return slices.Sorted(slices.Values(children.late))
}

// Running is how many go commands are started and not yet ended, so a test can hold a run to leaving none running as
// it returns.
func (children *Children) Running() int {
	if children == nil {
		return 0
	}
	children.lock.Lock()
	defer children.lock.Unlock()
	return children.running
}

// WatchStarted has watch told the name of each go command started from now on (childName), on the goroutine that
// starts it, once it is counted running and before it runs, so a test can hold a command there. Only a test calls it.
func (children *Children) WatchStarted(watch func(name string)) {
	children.lock.Lock()
	defer children.lock.Unlock()
	children.watchStarted = watch
}

// started records a go command started with arguments, the go command's own, named when the run's first load has
// begun, and running until ended, and tells a test watching (WatchStarted).
func (children *Children) started(arguments []string) {
	if children == nil {
		return
	}
	children.lock.Lock()
	children.running++
	if children.loading {
		children.late = append(children.late, childName(arguments))
	}
	watch := children.watchStarted
	children.lock.Unlock()
	if watch != nil {
		watch(childName(arguments))
	}
}

// ended records that a go command started has ended, every child it ran waited for.
func (children *Children) ended() {
	if children == nil {
		return
	}
	children.lock.Lock()
	defer children.lock.Unlock()
	children.running--
}

// answers are the run's kept answers (Answers), nil for a nil Children.
func (children *Children) answers() *GoAnswers {
	if children == nil {
		return nil
	}
	return children.Answers
}

// childName is a go command named short: go and its first childNameArguments arguments, each up to any `=`, so
// `-json=ImportPath,...` reads as `-json`.
func childName(arguments []string) string {
	words := []string{"go"}
	for _, argument := range arguments[:min(len(arguments), childNameArguments)] {
		flag, _, _ := strings.Cut(argument, "=")
		words = append(words, flag)
	}
	return strings.Join(words, " ")
}
