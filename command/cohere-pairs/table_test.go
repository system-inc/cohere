package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateTable has the golden test write the table it renders over the golden, after a change to the table's shape.
var updateTable = flag.Bool("update", false, "write the table golden")

// tableGolden is the table of fixtureRecords, with tracked files changed (withChangedFiles), as it is posted.
var tableGolden = filepath.Join("testdata", "table.golden")

// The table of a pair is the golden's, byte for byte: the header, what each corpus agreed on with loom's one stderr
// shown, each corpus and mode's wall seconds, medians and round deltas, the medians of user seconds and peak resident
// memory, and each extra build's medians beside base's, absent for root's base, which has none.
func TestRenderTableMatchesTheGolden(test *testing.T) {
	test.Parallel()
	rendered := renderTable(tableHeader{
		base: builtBinary{
			side:      sideBase,
			commit:    "1111111111111111111111111111111111111111",
			sha256:    "0a4601b8ffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			tableLine: "go standard library table: release 0.2.0-measure.111111111111 1111 go1.27.3, 51234567 bytes",
		},
		branch: builtBinary{
			side:      sideBranch,
			commit:    "2222222222222222222222222222222222222222",
			sha256:    "bfa25050ffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			tableLine: "go standard library table: release 0.2.0-measure.222222222222 2222 go1.27.3, 51234999 bytes",
		},
		host:              "pupa",
		goVersion:         "go version go1.27.3 linux/amd64",
		goOperatingSystem: "linux",
		goArchitecture:    "amd64",
		cores:             64,
		rounds:            2,
	}, withChangedFiles(fixtureRecords()))
	if *updateTable {
		if writeError := os.WriteFile(tableGolden, []byte(rendered), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	golden, readError := os.ReadFile(tableGolden)
	if readError != nil {
		test.Fatal(readError)
	}
	if rendered != string(golden) {
		test.Errorf("the table is\n%s\nand the golden is\n%s\nrun with -update after a change meant to move it",
			rendered, golden)
	}
}

// The median of an odd count is its middle, of an even count the mean of its two middles, whatever order the values
// came in.
func TestMedianIsTheMiddle(test *testing.T) {
	test.Parallel()
	for _, middle := range []struct {
		values []float64
		want   float64
	}{
		{[]float64{3, 1, 2}, 2},
		{[]float64{4, 1, 3, 2}, 2.5},
		{[]float64{7}, 7},
	} {
		if got := median(middle.values); got != middle.want {
			test.Errorf("the median of %v is %v, want %v", middle.values, got, middle.want)
		}
	}
}
