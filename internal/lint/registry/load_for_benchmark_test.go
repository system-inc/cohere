package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/lint/configuration"
)

// BenchmarkLoadForTheLiveConfig loads a real project's config against the real registry, which is what
// every run pays before its first phase. It was 590ms on ahra when every pair of keys rescanned the
// registry, and is about 7ms with each key asked once (#kgv1pry).
//
// It lives here rather than beside LoadFor because it needs the whole rule set, and every test binary that
// links the rule set relinks on every rule edit: in configuration it cost each rule edit a link and a
// second compile of the registry (#6gct10n).
//
// The config is COHERE_BENCHMARK_CONFIG when set, and otherwise ahra's beside this checkout's parent, so
// the benchmark names no one's home directory and runs on any machine that has a config to load.
func BenchmarkLoadForTheLiveConfig(b *testing.B) {
	path := os.Getenv("COHERE_BENCHMARK_CONFIG")
	if path == "" {
		path = filepath.Join("..", "..", "..", "..", "..", "ahra", "CohereSettings.json")
	}
	if _, err := os.Stat(path); err != nil {
		b.Skipf("no config to load at %s; set COHERE_BENCHMARK_CONFIG", path)
	}
	names := Names()
	for b.Loop() {
		if _, err := configuration.LoadFor(path, names); err != nil {
			b.Fatal(err)
		}
	}
}
