package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// mappedFindingCollector reports a finding at the raw offsets its cooked span maps to.
type mappedFindingCollector struct {
	ruleContext rule.Context
	bodyStart   int
	offsets     []int
}

func (collector *mappedFindingCollector) report(finding pendingMisleadingFinding) {
	if finding.start >= len(collector.offsets) || finding.end >= len(collector.offsets) {
		return
	}
	collector.ruleContext.ReportRange(
		core.NewTextRange(
			collector.bodyStart+collector.offsets[finding.start],
			collector.bodyStart+collector.offsets[finding.end]),
		finding.message)
}
