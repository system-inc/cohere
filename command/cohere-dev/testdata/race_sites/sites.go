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
