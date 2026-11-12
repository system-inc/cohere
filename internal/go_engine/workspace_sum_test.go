package go_engine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// staleWorkspaceFiles is a workspace whose go.work.sum is stale: a requires example.com/dep, whose go.mod's sum
// go.work.sum lacks and no go.sum holds, so go list -m all, the lister's, writes it there, as go writes a workspace's
// sums whatever -mod says. a has a file only windows builds, so `--other-builds` checks a second build.
var staleWorkspaceFiles = map[string]string{
	"go.work":        "go 1.21\n\nuse ./a\n",
	"go.work.sum":    "example.com/stale v1.0.0/go.mod h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n",
	"a/go.mod":       "module example.com/a\n\ngo 1.21\n\nrequire example.com/dep v1.0.0\n",
	"a/a.go":         "// Package a is the workspace's one package.\npackage a\n\n// Value is one.\nconst Value = 1\n",
	"a/a_windows.go": "package a\n\n// Separator is windows's.\nconst Separator = `\\`\n",
}

// writeStaleWorkspace writes the stale workspace, with extra, below a directory of the test's, and a module proxy
// beside it that serves example.com/dep from disk, and has every go command of the run read that proxy, no checksum
// database, and a module cache of the test's own. It returns the workspace.
func writeStaleWorkspace(test *testing.T, extra map[string]string) string {
	test.Helper()
	directory := test.TempDir()
	workspace := filepath.Join(directory, "workspace")
	writeFixtureFiles(test, workspace, staleWorkspaceFiles)
	writeFixtureFiles(test, workspace, extra)
	proxy := filepath.Join(directory, "proxy")
	writeFixtureFiles(test, filepath.Join(proxy, "example.com", "dep", "@v"), map[string]string{
		"list":        "v1.0.0\n",
		"v1.0.0.info": "{\"Version\":\"v1.0.0\",\"Time\":\"2026-01-01T00:00:00Z\"}\n",
		"v1.0.0.mod":  "module example.com/dep\n\ngo 1.21\n",
	})
	test.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	test.Setenv("GOSUMDB", "off")
	test.Setenv("GOMODCACHE", filepath.Join(directory, "modules"))
	// The cache's files are written read-only otherwise, and the test's cleanup could not remove them.
	test.Setenv("GOFLAGS", "-modcacherw")
	test.Setenv("GOTOOLCHAIN", "local")
	test.Setenv("GOWORK", "")
	return workspace
}

// A check under `--other-builds` of a workspace whose go.work.sum is stale leaves it byte for byte as it was, under
// the host's build and the extra one's, its summary names no file go may have rewritten, and its inputs name the
// tree's go.work and go.work.sum, never the side copy's every go list ran beside, so a replay reads what it always
// read (#34r3v1y). Beside a vendor directory, where no side copy answers exactly, go list runs in the tree, rewrites
// the stale file, and the summary names it.
// Not parallel: it sets the go environment of the process, which every go command of the run in it reads
func TestACheckLeavesAStaleGoWorkSumAsItWasUnderEveryBuild(test *testing.T) {
	cases := []struct {
		name      string
		extra     map[string]string
		rewritten bool
	}{
		{name: "a workspace"},
		{name: "a workspace with a vendor directory", extra: map[string]string{"vendor/README": "vendored\n"},
			rewritten: true},
	}
	for _, each := range cases {
		// Not parallel: it sets the go environment of the process, which every go command of the run in it reads
		test.Run(each.name, func(test *testing.T) {
			workspace := writeStaleWorkspace(test, each.extra)
			engine, stream := runOverRaw(test, workspace, exitClean, "--no-fix", "--other-builds")
			if len(engine.buildRuns) != 2 {
				test.Errorf("the run checked %d builds, want the host's and windows's", len(engine.buildRuns))
			}
			after, readError := os.ReadFile(filepath.Join(workspace, "go.work.sum"))
			if readError != nil {
				test.Fatal(readError)
			}
			if same := bytes.Equal(after, []byte(staleWorkspaceFiles["go.work.sum"])); same == each.rewritten {
				test.Errorf("go.work.sum after the run, rewritten %t:\n%s", each.rewritten, after)
			}
			var summary contract.SummaryRecord
			var inputs string
			for line := range strings.Lines(stream) {
				switch {
				case strings.HasPrefix(line, `{"kind":"summary"`):
					if decodeError := json.Unmarshal([]byte(line), &summary); decodeError != nil {
						test.Fatal(decodeError)
					}
				case strings.HasPrefix(line, `{"kind":"inputs"`):
					inputs = line
				}
			}
			var want []string
			if each.rewritten {
				want = []string{filepath.Join(physical(test, workspace), "go.work.sum")}
			}
			if !slices.Equal(summary.UnguardedWorkspaceSums, want) {
				test.Errorf("the summary names %q as go may have rewritten, want %q", summary.UnguardedWorkspaceSums,
					want)
			}
			if !summary.Complete {
				test.Errorf("the summary calls the run incomplete, and every file was checked")
			}
			if strings.Contains(inputs, "cohere-go-work-") || !strings.Contains(inputs, "/go.work.sum") {
				test.Errorf("the inputs name a side copy, or no go.work.sum: %s", inputs)
			}
		})
	}
}

// physical is directory with every symbolic link resolved, as go env spells the go.work it finds.
func physical(test *testing.T, directory string) string {
	test.Helper()
	resolved, resolveError := filepath.EvalSymlinks(directory)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	return resolved
}
