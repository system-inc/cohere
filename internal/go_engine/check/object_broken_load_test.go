package check

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// recordedReport is what a report of the object differential logged and each failure it reported, in its words, for a
// test of the report itself (objectReporter).
type recordedReport struct {
	logged []string
	failed []string
}

// Helper is what a test marks its helpers by, and a record's does nothing.
func (*recordedReport) Helper() {
	// A record names no file and line, so it has no caller to skip.
}

// Logf records one logged line.
func (report *recordedReport) Logf(format string, arguments ...any) {
	report.logged = append(report.logged, fmt.Sprintf(format, arguments...))
}

// Errorf records one failure.
func (report *recordedReport) Errorf(format string, arguments ...any) {
	report.failed = append(report.failed, fmt.Sprintf(format, arguments...))
}

// A package that loaded with errors in either build is one the object differential cannot compare, its types not
// being the build's, and a tree judged without it is judged in part: the report fails, naming how many such packages
// there are and the first of them with each one's first error, beside the counts of what it still compared, so a tree
// someone named and the test could not judge whole never reads as one that passed (#3096be2). In the broken_build
// fixture partial type-checks on darwin and not on linux, where the file only linux compiles declares an int from a
// string, unprovided is linux's alone and imports a path no module provides, a list error as a tree missing its
// modules loads with, never a type error, and whole type-checks in both: on linux whole is still compared, with no
// failure of its own, and the report fails once, naming the three packages linux holds with errors, partial,
// unprovided and the path it imports, each in the build, partial's error by its place and the path's as go list
// says it.
func TestAPackageLoadedWithErrorsFailsTheObjectDifferential(test *testing.T) {
	test.Parallel()

	directory := filepath.Join("testdata", "broken_build")
	host := loadObjects(test, directory, objectEnvironment("darwin", ""), "./...", true)
	built := loadObjects(test, directory, objectEnvironment("linux", ""), "./...", true)
	partial, unprovided, absent := "example.com/brokenbuild/partial", "example.com/brokenbuild/unprovided",
		"example.com/nowhere/absent"
	if want := []string{partial, unprovided, absent}; len(host.broken) > 0 || !slices.Equal(built.broken, want) {
		test.Fatalf("want the fixture whole on darwin and %q with errors on linux, got %q and %q", want, host.broken,
			built.broken)
	}
	tally := objectDifferences(host, built, objectDependence(host, built, objectFixtureWorkspace).ObjectBits)
	if tally.compared == 0 || len(tally.failures) > 0 {
		test.Errorf("want whole's objects compared with no failure of their own, got %d compared and the failures %q",
			tally.compared, tally.failures)
	}
	var report recordedReport
	reportObjectDifferences(&report, "linux", tally, time.Now())
	if len(report.failed) != 1 {
		test.Fatalf("want the report to fail once, for the package it left out, got %q", report.failed)
	}
	for _, words := range []string{
		"linux: 3 packages loaded with errors",
		partial + " in the build: ",
		"limit_linux.go",
		unprovided + " in the build: ",
		absent + " in the build: ",
		"no required module provides package " + absent,
	} {
		if !strings.Contains(report.failed[0], words) {
			test.Errorf("want the failure to say %q, got %q", words, report.failed[0])
		}
	}
	if len(report.logged) != 1 || !strings.Contains(report.logged[0], "packages with errors left out: 3") {
		test.Errorf("want the counts to say 3 packages with errors were left out, got %q", report.logged)
	}
}

// A package with errors is listed once, by the load it has them in, the host's, the build's or both, the host's
// first error where both have one (leftOutOf), and a report of more than it names (leftOutNamed) says how many there
// are and names the first of them: seven packages left out fail the report once, counting seven and naming five.
func TestAReportCountsEveryPackageLeftOutAndNamesTheFirstFew(test *testing.T) {
	test.Parallel()

	host := objectLoad{broken: []string{"a", "b"}, firstErrors: map[string]string{"a": "a's", "b": "b's on the host"}}
	build := objectLoad{broken: []string{"b", "c"}, firstErrors: map[string]string{"b": "b's in the build", "c": "c's"}}
	want := []string{"a on the host: a's", "b on the host and in the build: b's on the host", "c in the build: c's"}
	if leftOut := leftOutOf(host, build); !slices.Equal(leftOut, want) {
		test.Errorf("want the packages left out %q, got %q", want, leftOut)
	}

	var seven []string
	for index := range 7 {
		seven = append(seven, fmt.Sprintf("p%d in the build: its error", index))
	}
	var report recordedReport
	reportObjectDifferences(&report, "linux", objectTally{leftOut: seven}, time.Now())
	wantFailure := "linux: 7 packages loaded with errors, so the differential compared nothing of them and what it " +
		"compared is the tree in part; the first 5: " + strings.Join(seven[:5], "; ")
	if !slices.Equal(report.failed, []string{wantFailure}) {
		test.Errorf("want the report to fail once, saying %q, got %q", wantFailure, report.failed)
	}
}
