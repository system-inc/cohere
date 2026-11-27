package oracle_testing

import (
	"fmt"

	"github.com/system-inc/cohere/internal/format/format_options"
)

// Formatter is the fork's formatter's Format, answered from a golden.
type Formatter struct {
	golden  *Golden
	options format_options.Options
}

// Format is the answer the fork's formatter gave for fileName and text under the formatter's options.
func (formatter *Formatter) Format(fileName string, text string) (string, error) {
	return formatter.golden.Answer(Key(
		// "engine" is the part every recorded answer was hashed under; spelling it anew would orphan every golden.
		"engine",
		fmt.Sprintf("%+v", formatter.options),
		fileName,
		text,
	))
}
