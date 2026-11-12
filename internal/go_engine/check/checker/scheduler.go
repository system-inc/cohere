package checker

import (
	"cmp"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
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

// packageGroup is every action on one package, which the scheduler admits together once every action on another
// package that its actions inherit facts from has finished.
type packageGroup struct {
	analyzedPackage *packages.Package
	actions         []*scheduledAction

	// cost is the bytes of the package's parsed files, which stand for how long its actions run: 0 for a package
	// with no syntax, as one the cache restored or one an earlier graph released.
	cost int

	// priority is the package's longest weighted path to the end of the graph: its cost plus the largest priority of
	// the groups that wait on its actions. A ready action on a package with the larger priority runs first, since
	// more of what is still to run waits behind it.
	priority int

	// unfinished is how many distinct actions on other packages, which the group's actions inherit facts from, are
	// still to finish, and one more while the package is not yet final for analysis (Options.Ready). The group is
	// ready at 0.
	unfinished int

	// unexecuted is how many of the group's actions are still to finish once it is granted. Its slot is given back
	// at 0.
	unexecuted int

	granted bool
}

// scheduledAction is an action as the scheduler tracks it, within its package's group.
type scheduledAction struct {
	action *Action
	group  *packageGroup

	// order is the action's place in its group, the depth-first postorder Analyze grouped them in, which orders the
	// ready actions of one package the same way every run and puts each after its horizontal prerequisites.
	order int

	// prerequisites is how many of the action's horizontal prerequisites, the actions on its own package whose
	// Results it reads, are still to finish. The action is ready at 0, once its group is granted.
	prerequisites int

	// dependents are the actions on the same package that read this action's Result, one entry for each edge.
	dependents []*scheduledAction
}

// scheduler hands the package groups of one graph their slots, and runs the ready actions of the granted groups on
// a bounded pool of workers.
type scheduler struct {
	options *Options
	slots   int
	workers int
	groups  []*packageGroup

	// longest is the largest group priority, which a pool's slot puts an action's group's priority on its scale
	// against (bounded.ScaledPriority). It is written before the first group starts and only read after.
	longest int

	// waiting is, for each action an action on another package inherits facts from, the groups that wait on it.
	// It is written before the first group starts and only read after.
	waiting map[*Action][]*packageGroup

	lock sync.Mutex

	// wake is broadcast whenever an action finishes, which may ready actions, ready a group, give a slot back, or
	// end the graph, and whenever a package becomes final or the graph is abandoned.
	wake *sync.Cond

	// ready are the groups waiting for a slot.
	ready []*packageGroup

	// queue holds the ready actions of the granted groups, the highest priority first.
	queue readyQueue

	// executing is how many actions a worker is running now; running is how many groups hold a slot; remaining is
	// how many groups are still to be granted one.
	executing int
	running   int
	remaining int

	// awaiting counts the groups whose package is not yet final for analysis (Options.Ready), and abandoned is set
	// once Options.Abandon closes, after which no group is granted and no action starts.
	awaiting  int
	abandoned bool

	// stuck is the panic's message once a worker has found groups that nothing will make ready, for the caller's
	// goroutine to raise when every worker has stopped.
	stuck string
}

// schedule runs every action of groups, the groups Analyze made by package. Upstream's execAll(roots) starts every
// action at once, about 950 on cohere's own root, so every package's syntax, types and staticcheck IR are live
// together, and the collector marks all of it on every cycle under the engine's memory cap; and the largest
// packages, started no earlier than the rest, finish last.
//
// Here packages are admitted a bounded number at a time. A group waits until every action that any of its actions
// inherits facts from has finished: the count covers every action of the group, since its actions are not ordered
// so that the first's prerequisites cover the rest. Then it waits for a slot, of which there are slotsPerProcessor
// times `GOMAXPROCS`, one under Options.Sequential. Slots go to the ready group with the longest weighted path to the
// end of the graph first (packageGroup.priority), then the largest, then by package ID: granting by cost alone held
// the TypeScript checker's chain on cohere's root behind about 344 larger groups for 0.45 to 0.63s, though more of the
// run waited behind it than behind any of them. A group gives its slot back once its last action has finished, which
// is where releaseSyntax lets its syntax go.
//
// Within the granted groups only ready actions run, those whose horizontal prerequisites have all finished, from
// one queue, on `GOMAXPROCS` workers, one on the caller's goroutine under Options.Sequential. A goroutine for every
// action of a granted group, about 230 on one of cohere's workspace packages, and another for each prerequisite
// inside it, left up to 2,312 goroutines runnable on cohere's root while 12 to 93 actions ran, and the critical
// chain's actions queued behind the rest: rules/core's buildir spent 0.27 of its 0.74s runnable. A worker takes the
// ready action on the package with the longest weighted path to the end of the graph (packageGroup.priority), so
// the chain the rest waits on runs first.
//
// Under Options.Ready a group also waits for its package to be final, as one more thing unfinished, counted down by a
// goroutine of its own that waits on the package's channel, never by a worker. Its cost is read from the sizes of the
// files the load will parse for it (listedCost), since they are parsed only later, by the load beside the graph, so
// the priorities are those of a graph run after the load.
//
// A worker never waits inside an action: every action it inherits facts from finished before its group was granted,
// and every horizontal prerequisite before it was queued, so exec on those returns at once. So the pool cannot
// deadlock. A group still ungranted when nothing is queued, nothing runs and every package is final waits on an action
// that never finishes, which only an import cycle makes and the load never does, and the scheduler panics naming it.
func schedule(groups []*syntaxReaders, options *Options) {
	scheduler := &scheduler{options: options, slots: slotsPerProcessor * runtime.GOMAXPROCS(0),
		workers: runtime.GOMAXPROCS(0), waiting: map[*Action][]*packageGroup{}}
	if options.Sequential {
		scheduler.slots = 1
		scheduler.workers = 1
	}
	if options.slots > 0 {
		scheduler.slots = options.slots
	}
	if options.workers > 0 {
		scheduler.workers = options.workers
	}
	scheduler.wake = sync.NewCond(&scheduler.lock)
	// finals are the channels of the groups whose package is not yet final, by the group's index in groups.
	finals := map[int]<-chan struct{}{}
	for _, shared := range groups {
		// Every group holds at least the action that made it.
		group := &packageGroup{analyzedPackage: shared.actions[0].Package}
		var final <-chan struct{}
		if options.Ready != nil {
			final = options.Ready(group.analyzedPackage)
		}
		if final == nil {
			group.cost = packageCost(group.analyzedPackage)
		} else {
			finals[len(scheduler.groups)] = final
			group.cost = listedCost(group.analyzedPackage, options.Restored)
		}
		scheduled := make(map[*Action]*scheduledAction, len(shared.actions))
		for order, action := range shared.actions {
			scheduled[action] = &scheduledAction{action: action, group: group, order: order}
			group.actions = append(group.actions, scheduled[action])
		}
		// Every action counts, not only the first: a group's actions are in postorder, which does not put first the
		// one whose prerequisites cover the rest. A plain analyzer with none may come first, and a fact analyzer that
		// inherits from another package after it.
		inherited := map[*Action]bool{}
		for _, dependent := range group.actions {
			for _, dependency := range dependent.action.Deps {
				if dependency.Package == dependent.action.Package {
					// A horizontal prerequisite is on the same package, so in this group.
					scheduled[dependency].dependents = append(scheduled[dependency].dependents, dependent)
					dependent.prerequisites++
					continue
				}
				if !inherited[dependency] {
					inherited[dependency] = true
					scheduler.waiting[dependency] = append(scheduler.waiting[dependency], group)
				}
			}
		}
		group.unfinished = len(inherited)
		if _, waits := finals[len(scheduler.groups)]; waits {
			group.unfinished++
		}
		if group.unfinished == 0 {
			scheduler.ready = append(scheduler.ready, group)
		}
		scheduler.groups = append(scheduler.groups, group)
	}
	scheduler.prioritize()
	for _, group := range scheduler.groups {
		scheduler.longest = max(scheduler.longest, group.priority)
	}
	scheduler.remaining = len(scheduler.groups)
	scheduler.awaiting = len(finals)
	for index, final := range finals {
		go scheduler.await(index, final)
	}
	if options.Abandon != nil {
		ran := make(chan struct{})
		defer close(ran)
		go scheduler.watchAbandon(options.Abandon, ran)
	}
	scheduler.run()
}

// prioritize sets every group's priority, its cost plus the largest priority of the groups that wait on its
// actions, by a depth-first search that computes each group's once. A group met again while its own search is
// still open is on an import cycle, which the load never makes; it counts its cost alone there, so the search ends,
// and run's panic names the cycle's groups.
func (scheduler *scheduler) prioritize() {
	computed := map[*packageGroup]bool{}
	open := map[*packageGroup]bool{}
	var visit func(group *packageGroup) int
	visit = func(group *packageGroup) int {
		if computed[group] {
			return group.priority
		}
		if open[group] {
			return group.cost
		}
		open[group] = true
		longestAfter := 0
		for _, scheduled := range group.actions {
			for _, waiter := range scheduler.waiting[scheduled.action] {
				longestAfter = max(longestAfter, visit(waiter))
			}
		}
		delete(open, group)
		group.priority = group.cost + longestAfter
		computed[group] = true
		return group.priority
	}
	for _, group := range scheduler.groups {
		visit(group)
	}
}

// await counts the package of the group at index as final once final closes.
func (scheduler *scheduler) await(index int, final <-chan struct{}) {
	<-final
	scheduler.lock.Lock()
	scheduler.awaiting--
	scheduler.groups[index].unfinished--
	if scheduler.groups[index].unfinished == 0 {
		scheduler.ready = append(scheduler.ready, scheduler.groups[index])
	}
	scheduler.lock.Unlock()
	scheduler.wake.Broadcast()
}

// watchAbandon marks the graph abandoned once abandon closes, unless the graph has ended first.
func (scheduler *scheduler) watchAbandon(abandon <-chan struct{}, ran <-chan struct{}) {
	select {
	case <-abandon:
		scheduler.lock.Lock()
		scheduler.abandoned = true
		scheduler.lock.Unlock()
		scheduler.wake.Broadcast()
	case <-ran:
	}
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

// listedCost is packageCost for a package the load has not parsed yet, read from the sizes of the files it will parse
// for it, which are the bytes packageCost reads once they are: 0 for a package restored (Options.Restored), which the
// load never parses, and nothing for a file that cannot be read.
func listedCost(analyzedPackage *packages.Package, restored func(*packages.Package) bool) int {
	if restored != nil && restored(analyzedPackage) {
		return 0
	}
	cost := 0
	for _, file := range analyzedPackage.CompiledGoFiles {
		if information, statError := os.Stat(file); statError == nil {
			cost += int(information.Size())
		}
	}
	return cost
}

// run runs the workers until every group has run, one of them on the goroutine that called Analyze. Under
// Options.Sequential that one is the only one, so a sequential graph runs as upstream's does, one action at a time
// on the caller's goroutine, and in the same order every run.
func (scheduler *scheduler) run() {
	var workers sync.WaitGroup
	for range scheduler.workers - 1 {
		workers.Go(scheduler.work)
	}
	scheduler.work()
	workers.Wait()
	if scheduler.stuck != "" {
		// Invariant: only an import cycle leaves a group that nothing makes ready, and the load never makes one.
		panic(scheduler.stuck)
	}
}

// work grants the free slots and runs the highest-priority ready action, again and again, until every group has
// run or the graph is stuck.
//
// The lock is released by hand rather than deferred: a panic out of an action's pass, which runs outside the lock,
// would otherwise unlock an unlocked mutex, a fatal error, in place of the pass's own panic.
func (scheduler *scheduler) work() {
	scheduler.lock.Lock()
	for {
		if scheduler.abandoned {
			// What has started finishes, since an action is never stopped part way; nothing more starts.
			if scheduler.executing == 0 {
				scheduler.wake.Broadcast()
				scheduler.lock.Unlock()
				return
			}
			scheduler.wake.Wait()
			continue
		}
		scheduler.admit()
		if scheduler.stuck != "" || scheduler.remaining == 0 && scheduler.running == 0 {
			scheduler.lock.Unlock()
			return
		}
		if scheduler.queue.length() > 0 {
			next := scheduler.queue.take()
			scheduler.executing++
			scheduler.lock.Unlock()
			// The run's pool, when there is one, bounds the actions of every build at once; a worker holds no other
			// slot, and its action never waits on one (see schedule), so taking it here cannot deadlock.
			if pool := scheduler.options.Pool; pool != nil {
				pool.Acquire(bounded.ScaledPriority(next.group.priority, scheduler.longest))
			}
			next.action.exec()
			if pool := scheduler.options.Pool; pool != nil {
				pool.Release()
			}
			scheduler.lock.Lock()
			scheduler.executing--
			scheduler.finish(next)
			scheduler.wake.Broadcast()
			continue
		}
		if scheduler.executing == 0 && scheduler.awaiting == 0 {
			// Invariant: a group waits only on actions on the packages its package imports, and on its package being
			// final, and an action only on the actions on its own package that it requires, so with nothing queued,
			// nothing executing, every package final and every free slot granted, a group still to run waits on an
			// import cycle, which the load never makes.
			scheduler.stuck = scheduler.describeStuck()
			scheduler.wake.Broadcast()
			scheduler.lock.Unlock()
			return
		}
		scheduler.wake.Wait()
	}
}

// admit grants a slot to each ready group while one is free, the longest path first (takeFirst), and queues each
// granted group's actions that require nothing on their own package. The lock is held.
func (scheduler *scheduler) admit() {
	for scheduler.running < scheduler.slots && len(scheduler.ready) > 0 {
		next := scheduler.takeFirst()
		next.granted = true
		next.unexecuted = len(next.actions)
		scheduler.remaining--
		scheduler.running++
		if scheduler.options.observeGroup != nil {
			scheduler.options.observeGroup(next.analyzedPackage, true)
		}
		for _, scheduled := range next.actions {
			if scheduled.prerequisites == 0 {
				scheduler.queue.add(scheduled)
			}
		}
	}
}

// takeFirst removes and returns the ready group with the largest priority, of those alike the largest cost, and of
// those the one whose package ID sorts first, so the order is the same every run.
func (scheduler *scheduler) takeFirst() *packageGroup {
	first := 0
	for index, candidate := range scheduler.ready {
		chosen := scheduler.ready[first]
		order := cmp.Or(cmp.Compare(chosen.priority, candidate.priority), cmp.Compare(chosen.cost, candidate.cost))
		if order < 0 || order == 0 && candidate.analyzedPackage.ID < chosen.analyzedPackage.ID {
			first = index
		}
	}
	taken := scheduler.ready[first]
	scheduler.ready = append(scheduler.ready[:first], scheduler.ready[first+1:]...)
	return taken
}

// finish counts finished as done for every group waiting on it, readying each that waited on nothing else, and for
// every action on its package that reads its Result, queueing each that waited on nothing else. The last of its
// group's actions gives the group's slot back. The lock is held.
func (scheduler *scheduler) finish(finished *scheduledAction) {
	for _, group := range scheduler.waiting[finished.action] {
		group.unfinished--
		if group.unfinished == 0 {
			scheduler.ready = append(scheduler.ready, group)
		}
	}
	for _, dependent := range finished.dependents {
		dependent.prerequisites--
		if dependent.prerequisites == 0 {
			scheduler.queue.add(dependent)
		}
	}
	finished.group.unexecuted--
	if finished.group.unexecuted > 0 {
		return
	}
	if scheduler.options.observeGroup != nil {
		scheduler.options.observeGroup(finished.group.analyzedPackage, false)
	}
	scheduler.running--
}

// describeStuck is the panic's message when groups remain that nothing will make ready: each by package ID, with
// how many actions it still waits on.
func (scheduler *scheduler) describeStuck() string {
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
