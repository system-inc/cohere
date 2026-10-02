package configuration_test

import (
	"os"
	"testing"

	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/registry"
)

// BenchmarkLoadForTheLiveConfig loads ahra's real config against the real registry, which is what every
// run pays before its first phase. It was 590ms when every pair of keys rescanned the registry, and is
// about 7ms with each key asked once (#kgv1pry).
func BenchmarkLoadForTheLiveConfig(b *testing.B) {
	const path = "/Users/kirkouimet/Projects/ahra/CohereSettings.json"
	if _, err := os.Stat(path); err != nil {
		b.Skipf("the live config is not present at %s", path)
	}
	names := registry.Names()
	for b.Loop() {
		if _, err := configuration.LoadFor(path, names); err != nil {
			b.Fatal(err)
		}
	}
}
