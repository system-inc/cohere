// Package errgroup sits at errgroup's path, where the original reads a Group's Go, and its Group's Go takes any number
// of functions, so a Go given none is a call the original indexes the first argument of and panics on.
package errgroup

// A Go given no function launches nothing, so nothing in the loop can outlive its iteration, and the port, finding
// nothing, is right.
// port finds nothing

type Group struct{}

func (*Group) Go(functions ...func() error) {}

func launch(values []int, group *Group) {
	for _, value := range values {
		print(value)
		group.Go()
	}
}
