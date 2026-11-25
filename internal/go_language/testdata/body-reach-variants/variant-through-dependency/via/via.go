// Package via folds audit's Enabled into a constant of its own. Windows builds it again for audit's tests, against
// audit's test variant, and holds it plain as well.
package via

import "example.com/audit"

// Enabled is audit's.
const Enabled = audit.Enabled
