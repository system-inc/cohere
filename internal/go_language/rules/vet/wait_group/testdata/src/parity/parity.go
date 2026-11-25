// Package parity holds every branch of waitgroup's judgment, each beside a clean case, and the shapes most likely to
// part the port from the original. A line the original reports says so in a trailing comment.
package parity

import (
	"sync"
)

// The plain case and its clean twin: Add before the go statement.
func plain() {
	var group sync.WaitGroup
	go func() {
		group.Add(1) // reported
		defer group.Done()
	}()
	group.Add(1)
	go func() {
		defer group.Done()
	}()
	group.Wait()
}

// A pointer to a WaitGroup, a WaitGroup reached through a field, and one made by new.
func pointers(shared *sync.WaitGroup) {
	go func() {
		shared.Add(1) // reported
	}()
	holder := struct{ group *sync.WaitGroup }{group: shared}
	go func() {
		holder.group.Add(1) // reported
	}()
	made := new(sync.WaitGroup)
	go func() { made.Add(1) }() // reported
}

// Only the first statement of the literal is judged.
func laterStatements(group *sync.WaitGroup) {
	go func() {
		defer group.Done()
		group.Add(1)
	}()
	go func() {
		group.Wait()
		group.Add(1)
	}()
	go func() {
		var count = 1
		group.Add(count)
	}()
}

// A call nested inside the first statement is not the statement itself.
func nested(group *sync.WaitGroup, ready bool) {
	go func() {
		if ready {
			group.Add(1)
		}
	}()
	go func() {
		for range 2 {
			group.Add(1)
		}
	}()
	go func() {
		switch {
		case ready:
			group.Add(1)
		}
	}()
	go func() {
		{
			group.Add(1)
		}
	}()
	go func() {
		defer group.Add(-1)
	}()
	go func() {
		_ = func() int { group.Add(1); return 0 }()
	}()
}

// Parentheses around the statement's call or around the literal part the stack from the shape matched.
func parenthesized(group *sync.WaitGroup) {
	go func() {
		(group.Add(1))
	}()
	go (func() {
		group.Add(1)
	})()
}

// A literal started with arguments, and a literal passed as an argument of the started call: the original matches
// node types only, so a literal anywhere in the started call counts.
func arguments(group *sync.WaitGroup) {
	go func(count int) {
		group.Add(count) // reported
	}(1)
	go perform(func() {
		group.Add(1) // reported
	})
	go func(work func()) {
		work()
	}(func() {
		group.Add(1) // reported
	})
	go perform(func() {
		defer group.Done()
	})
}

func perform(work func()) { work() }

// Not a new goroutine: a deferred literal, a literal called in place, a named function started, a literal passed to
// a call inside the goroutine, and a literal kept for later.
func notStarted(group *sync.WaitGroup) {
	defer func() {
		group.Add(1)
	}()
	func() {
		group.Add(1)
	}()
	go addOne(group)
	go func() {
		perform(func() {
			group.Add(1)
		})
	}()
	later := func() {
		group.Add(1)
	}
	go later()
}

func addOne(group *sync.WaitGroup) {
	group.Add(1)
}

// A goroutine started inside another, each judged on its own.
func goroutinesInGoroutines(group *sync.WaitGroup) {
	go func() {
		group.Add(1) // reported
		go func() {
			group.Add(1) // reported
		}()
		go func() {
			group.Done()
		}()
	}()
}

// Other methods of WaitGroup are not Add, and a literal WaitGroup.Go starts is not a go statement's.
func otherMethods(group *sync.WaitGroup) {
	go func() {
		group.Done()
	}()
	go func() {
		group.Wait()
	}()
	go func() {
		group.Go(func() {
			group.Add(1)
		})
	}()
}

// A method value is a variable when called, and a method expression names the method itself.
func methodValuesAndExpressions(group *sync.WaitGroup) {
	add := group.Add
	go func() {
		add(1)
	}()
	go func() {
		(*sync.WaitGroup).Add(group, 1) // reported
	}()
	go func() {
		group.Add(1) // reported
	}()
}

// A WaitGroup embedded in a struct, in a pointer field, and twice over.
type tracked struct {
	sync.WaitGroup
	name string
}

type trackedTwice struct {
	tracked
}

type trackedPointer struct {
	*sync.WaitGroup
}

func embedded(first *tracked, second trackedTwice, third trackedPointer) {
	go func() {
		first.Add(1) // reported
	}()
	go func() {
		second.Add(1) // reported
	}()
	go func() {
		third.Add(1) // reported
	}()
	go func() {
		first.WaitGroup.Add(1) // reported
	}()
	_ = first.name
}

// An alias of WaitGroup is WaitGroup. A defined type over it has none of its methods, and converting back calls
// WaitGroup's Add.
type aliased = sync.WaitGroup

type pointerAliased = *sync.WaitGroup

type defined sync.WaitGroup

func aliases(first *aliased, second pointerAliased, third *defined) {
	go func() {
		first.Add(1) // reported
	}()
	go func() {
		second.Add(1) // reported
	}()
	go func() {
		(*sync.WaitGroup)(third).Add(1) // reported
	}()
	go func() {
		third.add(1)
	}()
}

func (counter *defined) add(delta int) {
	(*sync.WaitGroup)(counter).Add(delta)
}

// A type of the same name declared outside sync, and an interface WaitGroup satisfies, are not WaitGroup.
type WaitGroup struct{}

func (WaitGroup) Add(int) {}

type adder interface{ Add(int) }

func lookalikes(local WaitGroup, shared *sync.WaitGroup) {
	go func() {
		local.Add(1)
	}()
	var counter adder = shared
	go func() {
		counter.Add(1)
	}()
	var anonymous interface{ Add(int) } = shared
	go func() {
		anonymous.Add(1)
	}()
}

// Generic code: a generic function, a generic type embedding WaitGroup, its method, and a type parameter whose
// constraint names WaitGroup's methods.
func generic[T any](group *sync.WaitGroup, value T) T {
	go func() {
		group.Add(1) // reported
	}()
	return value
}

type box[T any] struct {
	sync.WaitGroup
	value T
}

func (container *box[T]) start() {
	go func() {
		container.Add(1) // reported
	}()
	_ = container.value
}

func boxes(container *box[int]) {
	go func() {
		container.Add(1) // reported
	}()
	go func() {
		container.WaitGroup.Add(1) // reported
	}()
	_ = generic[string]
}

func constrained[G interface {
	*sync.WaitGroup
	Add(int)
}](group G) {
	go func() {
		group.Add(1)
	}()
}

func useConstrained(group *sync.WaitGroup) {
	constrained(group)
}

// A method and a literal assigned to a package-level variable start goroutines too.
type worker struct {
	group sync.WaitGroup
}

func (current *worker) start() {
	go func() {
		current.group.Add(1) // reported
	}()
}

var started = func(group *sync.WaitGroup) {
	go func() {
		group.Add(1) // reported
	}()
}
