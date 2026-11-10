// Package our_tiers says whether code is ours, where the house holds it to more than it holds an outside project:
// inside our tiers every off says why (#v11k0g2), and outside them a project's own choices stand. TypeScript reads it
// from a settings chain that reaches one of our sets (internal/lint/configuration.InOurTiers). Go has no set to
// extend, so a Go module is ours by its path, which every reader of the question takes from here, never spelling the
// prefix again.
package our_tiers

import "strings"

// ModulePrefix opens the path of every Go module that is ours: cohere's, adamic's and loom's among them.
const ModulePrefix = "github.com/system-inc/"

// OwnsModule reports whether the Go module at modulePath, as its go.mod names it, is one of ours.
func OwnsModule(modulePath string) bool {
	return strings.HasPrefix(modulePath, ModulePrefix)
}
