// Package measured compares a Measured with itself.
package measured

import "example.com/fieldwalks/platform"

// Same compares what measured points to with itself.
func Same(measured *platform.Measured) bool { return *measured == *measured }
