// Package platform makes a value whose layout every build shares, and declares one whose layout windows changes.
package platform

import "example.com/records/base"

// Plain is a count, alike in every build.
type Plain struct{ N base.Count }

// Locked holds a Lock, whose fields windows declares its own way.
type Locked struct{ L Lock }

// Make is a Plain.
func Make() Plain { return Plain{} }
