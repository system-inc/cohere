// Package audit imports dep alone, in every build. dep imports context in every build but the audit_b tag's, so the
// packages audit reaches through its imports differ there though its own imports do not.
package audit

import "example.com/audit/dep"

// Target names a local ctx.
func Target() int {
	ctx := dep.Value()
	return ctx
}
