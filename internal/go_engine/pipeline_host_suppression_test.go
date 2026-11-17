package go_engine

import (
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_else_return"
	"github.com/system-inc/cohere/internal/go_engine/suppression"
)

// splitFile is a file every build of the other-builds fixture compiles, with two elses after a return, the first
// ignored with a reason and the second not, so the host's own suppression keeps one finding and withholds one.
const splitFile = `package separator

// Head is the part of path before the separator, all of it when it has none.
func Head(path string, at int) string {
	if at < 0 {
		return path
		//cohere:ignore ` + consistency_no_else_return.Name + ` the planted finding the host's suppression withholds
	} else {
		return path[:at]
	}
}

// Tail is the part of path after the separator, none of it when it has none.
func Tail(path string, at int) string {
	if at < 0 {
		return ""
	} else {
		return path[at+1:]
	}
}
`

// The host's lint pass applies the //cohere:ignore comments to its own findings only when something reads them: never
// in a run whose builds merge, which suppresses the host's findings again (outcomeOf), and once in a run with no
// builds or whose builds failed, which writes exactly that suppression, Tail's finding kept and Head's withheld, or in
// a fixing run, whose rename reads it, and whose lint phase with no builds reads that same outcome again.
func TestTheHostsSuppressionIsComputedOnlyWhenNoMergeReplacesIt(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name      string
		arguments []string
		failBuild bool
		wantCount int32
	}{
		{name: "builds merged", arguments: []string{"--no-fix", "--other-builds"}, wantCount: 0},
		{name: "no builds", arguments: []string{"--no-fix"}, wantCount: 1},
		{name: "builds failed", arguments: []string{"--no-fix", "--other-builds"}, failBuild: true, wantCount: 1},
		// A fixing run's rename reads the host's kept findings for the renames they ask, before any build runs.
		{name: "builds merged in a fixing run", arguments: []string{"--other-builds"}, wantCount: 1},
		// The rename reads it, asks nothing, and the lint phase reads the same pass again: the second read is the
		// first's, never computed twice.
		{name: "no builds in a fixing run", wantCount: 1},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			root := copyFixture(test, filepath.Join("testdata", "other-builds"))
			writeFixtureFiles(test, root, map[string]string{"internal/separator/split.go": splitFile})
			var suppressed atomic.Int32
			engine, stream := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
				engine.hostSuppressed = func() { suppressed.Add(1) }
				if each.failBuild {
					engine.extraBuildEnds = func(project.Build, io.Writer) error {
						return errors.New("the planted failure")
					}
				}
			}, append([]string{"--no-cache"}, each.arguments...)...)
			if got := suppressed.Load(); got != each.wantCount {
				test.Errorf("the host's suppression was computed %d times, want %d", got, each.wantCount)
			}
			if each.failBuild && engine.buildRuns != nil {
				test.Fatalf("the merge was handed %d runs, want none: every build failed", len(engine.buildRuns))
			}
			lint := lintRecordOf(test, stream)
			if lint.WithheldBySuppression[consistency_no_else_return.Name] != 1 {
				test.Errorf("the run withheld %v, want Head's one %s", lint.WithheldBySuppression,
					consistency_no_else_return.Name)
			}
			var inSplit []int
			for line := range strings.Lines(stream) {
				if !strings.HasPrefix(line, `{"kind":"finding"`) {
					continue
				}
				var finding contract.FindingRecord
				if decodeError := json.Unmarshal([]byte(line), &finding); decodeError != nil {
					test.Fatal(decodeError)
				}
				if finding.Rule == consistency_no_else_return.Name && strings.HasSuffix(finding.File, "split.go") {
					inSplit = append(inSplit, finding.Line)
				}
			}
			// Tail's else is on line 17 of splitFile.
			if len(inSplit) != 1 || inSplit[0] != 17 {
				test.Errorf("the run wrote %s on lines %v of split.go, want Tail's alone, on line 17",
					consistency_no_else_return.Name, inSplit)
			}
		})
	}
}

// A run whose builds merge indexes each file's //cohere:ignore comments once, however many builds compile it
// (#qqyq61s): every build's outcome reads the run's one index of the bytes it holds, rather than each build indexing
// every file it compiles again. Its findings are the same as before: Head's withheld, Tail's kept.
func TestARunWhoseBuildsMergeIndexesEachFileOnce(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	writeFixtureFiles(test, root, map[string]string{"internal/separator/split.go": splitFile})
	var lock sync.Mutex
	builtByFile := map[string]int{}
	built := 0
	engine, stream := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.suppressionIndexes.built = func(file string) {
			lock.Lock()
			defer lock.Unlock()
			builtByFile[file]++
			built++
		}
	}, "--no-cache", "--no-fix", "--other-builds")
	if len(engine.buildRuns) < 2 {
		test.Fatalf("the merge was handed %d runs, want the host's and at least one other", len(engine.buildRuns))
	}
	if built != len(builtByFile) {
		test.Errorf("the run indexed %d times over %d files, want each once: %v", built, len(builtByFile), builtByFile)
	}
	if builtByFile[filepath.Join(root, "internal", "separator", "split.go")] != 1 {
		test.Errorf("the run indexed split.go, which more than one build has a say on, %d times, want once: %v",
			builtByFile[filepath.Join(root, "internal", "separator", "split.go")], builtByFile)
	}
	lint := lintRecordOf(test, stream)
	if lint.WithheldBySuppression[consistency_no_else_return.Name] != 1 {
		test.Errorf("the run withheld %v, want Head's one %s", lint.WithheldBySuppression,
			consistency_no_else_return.Name)
	}
}

// A file the run holds new bytes of is indexed again, since its directives' lines may have moved: the rename's
// rewrite and a reread after the fix phase writes each replace the bytes, and the index of the old bytes would cover
// the wrong line. The same bytes read again are the index already built.
func TestTheRunsSuppressionIndexFollowsTheBytesItHolds(test *testing.T) {
	test.Parallel()
	ruleName := consistency_no_else_return.Name
	first := []byte("package moved\n\n//cohere:ignore " + ruleName + " the planted reason\nvar First = 1\n")
	moved := []byte("package moved\n\n// Second is first's line now.\n//cohere:ignore " + ruleName +
		" the planted reason\nvar First = 1\n")
	var indexes suppressionIndexes
	coveredLine := func(index *suppression.Index) int {
		keepings := index.Keepings(ruleName)
		if len(keepings) != 1 {
			test.Fatalf("the index keeps %d lines for %s, want one", len(keepings), ruleName)
		}
		return keepings[0].CoveredLine
	}
	firstIndex := indexes.of("moved.go", first)
	if again := indexes.of("moved.go", first); again != firstIndex {
		test.Errorf("the same bytes read again were indexed again, want the index already built")
	}
	if got := coveredLine(firstIndex); got != 4 {
		test.Errorf("the first bytes' directive covers line %d, want 4", got)
	}
	if got := coveredLine(indexes.of("moved.go", moved)); got != 5 {
		test.Errorf("the moved bytes' directive covers line %d, want 5: the index of the bytes the run held before "+
			"was read", got)
	}
}
