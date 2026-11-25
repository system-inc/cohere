package audit_test

import "example.com/audit/via"

// enabled is via's Enabled, which on windows is the one built against audit's test variant.
func enabled() bool {
	return via.Enabled
}
