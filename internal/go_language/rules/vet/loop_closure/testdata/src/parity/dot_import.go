package parity

import (
	. "golang.org/x/sync/errgroup"
)

// errgroup's Group through a dot import is still errgroup's.
func dotImported(values []int) {
	var group Group
	for _, value := range values {
		group.Go(func() error {
			use(value) // want "loop variable value captured by func literal"
			return nil
		})
	}
}
