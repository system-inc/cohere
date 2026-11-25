// Package differences imports fmt under another name alone, so the original's fix, which spells fmt, names nothing,
// and the port's spells the name the call of Sprintf is made by. Every finding here is the original's, and each
// record says how the port's fix differs, on the line the finding starts, its reason on the line above.
package differences

import (
	"errors"
	formatting "fmt"
)

func arguments(count int, text string, rows [][]any) {
	// errors.New of a string the code holds is no finding, and keeps errors imported once every fix is applied.
	_ = errors.New(text)
	// The fix's call holds Sprintf's own argument nodes, so format prints them from their positions, across lines and
	// without the comments among them, as the original's does, under the name formatting where it writes fmt.
	_ = errors.New(formatting.Sprintf( // port moves: should use fmt.Errorf(...) instead of errors.New(fmt.Sprintf(...))
		"%d %s",
		count,
		text,
	))
	// The original writes fmt.Errorf("%d", count), and the port formatting.Errorf("%d", count).
	_ = errors.New(formatting.Sprintf( // port moves: should use fmt.Errorf(...) instead of errors.New(fmt.Sprintf(...))
		"%d", // the format
		count /* the count */))
	// The original writes fmt.Errorf("%v", rows[len(rows)-1][:]), dropping the spread, and the port keeps it.
	_ = errors.New( // port moves: should use fmt.Errorf(...) instead of errors.New(fmt.Sprintf(...))
		formatting.Sprintf("%v", rows[len(rows)-1][:]...))
	// The original writes fmt.Errorf of a function literal's call, and the port formatting.Errorf.
	_ = errors.New( // port moves: should use fmt.Errorf(...) instead of errors.New(fmt.Sprintf(...))
		formatting.Sprintf("%v", func() int { return count }()))
}

func generic[Value any](value Value) error {
	// The original writes fmt.Errorf("%v", value), and the port formatting.Errorf("%v", value).
	return errors.New( // port moves: should use fmt.Errorf(...) instead of errors.New(fmt.Sprintf(...))
		formatting.Sprintf("%v", value))
}
