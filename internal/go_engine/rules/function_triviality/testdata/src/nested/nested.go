// Package nested holds a function literal called in place inside a function literal called in place, for the test
// that an answer names the declaration enclosing a literal however deep the literal is.
package nested

// Asked is trivial through twice.
func Asked() error { return twice() }

// twice calls a function literal in place, which calls another in place, which calls leaf. Both literals are named by
// twice's own declaration, and leaf is reached only through the inner one.
func twice() error {
	func() {
		func() { leaf() }()
	}()
	return nil
}

func leaf() {}
