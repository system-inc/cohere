// This file speaks of //go:build in a sentence, and of go:build lines, which constrains nothing.

package flagged

// With no //go:build line opening a comment, the file is no variant, and other is no suffix go/build reads.
type Timer struct { // want "`Timer` is the one type .* Name it `timer.go`.$"
	ticks int
}

func (timer *Timer) Tick() {
	timer.ticks++
}
