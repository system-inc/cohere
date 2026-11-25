package audit_test

import "example.com/audit"

// half is half of audit's Root2.
func half() float64 {
	return audit.Root2 / 2
}

// enabled is audit's Enabled.
func enabled() bool {
	return audit.Enabled
}
