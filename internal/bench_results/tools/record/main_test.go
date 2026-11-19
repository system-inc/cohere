package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// runsRow is one row of quiet_machine.sh's runs table for mode, with builds as its last column.
func runsRow(mode string, round int, builds string) string {
	return strings.Join([]string{mode, string(rune('0' + round)), "1.900", "2.000", "1.850", "0", "off", "3.1", "3.4",
		"95.0", "93.0", "quiet", "0", "0", builds}, "\t")
}

// The record names the builds the cold and idle runs checked, which must be one set: a warm run's own, here none from
// a replay, is never read, and a cold run that checked other builds than an earlier one is refused (#ccqee0e).
func TestReadRunsKeepsTheBuildsTheColdRunsChecked(test *testing.T) {
	test.Parallel()
	every := "darwin/arm64 cgo; linux/arm64 tags a,b"
	for name, testCase := range map[string]struct {
		rows    []string
		builds  []string
		refusal string
	}{
		"every cold run the same": {
			rows:   []string{runsRow("warm", 1, ""), runsRow("cold", 1, every), runsRow("cold", 2, every)},
			builds: []string{"darwin/arm64 cgo", "linux/arm64 tags a,b"},
		},
		"a host-only run names none": {
			rows: []string{runsRow("cold", 1, ""), runsRow("cold", 2, "")},
		},
		"two cold runs disagree": {
			rows:    []string{runsRow("cold", 1, every), runsRow("cold", 2, "darwin/arm64 cgo")},
			refusal: "and an earlier one",
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			path := filepath.Join(test.TempDir(), "runs.tsv")
			contents := []byte(strings.Join(testCase.rows, "\n") + "\n")
			if writeError := os.WriteFile(path, contents, 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			runs, builds, readError := readRuns(path)
			if testCase.refusal != "" {
				if readError == nil || !strings.Contains(readError.Error(), testCase.refusal) {
					test.Fatalf("read %d runs and the builds %q with %v, want a refusal saying %q", len(runs), builds,
						readError, testCase.refusal)
				}
				return
			}
			if readError != nil {
				test.Fatal(readError)
			}
			if len(runs) != len(testCase.rows) || !slices.Equal(builds, testCase.builds) {
				test.Errorf("read %d runs and the builds %q, want %d and %q", len(runs), builds, len(testCase.rows),
					testCase.builds)
			}
		})
	}
}
