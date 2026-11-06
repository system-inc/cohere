package rule_testing

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// Result is what one rule saw in one file.
type Result struct {
	Diagnostics []rule.Diagnostic
	SourceFile  *ast.SourceFile

	// capture is the case the docs capture records when an assertion passes on this result; see
	// docs_capture.go. Nil unless COHERE_DOCS_CAPTURE is set.
	capture *capturedRun
}

// MessageIds returns the ids of every finding, in the order they were reported.
func (result Result) MessageIds() []string {
	ids := make([]string, 0, len(result.Diagnostics))
	for _, diagnostic := range result.Diagnostics {
		ids = append(ids, diagnostic.Message.Id)
	}
	return ids
}
