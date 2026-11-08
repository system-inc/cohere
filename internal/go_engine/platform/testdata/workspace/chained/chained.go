// Package chained imports importer, which is in the closure only as an importer, and whose export differs too.
package chained

import "example.com/workspace/importer"

// Four is importer's value twice.
func Four() int { return importer.Twice() * 2 }
