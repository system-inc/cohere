package registry

import (
	"testing"

	"github.com/system-inc/cohere/internal/settings"
)

// BenchmarkLoadForAProjectConfiguration loads a project's config against the real registry, which is what
// every run pays before its first phase. It was 590ms on ahra when every pair of keys rescanned the
// registry, and is about 7ms with each key asked once (#kgv1pry).
//
// It lives here rather than beside LoadFor because it needs the whole rule set, and every test binary that
// links the rule set relinks on every rule edit: in configuration it cost each rule edit a link and a
// second compile of the registry (#6gct10n).
//
// The config is the settings package's project fixture (projectConfigurationPath), the chain of sets a
// Structure app extends, which is the most of the load a project pays.
func BenchmarkLoadForAProjectConfiguration(benchmark *testing.B) {
	names := Names()
	for benchmark.Loop() {
		if _, loadForError := settings.LoadFor(projectConfigurationPath(), names); loadForError != nil {
			benchmark.Fatal(loadForError)
		}
	}
}
