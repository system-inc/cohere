// Package converted copies a Source into a Target field by field.
package converted

import "example.com/fieldwalks/platform"

// Convert copies source.
func Convert(source *platform.Source) {
	copied := *source
	target := platform.Target{Name: copied.Name}
	_ = target
}
