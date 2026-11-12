// Package basic ignores the result of a function whose purity hangs on Basic's fields.
package basic

import "example.com/fieldwalks/platform"

// doubled is twice count, pure while value holds basic values alone.
func doubled(value platform.Basic, count int) int { return count * 2 }

// Ignore calls doubled and drops its result.
func Ignore(value *platform.Basic) { doubled(*value, 3) }
