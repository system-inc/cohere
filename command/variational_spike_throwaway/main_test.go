package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureRoot is the platform census's fork fixture: N, Kill and Stat declared once per platform, and functions that
// read them beside functions that don't.
var fixtureRoot = filepath.Join("..", "platform_census_throwaway", "testdata", "fork")

// fixtureCommandLine checks the fixture for darwin, linux and windows on arm64. The fixture requires no module and asks
// for this go, so its go list reaches no proxy and fetches no toolchain.
func fixtureCommandLine(test *testing.T, plant bool) commandLine {
	test.Helper()
	asked, parseError := parseCommandLine([]string{
		"--root", fixtureRoot,
		"--host", "darwin/arm64,cgo=0",
		"--build", "linux/arm64",
		"--build", "windows/arm64",
		"--patterns", "./...",
	}, io.Discard)
	if parseError != nil {
		test.Fatal(parseError)
	}
	asked.plant = plant
	return asked
}

// TestFixtureProjectsExactlyToEveryBuild runs the fixture through the whole spike: one variational check, each build's
// projection compared with go/packages' plain answers, every expression matched and none on one side only.
func TestFixtureProjectsExactlyToEveryBuild(test *testing.T) {
	// Not parallel: the fork holds a variational check's state in package variables, one check at a time.
	report, spikeError := runSpike(fixtureCommandLine(test, false), io.Discard)
	if spikeError != nil {
		test.Fatal(spikeError)
	}
	if !report.green() {
		test.Errorf("the fixture's proof is red:\n%s", tableText(report))
	}
	if len(report.Proof) != 3 {
		test.Fatalf("%d builds proved, want 3", len(report.Proof))
	}
	for _, proof := range report.Proof {
		if proof.Compared == 0 || proof.Matched != proof.Compared {
			test.Errorf("%s: %d of %d matched", proof.Target, proof.Matched, proof.Compared)
		}
	}
	if report.Totals.ForkedExpressions == 0 || report.Totals.Restarts == 0 {
		test.Errorf("the fixture forks, yet %d expressions have more than one answer and %d units restarted",
			report.Totals.ForkedExpressions, report.Totals.Restarts)
	}
}

// TestPlantedMismatchShowsRed changes one projected answer of the host before the proof, which must count exactly that
// one as mismatched and call the run red, while the other builds stay green.
func TestPlantedMismatchShowsRed(test *testing.T) {
	// Not parallel: the fork holds a variational check's state in package variables, one check at a time.
	report, spikeError := runSpike(fixtureCommandLine(test, true), io.Discard)
	if spikeError != nil {
		test.Fatal(spikeError)
	}
	if report.green() {
		test.Fatal("a planted mismatch left the run green")
	}
	host := report.Proof[0]
	if host.Green || host.Mismatched != 1 || len(host.FirstMismatches) != 1 {
		test.Fatalf("host: %d mismatched, %d mismatches spelled, want the one planted", host.Mismatched,
			len(host.FirstMismatches))
	}
	if !strings.HasSuffix(host.FirstMismatches[0].Projected, plantedSuffix) {
		test.Errorf("the mismatch spelled is %+v, not the planted one", host.FirstMismatches[0])
	}
	for _, proof := range report.Proof[1:] {
		if !proof.Green {
			test.Errorf("%s turned red, though nothing was planted there", proof.Target)
		}
	}
}

// TestMixedArchitecturesAreRefused refuses builds of two architectures, which one Sizes can't check.
func TestMixedArchitecturesAreRefused(test *testing.T) {
	test.Parallel()
	_, parseError := parseCommandLine([]string{"--root", ".", "--host", "darwin/arm64", "--build", "linux/amd64"},
		io.Discard)
	if parseError == nil || !strings.Contains(parseError.Error(), "mix architectures") {
		test.Fatalf("mixed architectures: %v, want a refusal", parseError)
	}
}
