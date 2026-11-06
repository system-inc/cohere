package prettier

import "github.com/system-inc/cohere/internal/format/format_files"

// Enumerate walks a project root and returns the files this engine would format.
func (engine *Engine) Enumerate(root string) (format_files.Enumeration, error) {
	return format_files.Enumerate(root, engine.Handles)
}
