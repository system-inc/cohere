// Command cohere-parity is the census of the Go engine's ports against their originals over a real tree. A port's
// tests hold it to its original over fixtures (internal/go_engine/check/parity); this holds it over code nobody wrote
// for the comparison, as the engine loads it, tests included, and prints where the two disagree.
//
//	cohere-parity <directory>                     every pair over ./... of directory
//	cohere-parity <directory> std                 the patterns to load, as go list takes them
//	cohere-parity --rule nilness <directory>      only the pairs named, once each
//
// It prints the analyzed packages that did not load clean, with their first errors, then, for each pair, each side's
// count, every finding one side made and the other did not, and the packages each side did not run on, with why: a
// side's silence on a package it never ran on, or ran on half type-checked, would read as agreement. It exits
// 1 when the original made any finding the port did not, and 2 when it could not load or analyze the tree at all. A
// tree has no differences fixture, so no `drops:` record covers a finding here: each the original makes alone exits 1
// until a person adjudicates it with the finding in hand.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"

	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rules/nil_ness"
	"github.com/system-inc/cohere/internal/go_engine/rules/unused_write"
)

// pairs is every port beside its original, by the name the census prints. A new port is one line here.
var pairs = []parity.Pair{
	{Name: "nilness", Original: nilness.Analyzer, Port: nil_ness.Analyzer},
	{Name: "unusedwrite", Original: unusedwrite.Analyzer, Port: unused_write.Analyzer},
}

