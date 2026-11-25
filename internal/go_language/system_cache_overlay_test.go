package go_language

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/rules"
)

// A run whose go flags name an overlay keeps nothing in the system cache and restores nothing from it (#57wgzhp): the
// overlay's files are what go list reads in place of the disk's, and a standard-library key holds the disk's bytes,
// never the overlay's. The overlay here is of strings.go, below `GOROOT`, which go takes and which the fixture reaches:
// with the system cache warm, a run would otherwise write analyses keyed by the flag and the disk's bytes, which the
// next run under the same flag restores whatever its overlay holds by then. The run says why it kept no system cache,
// and its findings are the run's without the overlay.
// Not parallel: it sets the go environment of the process, which every go command of the run in it reads
func TestARunWithAnOverlayKeepsNothingInTheSystemCache(test *testing.T) {
	root := copyFixture(test, standardLibraryFixture)
	source := standardLibrarySource{stamp: testsStamp, systemCacheRoot: test.TempDir()}
	runWith(test, root, source, rules.All(), "--no-cache")
	warm := runWith(test, root, source, rules.All(), "--no-cache")
	if warm.counts.FromSystemCache == 0 {
		test.Fatalf("want the system cache warm with the standard library, got %+v", warm.counts)
	}

	goroot := toolchainRoot()
	if goroot == "" {
		test.Fatal("go env named no GOROOT")
	}
	standardFile := filepath.Join(goroot, "src", "strings", "strings.go")
	contents, readError := os.ReadFile(standardFile)
	if readError != nil {
		test.Fatal(readError)
	}
	overlaid := filepath.Join(test.TempDir(), "strings.go")
	encoded, encodeError := json.Marshal(map[string]map[string]string{"Replace": {standardFile: overlaid}})
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	overlay := filepath.Join(test.TempDir(), "overlay.json")
	if writeError := os.WriteFile(overlay, encoded, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	test.Setenv("GOFLAGS", os.Getenv("GOFLAGS")+" -overlay="+overlay)
	// Twice, the overlay's bytes moved between, under the one flag: what one run kept would be the next one's to
	// restore, though its file is another.
	for _, appended := range []string{"\n// The overlay's first byte.\n", "\n// The overlay's second byte.\n"} {
		if writeError := os.WriteFile(overlaid, append(slices.Clone(contents), appended...), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		withOverlay, noted := runNoting(test, root, source, rules.All(), nil, "--no-cache")
		if withOverlay.counts.FromSystemCache != 0 || withOverlay.counts.SystemCacheWritten != 0 ||
			!strings.Contains(withOverlay.counts.SystemCacheNotKept, "-overlay") {
			test.Errorf("want nothing restored from the system cache or written to it under -overlay, and why, got "+
				"%+v", withOverlay.counts)
		}
		if !strings.Contains(noted, "-overlay") {
			test.Errorf("want the run to say why it kept no system cache, got stderr:\n%s", noted)
		}
		if !slices.Equal(findingLines(withOverlay.stream), findingLines(warm.stream)) {
			test.Errorf("under the overlay\n%s\nwithout it\n%s", withOverlay.stream, warm.stream)
		}
	}
}
