// Package lookalike holds calls shaped like a dropped error that drop none, which the rule must leave alone.
package lookalike

// door has a Close that returns nothing, so closing it drops nothing.
type door struct{}

func (door) Close() {}

// notice has an Error method of the wrong shape, so it is no error.
type notice struct{}

func (notice) Error() int { return 0 }

func announce() notice { return notice{} }

// Println here is this package's own and returns nothing.
func Println(text string) {}

// errors returns many errors in a slice, which is not itself an error.
func errors() []error { return nil }

// cleanup returns a function that would return an error, and dropping the function drops no error.
func cleanup() func() error { return func() error { return nil } }

func failures() chan error { return nil }

func anything() any { return nil }

func calls() {
	door{}.Close()
	defer door{}.Close()
	announce()
	_ = announce()
	Println("text")
	errors()
	_ = errors()
	cleanup()
	defer cleanup()
	failures()
	anything()
	_, _ = anything(), anything()
	// Within this function, error is another type, which the rule reads by its type and not its name.
	type error = int
	shadowed := func() error { return 0 }
	shadowed()
	_ = shadowed()
}
