// Package audit imports dep alone, in every build. dep imports context under the audit_b tag alone, so the packages
// audit reaches through its imports differ there though its own imports do not.
package audit

import "example.com/audit/dep"

// Target names a local ctx.
func Target() int {
	ctx := dep.Value()
	return ctx
}
