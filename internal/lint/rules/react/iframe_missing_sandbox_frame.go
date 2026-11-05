package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// iframeMissingSandboxFrame gathers, in member order, what an iframe's props say about where its
// document comes from, so the dangerous pair is silent only when the frame provably loads another
// origin.
//
// Order matters because React lets a later prop replace an earlier one: `src` written after a spread
// is the `src` that renders, and `src` written before one may not be.
type iframeMissingSandboxFrame struct {
	// sourceDocument is set by any `srcDoc`. An `srcdoc` document always takes the framing page's
	// origin, and it wins over `src` when both are present, so its presence alone settles the
	// question.
	sourceDocument bool

	// sourceSeen and sourceValue describe the last `src`. A nil value means present and unreadable,
	// as in a bare `<iframe src />` or `src={}`.
	sourceSeen  bool
	sourceValue *ast.Node

	// opaqueAfterSource is set by a spread, or any member this cannot name, that follows the last
	// `src`, or that appears when there is no `src` at all. Either way the rendered `src` is unknown.
	opaqueAfterSource bool
}

func (frame *iframeMissingSandboxFrame) source(value *ast.Node) {
	frame.sourceSeen = true
	frame.sourceValue = value
	frame.opaqueAfterSource = false
}

func (frame *iframeMissingSandboxFrame) opaque() {
	frame.opaqueAfterSource = true
}

// provablyCrossOrigin answers whether the framed document provably loads an origin other than the
// framing page's. Only a yes keeps the pair silent, so every shape this cannot read reports: a
// `srcDoc` and an absent `src` (`about:blank`) are the page's own origin, and a bare `src` or a
// spread after the last one leaves the rendered `src` unknown.
func (frame *iframeMissingSandboxFrame) provablyCrossOrigin(ruleContext rule.Context) bool {
	if frame.sourceDocument || frame.opaqueAfterSource || !frame.sourceSeen {
		return false
	}
	return iframeMissingSandboxValueIsCrossOrigin(ruleContext, frame.sourceValue, true)
}
