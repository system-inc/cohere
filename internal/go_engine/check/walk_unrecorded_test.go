package check

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// The unrecorded fixture's packages: alone imports nothing, changes declares Size its own way on windows, and leans
// reads it.
const (
	unrecordedAlone = "example.com/unrecorded/alone"
	unrecordedLeans = "example.com/unrecorded/leans"
)

// unrecordedFiles are the unrecorded fixture's module.
var unrecordedFiles = map[string]string{
	"go.mod":                     "module example.com/unrecorded\n\ngo 1.24\n",
	"alone/alone.go":             "// Package alone imports nothing.\npackage alone\n\n// One is one.\nfunc One() int { return 1 }\n",
	"changes/changes_other.go":   "//go:build !windows\n\npackage changes\n\n// Size is a size.\nconst Size = 1\n",
	"changes/changes_windows.go": "package changes\n\n// Size is a size, another on windows.\nconst Size = 2\n",
	"leans/leans.go": "// Package leans reads changes.\npackage leans\n\n" +
		"import \"example.com/unrecorded/changes\"\n\n" +
		"// Twice is twice changes' Size.\nfunc Twice() int { return changes.Size * 2 }\n",
}

// A package the host kept with no reads a verdict can judge, never recorded or never persisted (unrestorableReads),
// is left untouched by the walk when its whole import closure misses what changed, on the walk's argument for it, not
// a verdict's: its own inputs are the host's and nothing it could reach changed, so its findings, facts and answers
// are the host's whatever it read. When its closure meets what changed it is visited, and declined as before, its
// reads being none a verdict can judge: alone, which imports nothing, is untouched on windows, and leans, which reads
// changes, is visited and declined.
func TestAPackageWithoutReadsIsUntouchedOnlyWhenItsClosureIsClean(test *testing.T) {
	test.Parallel()

	for _, step := range []struct {
		name      string
		withdrawn func(kept *hostEntry)
		reason    string
	}{
		{"reads never recorded", func(kept *hostEntry) { kept.restoring.Reads = &build_reads.Reads{} }, "readsUnknown"},
		{"reads never persisted", func(kept *hostEntry) { kept.restoring.Reads = nil }, "readsNotPersisted"},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			root := test.TempDir()
			writeFiles(test, root, unrecordedFiles)
			stores := test.TempDir()
			host := throughHost(test, root, filepath.Join(stores, "host"))
			for _, path := range []string{unrecordedAlone, unrecordedLeans} {
				kept := host.entries[path]
				if kept == nil {
					test.Fatalf("the host kept no entry of %s", path)
				}
				step.withdrawn(kept)
			}
			built, _ := throughBuild(test, root, filepath.Join(stores, "build"), host)
			fromHost := built.FromHost
			if !slices.Contains(fromHost.Untouched, unrecordedAlone) {
				test.Errorf("windows left %q untouched, want alone among them", fromHost.Untouched)
			}
			if slices.Contains(fromHost.Untouched, unrecordedLeans) || slices.Contains(fromHost.Restored,
				unrecordedLeans) || fromHost.DeclinedBy[step.reason] == 0 {
				test.Errorf("windows left %q untouched and restored %q, declining %v, want leans declined as %s",
					fromHost.Untouched, fromHost.Restored, fromHost.DeclinedBy, step.reason)
			}
		})
	}
}
