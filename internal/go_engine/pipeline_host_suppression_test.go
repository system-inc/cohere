package go_engine

import (
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_else_return"
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
// a fixing run, whose rename reads it.
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
