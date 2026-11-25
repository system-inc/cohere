// Package parity holds every branch of loopclosure's judgment, with a clean case beside each, for the original and
// the port to judge side by side. This file names no Go version, so it is judged at its module's go1.21 and passed
// over when loaded outside a module, where its version is the empty, future one.
//
// Range over an integer or a function needs go1.22 or later, so neither type-checks in a file the pass judges, and
// neither is a fixture.
package parity

import (
	"golang.org/x/sync/errgroup"
)

var outside int

var other int

func use(values ...any) {}

func worker(index int) {}

type cons struct {
	car  int
	next *cons
}

type holder struct {
	field int
}

func (holder) method() {}

// Every loop statement that updates a variable.
func loopForms(values []int, table map[string]int, channel chan int, text string) {
	for index, value := range values {
		go func() {
			use(index) // want "loop variable index captured by func literal"
			use(value) // want "loop variable value captured by func literal"
		}()
	}
	for key := range table {
		defer func() {
			use(key) // want "loop variable key captured by func literal"
		}()
	}
	for received := range channel {
		go func() {
			use(received) // want "loop variable received captured by func literal"
		}()
	}
	for _, character := range text {
		go func() {
			use(character) // want "loop variable character captured by func literal"
		}()
	}
	// A blank key updates nothing that can be used.
	for _ = range values {
		go func() {
			use(values)
		}()
	}
	// A loop that names no variable at all.
	for range values {
		go func() {
			use(values)
		}()
	}
	// A variable declared outside the loop, assigned by it.
	for outside = range values {
		go func() {
			use(outside) // want "loop variable outside captured by func literal"
		}()
	}
	// The same variable updated twice is reported twice.
	for outside, outside = range values {
		go func() {
			use(outside) // want "loop variable outside captured by func literal" "loop variable outside captured by func literal"
		}()
	}
	// A parenthesized name is not an identifier, so the loop updates nothing the pass tracks.
	for (outside) = range values {
		go func() {
			use(outside)
		}()
	}
	// An index or a field is no variable either.
	var held holder
	for values[0], other = range values {
		go func() {
			use(values[0], other) // want "loop variable other captured by func literal"
		}()
	}
	for held.field = range values {
		go func() {
			use(held.field)
		}()
	}
}

// Three-clause loops, judged by their post statement.
func threeClauseLoops(head *cons, channel chan int) {
	for index := 0; index < 10; index++ {
		go func() {
			use(index) // want "loop variable index captured by func literal"
		}()
	}
	for index := 10; index > 0; index-- {
		defer func() {
			use(index) // want "loop variable index captured by func literal"
		}()
	}
	for first, second := 0, 1; first < 100; first, second = second, first+second {
		go func() {
			use(first, second) // want "loop variable first captured by func literal" "loop variable second captured by func literal"
		}()
	}
	for index := 0; index < 10; index += 2 {
		go func() {
			use(index) // want "loop variable index captured by func literal"
		}()
	}
	for pointer := head; pointer != nil; pointer = pointer.next {
		go func() {
			use(pointer.car) // want "loop variable pointer captured by func literal"
		}()
	}
	// A post statement that assigns a field updates no variable.
	for head.car = 0; head.car < 10; head.car++ {
		go func() {
			use(head.car)
		}()
	}
	// No post statement, or one that neither assigns nor increments.
	for index := 0; index < 10; {
		index++
		go func() {
			use(index)
		}()
	}
	for index := 0; index < 10; channel <- index {
		go func() {
			use(index)
		}()
	}
	for index := 0; index < 10; worker(index) {
		go func() {
			use(index)
		}()
	}
}

// What the last statement launches.
func launched(values []int, held holder) {
	for _, value := range values {
		go worker(value)
	}
	for _, value := range values {
		held.field = value
		defer held.method()
	}
	// A parenthesized literal is not a literal as written.
	for _, value := range values {
		go (func() {
			use(value)
		})()
	}
	for _, value := range values {
		run := func() {
			use(value)
		}
		go run()
	}
	// An argument is evaluated in the iteration.
	for _, value := range values {
		go func(copied int) {
			use(copied)
		}(value)
	}
	// A shadowing copy inside the literal still reads the loop's variable once, on the right.
	for _, value := range values {
		go func() {
			value := value // want "loop variable value captured by func literal"
			use(value)
		}()
	}
	// A nested literal is walked too.
	for _, value := range values {
		defer func() {
			func() {
				use(value) // want "loop variable value captured by func literal"
			}()
		}()
	}
	// A field selected by the variable's name is not the variable.
	for _, field := range values {
		go func() {
			use(holder{field: 1}.field, field) // want "loop variable field captured by func literal"
		}()
	}
}

