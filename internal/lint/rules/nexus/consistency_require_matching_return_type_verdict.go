package nexus

import "github.com/system-inc/cohere/policy"

// consistencyRequireMatchingReturnTypeVerdict is what one function's returns are judged against.
type consistencyRequireMatchingReturnTypeVerdict struct {
	kind consistencyRequireMatchingReturnTypeKind

	// functionKind names the function in the message: "function", "async function", "method",
	// "callback", "getter", "generator", "constructor", "setter".
	functionKind string

	// rendered and source describe a value verdict's type and where it came from.
	rendered string
	source   policy.MessageOption

	// voidReason says why a void verdict is void, as a clause, and written is the type it names when
	// the reason is a `void` type rather than a constructor or a setter.
	voidReason policy.MessageOption
	written    string

	// admitsUndefined is whether `undefined` is a member of a value verdict's type, which is what
	// makes the bare-return fix type-safe as well as behaviour-safe.
	admitsUndefined bool
}

// voidValues are the values the undefined-return message takes: the function's kind, and the type its
// reason names when the reason is a `void` type.
func (verdict consistencyRequireMatchingReturnTypeVerdict) voidValues() map[string]string {
	values := map[string]string{"functionKind": verdict.functionKind}
	if verdict.written != "" {
		values["written"] = verdict.written
	}
	return values
}
