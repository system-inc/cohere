package oracle_testing

import (
	"fmt"
	"sync"

	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// Formatter is a prettier.Formatter's Format, answered from a golden.
type Formatter struct {
	golden         *Golden
	options        format_options.Options
	once           sync.Once
	formatter      *prettier.Formatter
	formatterError error
}

// Format is the answer prettier.Formatter.Format gives for fileName and text under the formatter's options.
func (formatter *Formatter) Format(fileName string, text string) (string, error) {
	return formatter.golden.Answer(Key(
		// "engine" is the part every recorded answer was hashed under; spelling it anew would orphan every golden.
		"engine",
		fmt.Sprintf("%+v", formatter.options),
		fileName,
		text,
	), func() (string, error) {
		formatter.once.Do(func() { formatter.formatter, formatter.formatterError = prettier.New(formatter.options) })
		if formatter.formatterError != nil {
			return "", formatter.formatterError
		}
		return formatter.formatter.Format(fileName, text)
	})
}