// The exit codes: the two agree, or differ only where the port finds more; the original found what the port did not;
// the command line or the tree could not be read.
const (
	exitAgreed     = 0
	exitRegression = 1
	exitFailed     = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run runs the census the command line asks for, printing it to standardOutput and what stopped it to
// standardError, and returns the exit code.
func run(arguments []string, standardOutput io.Writer, standardError io.Writer) int {
	asked, parseError := parseArguments(arguments)
	if parseError != nil {
		// Ignored: the exit code says the command line was refused even when standard error cannot.
		_, _ = fmt.Fprintf(standardError, "cohere-parity: %v\n"+
			"usage: cohere-parity [--rule <name>]... <directory> [patterns, default ./...]\n", parseError)
		return exitFailed
	}
	report, regressions, censusError := census(asked.pairs, asked.directory, asked.patterns)
	if censusError != nil {
		// Ignored: as above.
		_, _ = fmt.Fprintf(standardError, "cohere-parity: %v\n", censusError)
		return exitFailed
	}
	if _, writeError := io.WriteString(standardOutput, report); writeError != nil {
		// Ignored: as above.
		_, _ = fmt.Fprintf(standardError, "cohere-parity: printing the census: %v\n", writeError)
		return exitFailed
	}
	if regressions > 0 {
		return exitRegression
	}
	return exitAgreed
}

// commandLine is what the command line asks for: the pairs to run, the directory to load from, and the patterns to
// load.
type commandLine struct {
	pairs     []parity.Pair
	directory string
	patterns  []string
}

// parseArguments reads the command line: the pairs `--rule` names, every pair when none is named, then the
// directory, then the patterns, ./... when none is given.
func parseArguments(arguments []string) (commandLine, error) {
	named := []string{}
	positional := []string{}
	for index := 0; index < len(arguments); index++ {
		switch argument := arguments[index]; {
		case argument == "--rule" || argument == "-rule":
			if index+1 >= len(arguments) {
				return commandLine{}, fmt.Errorf("%s needs a rule name", argument)
			}
			index++
			named = append(named, arguments[index])
		case strings.HasPrefix(argument, "-"):
			return commandLine{}, fmt.Errorf("unknown flag %q", argument)
		default:
			positional = append(positional, argument)
		}
	}
	if len(positional) == 0 {
		return commandLine{}, fmt.Errorf("no directory given")
	}
	asked := commandLine{directory: positional[0], patterns: positional[1:]}
	known := []string{}
	for _, pair := range pairs {
		known = append(known, pair.Name)
		if len(named) == 0 || slices.Contains(named, pair.Name) {
			asked.pairs = append(asked.pairs, pair)
		}
	}
	for _, name := range named {
		if !slices.Contains(known, name) {
			return commandLine{}, fmt.Errorf("no pair is named %q; the pairs are %s", name, strings.Join(known, ", "))
		}
	}
	if len(asked.patterns) == 0 {
		asked.patterns = []string{"./..."}
	}
	return asked, nil
}

// census loads the packages patterns name from directory as the engine loads them, tests included, runs every
// selected pair's original and port in one checker over what the engine analyzes, and returns the report and how
// many findings the originals made that the ports did not.
func census(selected []parity.Pair, directory string, patterns []string) (string, int, error) {
	workspace, workspaceError := project.ReadWorkspace(directory)
	if workspaceError != nil {
		return "", 0, fmt.Errorf("reading the modules of %s: %w", directory, workspaceError)
	}
	loaded, loadError := load.Packages(workspace.Root, patterns, workspace.Environment(), nil)
	if loadError != nil {
		return "", 0, loadError
	}
	// Nothing loaded is no census: every side would agree, and prove nothing.
	if len(loaded.Packages) == 0 {
		return "", 0, fmt.Errorf("%s in %s matched no packages", strings.Join(patterns, " "), workspace.Root)
	}
	outcomes, analyzeError := parity.Analyze(selected, loaded.Packages)
	if analyzeError != nil {
		return "", 0, analyzeError
	}
	report := &strings.Builder{}
	fmt.Fprintf(report, "loaded %d packages, %d analyzed, with %d load diagnostics\n",
		loaded.PackagesLoaded, len(loaded.Packages), len(loaded.Diagnostics))
	// A tree may hold packages that do not type-check, so each is named rather than failing the census. Both sides
	// read the same load, so the list is the same for each, and what each side then did on them follows per pair.
	loadFailures := parity.LoadFailures(loaded.Packages)
	fmt.Fprintf(report, "%d analyzed packages did not load clean\n", len(loadFailures))
	for _, failure := range loadFailures {
		fmt.Fprintf(report, "  %s: %s\n", failure.Package, failure.Error)
	}
	regressions := 0
	for _, outcome := range outcomes {
		originalFindings := relative(workspace.Root, outcome.Original.Findings)
		portFindings := relative(workspace.Root, outcome.Port.Findings)
		fmt.Fprintf(report, "%s: the original found %d, the port %d\n", outcome.Pair.Name, len(originalFindings),
			len(portFindings))
		missed := parity.Difference(originalFindings, portFindings)
		regressions += len(missed)
		for _, finding := range missed {
			fmt.Fprintf(report, "  the original alone: %s\n", finding)
		}
		for _, finding := range parity.Difference(portFindings, originalFindings) {
			fmt.Fprintf(report, "  the port alone: %s\n", finding)
		}
		for _, side := range []struct {
			name     string
			failures []parity.Failure
		}{{"the original", outcome.Original.Failures}, {"the port", outcome.Port.Failures}} {
			fmt.Fprintf(report, "  %s did not run on %d packages\n", side.name, len(side.failures))
			for _, failure := range side.failures {
				fmt.Fprintf(report, "    %s: %s\n", failure.Package, failure.Error)
			}
		}
	}
	if regressions > 0 {
		fmt.Fprintf(report, "the originals found %d findings the ports did not\n", regressions)
	}
	return report.String(), regressions, nil
}

// relative is findings with each file they name, their own, their related entries' and their edits', named relative
// to root where it is inside root, so the report reads the same in any checkout.
func relative(root string, findings []parity.Finding) []parity.Finding {
	insideRoot := func(file string) string {
		path, relativeError := filepath.Rel(root, file)
		if relativeError != nil || strings.HasPrefix(path, "..") {
			return file
		}
		return path
	}
	named := []parity.Finding{}
	for _, finding := range findings {
		named = append(named, finding.WithFileNames(insideRoot))
	}
	return named
}
