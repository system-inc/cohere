package main

import (
	"io"
	"maps"
	"path/filepath"
	"slices"
	"testing"
)

// The census over testdata/fork, linux/amd64 against windows/386 and darwin/arm64, finds the forks the fixture plants:
// N differs, N and N * 3 fork by reference, unsafe.Sizeof(uintptr(0)) forks locally between amd64 and 386, F and
// WordSize are forked functions and Untouched is not, and the variants are N's three answers, N's and N * 3's three
// each, and the word size's two.
func TestCensusCountsTheFixturesForks(test *testing.T) {
	test.Parallel()
	// The fixture requires no module and asks for this go, so its go list reaches no proxy and fetches no toolchain.
	host, hostError := parseConfiguration("linux/amd64,cgo=0")
	if hostError != nil {
		test.Fatal(hostError)
	}
	host.isHost = true
	windows, windowsError := parseConfiguration("windows/386")
	darwin, darwinError := parseConfiguration("darwin/arm64")
	if windowsError != nil || darwinError != nil {
		test.Fatal(windowsError, darwinError)
	}
	report, censusError := takeCensus(commandLine{
		root:     filepath.Join("testdata", "fork"),
		host:     host,
		builds:   []configuration{windows, darwin},
		patterns: []string{"./..."},
		tests:    true,
	}, io.Discard)
	if censusError != nil {
		test.Fatal(censusError)
	}
	if len(report.Builds) != 2 {
		test.Fatalf("%d builds counted, want 2", len(report.Builds))
	}

	againstWindows := report.Builds[0].Groups[groupOurs]
	if !slices.Equal(againstWindows.DifferingDeclarationNames, []string{"example.com/fork.N"}) {
		test.Errorf("windows/386: differing declarations %q, want only example.com/fork.N",
			againstWindows.DifferingDeclarationNames)
	}
	classes := map[string]string{}
	for _, fork := range againstWindows.Forks {
		classes[fork.Expression] = fork.Class
	}
	want := map[string]string{"N": classByReference, "N * 3": classByReference, "unsafe.Sizeof(uintptr(0))": classLocal}
	if !maps.Equal(classes, want) {
		test.Errorf("windows/386: forks %v, want %v", classes, want)
	}
	if againstWindows.ForkedByReference != 2 || againstWindows.ForkedLocal != 1 {
		test.Errorf("windows/386: %d by reference and %d local, want 2 and 1",
			againstWindows.ForkedByReference, againstWindows.ForkedLocal)
	}
	forkedFunctions := []string{"example.com/fork.F", "example.com/fork.WordSize"}
	if !slices.Equal(againstWindows.ForkedFunctionNames, forkedFunctions) || againstWindows.Functions != 3 {
		test.Errorf("windows/386: forked functions %q of %d, want %q of 3",
			againstWindows.ForkedFunctionNames, againstWindows.Functions, forkedFunctions)
	}

	againstDarwin := report.Builds[1].Groups[groupOurs]
	if !slices.Equal(againstDarwin.ForkedFunctionNames, []string{"example.com/fork.F"}) ||
		againstDarwin.ForkedLocal != 0 {
		test.Errorf("darwin/arm64: forked functions %q and %d local forks, want only F and none local",
			againstDarwin.ForkedFunctionNames, againstDarwin.ForkedLocal)
	}

	overAll := report.OverAll[groupOurs]
	if !maps.Equal(overAll.DeclarationVariants, map[int]int{3: 1}) {
		test.Errorf("declaration variants %v, want N's three answers", overAll.DeclarationVariants)
	}
	if !maps.Equal(overAll.ExpressionVariants, map[int]int{2: 1, 3: 2}) {
		test.Errorf("expression variants %v, want N and N * 3 with three answers and the word size with two",
			overAll.ExpressionVariants)
	}
	if overAll.ForkedFunctions != 2 || overAll.HostFunctions != 3 {
		test.Errorf("over every build %d of %d functions forked, want 2 of 3", overAll.ForkedFunctions,
			overAll.HostFunctions)
	}
}
