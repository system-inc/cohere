package sites

import "sync"

// A fixture for the -race list's guard (race_gated_test.go): each place it starts goroutines is a site.

type pool struct{}

func (*pool) ForEach(count int, work func(int)) {}

func fanOut(items []int) {
	var group sync.WaitGroup
	for range items {
		group.Go(func() {})
	}
	group.Wait()
	go func() {}()
}

func (worker *pool) run(each *pool) {
	each.ForEach(2, func(int) {})
}

func plain() int { return 1 }

// Beside stands in for bounded.Beside: a call by its name is a site, as the guard reads bounded's own starters.
func Beside(work func()) func() { return work }

func besides() { Beside(func() {}) }

func init() { go func() {}() }

func init() { go func() {}() }

type set[Value any] struct{ values []Value }

func (each *set[Value]) walk() { go func() {}() }

var started = func() bool { go func() {}(); return true }()
