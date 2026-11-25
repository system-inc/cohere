// Package indirect reaches WaitGroup's Add only through counted and never imports sync itself, so the original skips
// it whole: its first check is that the package imports sync directly.
package indirect

import "counted"

func start(holder *counted.Holder) {
	go func() {
		counted.Group.Add(1)
	}()
	go func() {
		holder.Add(1)
	}()
}
