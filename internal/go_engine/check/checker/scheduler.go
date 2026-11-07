package checker

import (
	"cmp"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// slotsPerProcessor is how many packages hold a slot at once for each processor the runtime runs goroutines on.
// Measured on cohere's own root with 16 processors, the engine alone, cold, under its memory cap, three rotated runs
// of each factor at a load average of 25 to 38, the same records in every run:
//
//	1:  9.5 to 10.7s wall (mean 10.1), 45.2 to 47.4s user (mean 46.1), lint 6.8 to 7.7s
//	2:  8.2 to  9.9s wall (mean  9.2), 44.2 to 47.8s user (mean 45.9), lint 6.2 to 7.0s
//	4:  9.0 to 11.0s wall (mean 10.3), 43.6 to 46.9s user (mean 45.8), lint 6.8 to 8.9s
//
// User time is alike at every factor, so the work and the collector's share of it barely move across this range; 2
// gave the shortest wall and lint phase, by less than the load's swing between runs, so it is the factor the numbers
// lean to rather than one they prove.
const slotsPerProcessor = 2

// packageGroup is every action on one package, which the scheduler starts together once every action on another
// package that its actions inherit facts from has finished.
type packageGroup struct {
	analyzedPackage *packages.Package
	actions         []*Action

	// cost is the bytes of the package's parsed files, which stand for how long its actions run: 0 for a package
	// with no syntax, as one the cache restored or one an earlier graph released.
	cost int

	// unfinished is how many distinct actions on other packages, which the group's actions inherit facts from, are
	// still to finish. The group is ready at 0.
	unfinished int

	granted bool
}

// scheduler hands the package groups of one graph their slots.
type scheduler struct {
	options *Options
	slots   int
	groups  []*packageGroup

	// waiting is, for each action an action on another package inherits facts from, the groups that wait on it.
	// It is written before the first group starts and only read after.
	waiting map[*Action][]*packageGroup

	lock sync.Mutex

	// wake is signaled whenever a group becomes ready or gives its slot back.
	wake      *sync.Cond
	ready     []*packageGroup
	running   int
	remaining int
}

// schedule runs every action of groups, the groups Analyze made by package, a package's actions together. Upstream's
// execAll(roots) starts every action at once, about 950 on cohere's own root, so every package's syntax, types and
// staticcheck IR are live together, and the collector marks all of it on every cycle under the engine's memory cap;
// and the largest packages, started no earlier than the rest, finish last. Here a group waits until every action it
// inherits facts from has finished, then for a slot, of which there are slotsPerProcessor times `GOMAXPROCS`, one
// under Options.Sequential. Slots go to the ready group with the largest cost first, ties by package ID, so the long
// packages start early and fewer packages are live at once. A group gives its slot back once its last action has
// finished, which is where releaseSyntax lets its syntax go.
//
// A slot holder never waits on another package: every action it inherits facts from finished before it was granted,
// so exec on those returns at once, and its horizontal dependencies are on its own package, in its own group. So the
// slots cannot deadlock. A group still unscheduled when nothing is ready and nothing runs waits on an action that
// never finishes, which only an import cycle makes and the load never does, and the scheduler panics naming it.
func schedule(groups []*syntaxReaders, options *Options) {
	scheduler := &scheduler{options: options, slots: slotsPerProcessor * runtime.GOMAXPROCS(0),
		waiting: map[*Action][]*packageGroup{}}
	if options.Sequential {
		scheduler.slots = 1
	}
	if options.slots > 0 {
		scheduler.slots = options.slots
	}
	scheduler.wake = sync.NewCond(&scheduler.lock)
	for _, shared := range groups {
		// Every group holds at least the action that made it.
		group := &packageGroup{analyzedPackage: shared.actions[0].Package, actions: shared.actions,
			cost: packageCost(shared.actions[0].Package)}
		inherited := map[*Action]bool{}
		for _, action := range shared.actions {
			for _, dependency := range action.Deps {
				if dependency.Package != action.Package && !inherited[dependency] {
					inherited[dependency] = true
					scheduler.waiting[dependency] = append(scheduler.waiting[dependency], group)
				}
			}
		}
		group.unfinished = len(inherited)
		if group.unfinished == 0 {
			scheduler.ready = append(scheduler.ready, group)
		}
		scheduler.groups = append(scheduler.groups, group)
	}
	scheduler.remaining = len(scheduler.groups)
	scheduler.run()
}

// packageCost is the bytes of analyzedPackage's parsed files, 0 when it has none.
func packageCost(analyzedPackage *packages.Package) int {
	cost := 0
	for _, file := range analyzedPackage.Syntax {
		if tokenFile := analyzedPackage.Fset.File(file.Pos()); tokenFile != nil {
			cost += tokenFile.Size()
		}
	}
	return cost
}

// run grants slots until every group has run, on the goroutine that called Analyze. Under Options.Sequential the
// one slot's group runs on this goroutine too, so a sequential graph runs as upstream's does, one action at a time
// on the caller's goroutine.
func (scheduler *scheduler) run() {
	scheduler.lock.Lock()
	defer scheduler.lock.Unlock()
	for scheduler.remaining > 0 || scheduler.running > 0 {
		if scheduler.running < scheduler.slots && len(scheduler.ready) > 0 {
			next := scheduler.takeLargest()
			next.granted = true
			scheduler.remaining--
			scheduler.running++
			if scheduler.options.Sequential {
				scheduler.lock.Unlock()
				scheduler.runGroup(next)
				scheduler.lock.Lock()
			} else {
				go scheduler.runGroup(next)
			}
			continue
		}
		if scheduler.running == 0 {
			// Invariant: a group waits only on actions on the packages its package imports, so with nothing
			// running and nothing ready a group still to start waits on an import cycle, which the load never makes.
			panic(scheduler.stuck())
		}
		scheduler.wake.Wait()
	}
}

// takeLargest removes and returns the ready group with the largest cost, of those alike the one whose package ID
// sorts first, so the order is the same every run.
func (scheduler *scheduler) takeLargest() *packageGroup {
	largest := 0
	for index, candidate := range scheduler.ready {
		chosen := scheduler.ready[largest]
		if order := cmp.Compare(chosen.cost, candidate.cost); order < 0 ||
			order == 0 && candidate.analyzedPackage.ID < chosen.analyzedPackage.ID {
			largest = index
		}
	}
	taken := scheduler.ready[largest]
	scheduler.ready = append(scheduler.ready[:largest], scheduler.ready[largest+1:]...)
	return taken
}

// runGroup runs group's actions, each with its horizontal dependencies inside it as execAll runs them, then gives
// its slot back. Each action that finishes may make another package's group ready.
func (scheduler *scheduler) runGroup(group *packageGroup) {
	if scheduler.options.observeGroup != nil {
		scheduler.options.observeGroup(group.analyzedPackage, true)
	}
	var running sync.WaitGroup
	for _, action := range group.actions {
		if scheduler.options.Sequential {
			action.exec()
			scheduler.finish(action)
			continue
		}
		running.Go(func() {
			action.exec()
			scheduler.finish(action)
		})
	}
	running.Wait()
	if scheduler.options.observeGroup != nil {
		scheduler.options.observeGroup(group.analyzedPackage, false)
	}
	scheduler.lock.Lock()
	scheduler.running--
	scheduler.lock.Unlock()
	scheduler.wake.Signal()
}

// finish counts finished as done for every group waiting on it, and readies each that waited on nothing else.
func (scheduler *scheduler) finish(finished *Action) {
	waiters := scheduler.waiting[finished]
	if len(waiters) == 0 {
		return
	}
	scheduler.lock.Lock()
	for _, group := range waiters {
		group.unfinished--
		if group.unfinished == 0 {
			scheduler.ready = append(scheduler.ready, group)
		}
	}
	scheduler.lock.Unlock()
	scheduler.wake.Signal()
}

// stuck is the panic's message when groups remain that nothing will make ready: each by package ID, with how many
// actions it still waits on.
func (scheduler *scheduler) stuck() string {
	var waiting []string
	for _, group := range scheduler.groups {
		if !group.granted {
			waiting = append(waiting, fmt.Sprintf("%s (waiting on %d)", group.analyzedPackage.ID, group.unfinished))
		}
	}
	sort.Strings(waiting)
	return fmt.Sprintf("checker: %d package groups wait on actions that never finish, which only an import cycle "+
		"makes: %s", len(waiting), strings.Join(waiting, ", "))
}
