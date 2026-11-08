// Package field reads a field holder declares through provider's variable, and a field is no package-level object, so
// the index holds holder, which this package never imports.
package field

import "indexed/provider"

const Value = 1

func count() int {
	return provider.Shared.Count
}
