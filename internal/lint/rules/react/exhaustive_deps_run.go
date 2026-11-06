package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	esregexp "github.com/system-inc/cohere/internal/lint/ecmascript/regexp"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// exhaustiveDepsRun is the options, read once per file into the shape the visitors use.
type exhaustiveDepsRun struct {
	additionalHooks           *esregexp.RegExp
	autoDependenciesHooks     map[string]bool
	dangerousAutofix          bool
	requireExplicitEffectDeps bool
}

// report reports a finding, carrying its repair as a fix only under the dangerous option.
//
// Without the option a repair is not offered here at all, as before, because the message already
// says what to write and this rule never applies one unattended.
func (run exhaustiveDepsRun) report(ruleContext rule.Context, node *ast.Node, message rule.Message, fixes ...rule.Fix) {
	if run.dangerousAutofix && len(fixes) > 0 {
		ruleContext.ReportNodeWithFixes(node, message, fixes...)
		return
	}
	ruleContext.ReportNode(node, message)
}
