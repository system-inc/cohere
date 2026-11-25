// Package parity holds every shape the pattern accepts or refuses: where the Add call sits in the goroutine's body
// and what wraps it, how the function literal is called, the receivers through which Add is reached, and Adds of
// other types.
package parity

import (
	"sync"
	"sync/atomic"
)

type counted struct {
	sync.WaitGroup
}

type group = sync.WaitGroup

type other struct{}

func (other) Add(int) {}

func positions(wg *sync.WaitGroup) {
	go func() {
		wg.Add(1)
	}()
	go func() {
		wg.Done()
		wg.Add(1)
	}()
	go func() {
		{
			wg.Add(1)
		}
		wg.Done()
	}()
	go func() {
		{
			wg.Add(1)
			wg.Done()
		}
	}()
	go func() {
		{
			{
				wg.Add(2)
			}
		}
	}()
	go func() {
	label:
		wg.Add(1)
		goto label
	}()
	go func() {
		(wg.Add(1))
	}()
	go func() {
		defer wg.Add(1)
	}()
	go func() {
		if true {
			wg.Add(1)
		}
	}()
	go func() {
		var _ = 1
		wg.Add(1)
	}()
	go func() {}()
}

func literals(wg *sync.WaitGroup) {
	go (func() {
		wg.Add(1)
	})()
	go func(group *sync.WaitGroup) {
		group.Add(1)
	}(wg)
	go func(count int) {
		wg.Add(count)
	}(1)
	start := func() {
		wg.Add(1)
	}
	go start()
	func() {
		wg.Add(1)
	}()
	defer func() {
		wg.Add(1)
	}()
}

func receivers(value sync.WaitGroup, pointer *sync.WaitGroup, embedded counted, aliased *group, nothing other,
	number *atomic.Int64) {
	go func() {
		value.Add(1)
	}()
	go func() {
		(*sync.WaitGroup).Add(pointer, 1)
	}()
	go func() {
		embedded.Add(1)
	}()
	go func() {
		embedded.WaitGroup.Add(1)
	}()
	go func() {
		aliased.Add(1)
	}()
	go func() {
		(pointer.Add)(1)
	}()
	go func() {
		nothing.Add(1)
	}()
	go func() {
		number.Add(1)
	}()
	go func() {
		add := pointer.Add
		add(1)
	}()
}

func nested(wg *sync.WaitGroup) {
	go func() {
		go func() {
			wg.Add(1)
		}()
	}()
}

func generic[T any](wg *sync.WaitGroup, value T) T {
	go func() {
		wg.Add(1)
	}()
	return value
}
