package prettier

import "github.com/system-inc/cohere/internal/format/format_files"

// Enumerate walks a project root and returns the files the formatter would format.
func (formatter *Formatter) Enumerate(root string) (format_files.Enumeration, error) {
	return format_files.Enumerate(root, formatter.Handles)
}
