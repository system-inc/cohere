package canonical_function_hash

import (
	"github.com/system-inc/cohere/internal/duplicate_functions"
)

// refusal is why a function cannot be hashed, as an error, so a reason found deep in a nested function's walk
// reaches the function it belongs to through the wrapping.
type refusal struct {
	reason duplicate_functions.RefusalReason
	detail string
}

func (refused *refusal) Error() string {
	if refused.detail == "" {
		return string(refused.reason)
	}
	return string(refused.reason) + ": " + refused.detail
}

// refuse is a refusal for reason.
func refuse(reason duplicate_functions.RefusalReason, detail string) error {
	return &refusal{reason: reason, detail: detail}
}

// notHashable is an IR this hash cannot walk as it expects.
func notHashable(detail string) error {
	return refuse(duplicate_functions.RefusalNotHashable, detail)
}
