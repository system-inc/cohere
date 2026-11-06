// Package lookalike holds shapes that look like a buffered WaitDelay and are not exec's, which the rule must leave
// alone.
package lookalike

import (
	"bytes"
	"io"
	"time"
)

// A field named WaitDelay on some other type is not exec's, whatever its neighbors are called.
type lookalike struct {
	WaitDelay time.Duration
	Stdout    io.Writer
}

func (lookalike) Output() ([]byte, error) { return nil, nil }

func notExec() lookalike {
	var output bytes.Buffer
	fake := lookalike{WaitDelay: time.Second}
	fake.WaitDelay = time.Second
	fake.Stdout = &output
	_, _ = fake.Output()
	return fake
}

// A struct that embeds a lookalike with the same field names is no exec.Cmd either.
type wrapper struct {
	lookalike
}

func throughAnEmbeddedLookalike() *wrapper {
	var output bytes.Buffer
	wrapped := &wrapper{}
	wrapped.WaitDelay = time.Second
	wrapped.Stdout = &output
	return wrapped
}
