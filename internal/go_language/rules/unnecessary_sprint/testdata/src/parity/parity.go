package parity

import (
	"fmt"
	renamed "fmt"

	lookalike "parity/fmt"
)

type printer struct{}

func (printer) Sprint(values ...any) string { return "" }

func (printer) Sprintf(format string, values ...any) string { return "" }

// Sprint is the package's own, which the query never names.
func Sprint(values ...any) string { return "" }

const constant = "foo"

func reported() {
	_ = fmt.Sprint("foo")
	_ = fmt.Sprintf("foo")
	_ = fmt.Sprint(`raw`)
	_ = fmt.Sprintf(`raw`)
	_ = fmt.Sprint("")

	// Sprint reads no format, so a percent sign is no reason to leave it alone.
	_ = fmt.Sprint("%d")

	// The original reads the literal as written, and an escape that spells a percent sign holds none.
	_ = fmt.Sprintf("\x25d")
	_ = fmt.Sprintf("\045s")

	// The pattern sees through parentheses around the callee and the literal, and the fix is the literal alone.
	_ = fmt.Sprint(("foo"))
	_ = (fmt.Sprint)("foo")
	_ = (fmt.Sprintf)(("foo"))

	_ = renamed.Sprint("renamed")
	_ = renamed.Sprintf("renamed")

	// The finding and the fix span the whole call, across its lines and comments.
	_ = fmt.Sprint(
		"across lines",
	)
	_ = fmt.Sprintf( /* before */ "commented" /* after */)

	// Only the inner call is given a literal.
	_ = fmt.Sprint(fmt.Sprint("nested"))
	_ = len(fmt.Sprintf("argument"))
	fmt.Sprint("statement")
	go fmt.Sprint("goroutine")
	defer fmt.Sprintf("deferred")
}

func untouched(text string, values []any) {
	// A percent sign may make a format, written or raw, doubled or not.
	_ = fmt.Sprintf("%d")
	_ = fmt.Sprintf(`%`)
	_ = fmt.Sprintf("100%%")
	_ = fmt.Sprintf("foo %d", 1)

	// No string literal: a variable, a constant, a concatenation, a conversion, a rune, a number.
	_ = fmt.Sprint(text)
	_ = fmt.Sprint(constant)
	_ = fmt.Sprint("a" + "b")
	_ = fmt.Sprint(string("conversion"))
	_ = fmt.Sprint('r')
	_ = fmt.Sprint(1)
	_ = fmt.Sprint([]any{"spread"}...)
	_ = fmt.Sprint(values...)

	// Not one argument.
	_ = fmt.Sprint()
	_ = fmt.Sprint("two", "arguments")
	_ = fmt.Sprintf("format", "argument")

	// Not fmt.Sprint or fmt.Sprintf.
	_ = fmt.Sprintln("line")
	_ = fmt.Errorf("error")
	var value printer
	_ = value.Sprint("method")
	_ = value.Sprintf("method")
	_ = Sprint("own")
	_ = lookalike.Sprint("lookalike")
	_ = lookalike.Sprintf("lookalike")

	// A function value is no Symbol, though it holds fmt.Sprint.
	sprint := fmt.Sprint
	_ = sprint("value")
}