// Last is defined recursively, and only the last statement counts.
func lastStatements(values []int, channel chan int, boxed interface{}) {
	for _, value := range values {
		go func() {
			use(value)
		}()
		use("not last")
	}
	for _, value := range values {
		if value > 0 {
			go func() {
				use(value) // want "loop variable value captured by func literal"
			}()
		}
	}
	for _, value := range values {
		if value > 0 {
			go func() {
				use(value) // want "loop variable value captured by func literal"
			}()
		} else if value < 0 {
			defer func() {
				use(value) // want "loop variable value captured by func literal"
			}()
		} else if value == 0 {
			go func() {
				use(value) // want "loop variable value captured by func literal"
			}()
		} else {
			go func() {
				use(value) // want "loop variable value captured by func literal"
			}()
		}
	}
	for _, value := range values {
		for inner := 0; inner < 2; inner++ {
			go func() {
				use(value, inner) // want "loop variable value captured by func literal" "loop variable inner captured by func literal"
			}()
		}
	}
	for _, value := range values {
		for range values {
			go func() {
				use(value) // want "loop variable value captured by func literal"
			}()
		}
	}
	for _, value := range values {
		switch value {
		case 0:
			go func() {
				use(value) // want "loop variable value captured by func literal"
			}()
		case 1:
		default:
			go func() {
				use(value)
			}()
			use("not last")
		}
	}
	for _, value := range values {
		switch boxed.(type) {
		case int:
			defer func() {
				use(value) // want "loop variable value captured by func literal"
			}()
		}
	}
	for _, value := range values {
		select {
		case <-channel:
			go func() {
				use(value) // want "loop variable value captured by func literal"
			}()
		default:
		}
	}
	// A block or a labeled statement is not looked into.
	for _, value := range values {
		{
			go func() {
				use(value)
			}()
		}
	}
	for _, value := range values {
		if value > 0 {
			goto launch
		}
	launch:
		go func() {
			use(value)
		}()
	}
	for _, value := range values {
		if value > 0 {
			use("an if not last")
		}
		use(value)
	}
	// An empty body has no last statement.
	for range values {
	}
}

// errgroup.Group's Go, and what only looks like it.
type local struct{}

func (local) Go(func() error) {}

type groupAlias = errgroup.Group

type embedding struct {
	errgroup.Group
}

type generic[Group any] struct {
	group Group
}

func errgroups(values []int, group *errgroup.Group, aliased *groupAlias, embedded embedding, wrapped generic[errgroup.Group]) {
	for _, value := range values {
		group.Go(func() error {
			use(value) // want "loop variable value captured by func literal"
			return nil
		})
	}
	for _, value := range values {
		aliased.Go(func() error {
			use(value) // want "loop variable value captured by func literal"
			return nil
		})
	}
	for _, value := range values {
		embedded.Go(func() error {
			use(value) // want "loop variable value captured by func literal"
			return nil
		})
	}
	for _, value := range values {
		wrapped.group.Go(func() error {
			use(value) // want "loop variable value captured by func literal"
			return nil
		})
	}
	var byValue errgroup.Group
	for _, value := range values {
		byValue.Go(func() error {
			use(value) // want "loop variable value captured by func literal"
			return nil
		})
	}
	for _, value := range values {
		(group).Go(func() error {
			use(value) // want "loop variable value captured by func literal"
			return nil
		})
	}
	// A method value, a method expression, a function that is not a literal, and another type's Go.
	for _, value := range values {
		launch := group.Go
		launch(func() error {
			use(value)
			return nil
		})
	}
	for _, value := range values {
		(*errgroup.Group).Go(group, func() error {
			use(value)
			return nil
		})
	}
	task := func() error { return nil }
	for range values {
		group.Go(task)
	}
	for _, value := range values {
		local{}.Go(func() error {
			use(value)
			return nil
		})
	}
	// A Go that is not last.
	for _, value := range values {
		group.Go(func() error {
			use(value)
			return nil
		})
		use("not last")
	}
	// A Go not called as a statement of its own.
	for _, value := range values {
		if true {
			defer group.Go(func() error {
				use(value)
				return nil
			})
		}
	}
}

// A type declared inside a function that embeds errgroup's Group promotes its Go, whose receiver is still errgroup's.
func localGroup(values []int) {
	type Group struct{ errgroup.Group }
	var group Group
	for _, value := range values {
		group.Go(func() error {
			use(value) // want "loop variable value captured by func literal"
			return nil
		})
	}
}

// Generic code: a loop over a type parameter's values, and a Go through a constraint.
func generics[Element any, Launcher interface {
	*errgroup.Group
	Go(func() error)
}](elements []Element, launcher Launcher) {
	for index, element := range elements {
		go func() {
			use(index, element) // want "loop variable index captured by func literal" "loop variable element captured by func literal"
		}()
	}
	for _, element := range elements {
		launcher.Go(func() error {
			use(element)
			return nil
		})
	}
}

// A loop inside a literal inside a loop is judged as its own loop, and the outer loop through it.
func nested(values []int) {
	for _, outer := range values {
		go func() {
			for _, inner := range values {
				go func() {
					use(outer, inner) // want "loop variable outer captured by func literal" "loop variable inner captured by func literal"
				}()
			}
		}()
	}
}
