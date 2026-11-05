package core

import (
	"fmt"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// PreserveCaughtErrorClassName is one entry of `errorClassNames`, in either of its two spellings.
type PreserveCaughtErrorClassName struct {
	Name string

	// ArgumentPosition is one-based, as ESLint's schema writes it: 2 means the second argument.
	ArgumentPosition int
}

// UnmarshalJSON accepts a bare string or a `{ name, argumentPosition }` object, which is ESLint's
// `oneOf`.
//
// The object is read strictly, which is ESLint's `additionalProperties: false`, and both keys are
// required. A position below 1 is refused rather than clamped: it would index the arguments at -1,
// and ESLint's schema sets `minimum: 1` for the same reason.
func (entry *PreserveCaughtErrorClassName) UnmarshalJSON(raw []byte) error {
	var name string
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &name); unmarshalOptionsError == nil {
		entry.Name = name
		entry.ArgumentPosition = 2
		return nil
	}
	var object struct {
		Name             *string `json:"name"`
		ArgumentPosition *int    `json:"argumentPosition"`
	}
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &object); unmarshalOptionsError != nil {
		return unmarshalOptionsError
	}
	if object.Name == nil || object.ArgumentPosition == nil {
		return fmt.Errorf("an errorClassNames object needs both name and argumentPosition")
	}
	if *object.ArgumentPosition < 1 {
		return fmt.Errorf(
			"errorClassNames argumentPosition is %d, and the first argument is 1",
			*object.ArgumentPosition,
		)
	}
	entry.Name = *object.Name
	entry.ArgumentPosition = *object.ArgumentPosition
	return nil
}
