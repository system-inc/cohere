package staticcheck_code

import "github.com/system-inc/cohere/internal/go_engine/rule"

// cobraPath is the import path of the command framework whose importers IsMainLike counts as commands.
const cobraPath = "github.com/spf13/cobra"

// IsMainLike is whether the context's package is main or, by importing cobra itself, likely implements a command, so
// a check can leave alone what is fine in a program and not in a library: staticcheck's code.IsMainLike. It errs, as
// the original does, toward saying yes, and counts only a direct import of cobra's own package, never a subpackage.
func IsMainLike(context *rule.Context) bool {
	if context.Package.Name() == "main" {
		return true
	}
	for _, imported := range context.Package.Imports() {
		if imported.Path() == cobraPath {
			return true
		}
	}
	return false
}
