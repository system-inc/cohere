// Package sizer uses Shared, whose method differs by build.
package sizer

import "example.com/restore/platform"

// Value is shared's value.
func Value(shared platform.Shared) int { return shared.Value }
