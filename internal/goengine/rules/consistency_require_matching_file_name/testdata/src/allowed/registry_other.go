//go:build !plan9

package allowed

// A build-constrained file is one variant of its type, so the word after the type's name is the variant's, as
// registry_unix.go and registry_other.go split one type by build tag.
type Registry struct {
	entries map[string]int
}

func (registry *Registry) Add(name string) {
	registry.entries[name]++
}
