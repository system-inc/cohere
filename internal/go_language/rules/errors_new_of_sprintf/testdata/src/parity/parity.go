package parity

import (
	"errors"
	"fmt"
)

type maker struct{}

func (maker) New(text string) error { return nil }

func (maker) Sprintf(format string, arguments ...any) string { return "" }

// New and Sprintf are this package's own, spelled as errors' and fmt's are.
func New(text string) error { return nil }

func Sprintf(format string, arguments ...any) string { return "" }

var newFunction = errors.New

var sprintfFunction = fmt.Sprintf

func reported(count int, text string, format string) error {
	_ = errors.New(fmt.Sprintf("%d", count))
	_ = errors.New(fmt.Sprintf("plain"))
	_ = errors.New(fmt.Sprintf("%d %s", count, text))
	_ = errors.New(fmt.Sprintf(format, count, text))

	// The pattern sees through parentheses around the argument and around either function, and the finding spans
	// the call of errors.New as written.
	_ = errors.New((fmt.Sprintf("%d", count)))
	_ = (errors.New)(fmt.Sprintf("%d", count))
	_ = errors.New((fmt.Sprintf)("%d", count))
	_ = (errors.New)((fmt.Sprintf)("%d", count))

	// Each call is its own finding, the inner one within the outer.
	_ = errors.New(fmt.Sprintf("%v", errors.New(fmt.Sprintf("%d", count))))

	var declared = errors.New(fmt.Sprintf("%s", text))
	_ = declared
	return errors.New(fmt.Sprintf("%d", count))
}

func near(count int, text string, holder maker) {
	// errors.New of anything but one call of fmt.Sprintf is not the query's shape.
	_ = errors.New(text)
	_ = errors.New("plain")
	_ = errors.New(fmt.Sprint(count))
	_ = errors.New(fmt.Sprintln(count))
	_ = errors.New(fmt.Sprintf("%d", count) + text)
	_ = errors.New(string(fmt.Sprintf("%d", count)))
	_ = errors.New(text + fmt.Sprintf("%d", count))
	_ = fmt.Errorf("%d", count)
	_ = fmt.Sprintf("%v", errors.New(text))

	// A function or method named New or Sprintf that is not errors' or fmt's, and either held in a variable.
	_ = New(fmt.Sprintf("%d", count))
	_ = errors.New(Sprintf("%d", count))
	_ = holder.New(fmt.Sprintf("%d", count))
	_ = errors.New(holder.Sprintf("%d", count))
	_ = newFunction(fmt.Sprintf("%d", count))
	_ = errors.New(sprintfFunction("%d", count))
	localNew := errors.New
	_ = localNew(fmt.Sprintf("%d", count))
	localSprintf := fmt.Sprintf
	_ = errors.New(localSprintf("%d", count))
}
