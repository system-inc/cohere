// Command cohere-parity is the census of the Go engine's ports against their originals over a real tree. A port's
// tests hold it to its original over fixtures (internal/go_engine/check/parity); this holds it over code nobody wrote
// for the comparison, as the engine loads it, tests included, and prints where the two disagree.
//
//	cohere-parity <directory>                     every pair over ./... of directory
//	cohere-parity <directory> std                 the patterns to load, as go list takes them
//	cohere-parity --rule nilness <directory>      only the pairs named, once each
//
// It prints the analyzed packages that did not load clean, with their first errors, then, for each pair, each side's
// count, its gains and its unexplained gains, every finding one side made and the other did not, the recorded gains
// the tree did not produce, and the packages each side did not run on, with why: a side's silence on a package it
// never ran on, or ran on half type-checked, would read as agreement.
//
// A finding only the port makes is a gain when the port's gains file records it, with its reason above it (see
// readGains), and an unexplained gain otherwise (Square, #p1yvetm). A recorded gain the tree does not produce is
// listed as not seen and fails nothing, since a tree need not hold the file. A tree has no differences fixture, so
// no `drops:` record covers a finding here: each the original makes alone stays a regression until a person
// adjudicates it with the finding in hand. It exits 1 on any regression or unexplained gain, and 2 when it could not
// read the gains, or load or analyze the tree at all.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"
	"honnef.co/go/tools/stylecheck/st1001"
	"honnef.co/go/tools/stylecheck/st1006"
	"honnef.co/go/tools/stylecheck/st1008"
	"honnef.co/go/tools/stylecheck/st1011"
	"honnef.co/go/tools/stylecheck/st1012"
	"honnef.co/go/tools/stylecheck/st1013"
	"honnef.co/go/tools/stylecheck/st1016"
	"honnef.co/go/tools/stylecheck/st1017"
	"honnef.co/go/tools/stylecheck/st1018"
	"honnef.co/go/tools/stylecheck/st1019"

	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rules/dot_import"
	"github.com/system-inc/cohere/internal/go_engine/rules/duplicate_import"
	"github.com/system-inc/cohere/internal/go_engine/rules/duration_unit_suffix"
	"github.com/system-inc/cohere/internal/go_engine/rules/error_variable_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/http_status_literal"
	"github.com/system-inc/cohere/internal/go_engine/rules/inconsistent_receiver_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/invisible_character"
	"github.com/system-inc/cohere/internal/go_engine/rules/misplaced_error_result"
	"github.com/system-inc/cohere/internal/go_engine/rules/nil_ness"
	"github.com/system-inc/cohere/internal/go_engine/rules/receiver_name"
	"github.com/system-inc/cohere/internal/go_engine/rules/unused_write"
	"github.com/system-inc/cohere/internal/go_engine/rules/yoda_condition"
)

// censusPair is a pair as the census runs it, with its port's gains file, relative to cohere's root, or empty for a
// port that records none.
type censusPair struct {
	pair  parity.Pair
	gains string
}

// pairs is every port beside its original, by the name the census prints. A new port is one entry here. Over a tree
// under the user's cache directory, ST1001's and ST1013's originals alone report what its staticcheck.conf allows,
// which each port's differences fixture records as dropped (#sc5qmy7), so run the census over a tree kept elsewhere.
var pairs = []censusPair{
	{
		pair:  parity.Pair{Name: "nilness", Original: nilness.Analyzer, Port: nil_ness.Analyzer},
		gains: "internal/go_engine/rules/nil_ness/testdata/gains.txt",
	},
	{pair: parity.Pair{Name: "unusedwrite", Original: unusedwrite.Analyzer, Port: unused_write.Analyzer}},
	{pair: parity.Pair{Name: "ST1006", Original: st1006.Analyzer, Port: receiver_name.Analyzer}},
	{pair: parity.Pair{Name: "ST1016", Original: st1016.Analyzer, Port: inconsistent_receiver_name.Analyzer}},
	{pair: parity.Pair{Name: "ST1008", Original: st1008.Analyzer, Port: misplaced_error_result.Analyzer}},
	{pair: parity.Pair{Name: "ST1012", Original: st1012.Analyzer, Port: error_variable_name.Analyzer}},
	{pair: parity.Pair{Name: "ST1011", Original: st1011.Analyzer, Port: duration_unit_suffix.Analyzer}},
	{pair: parity.Pair{Name: "ST1013", Original: st1013.Analyzer, Port: http_status_literal.Analyzer}},
	{pair: parity.Pair{Name: "ST1017", Original: st1017.Analyzer, Port: yoda_condition.Analyzer}},
	{pair: parity.Pair{Name: "ST1001", Original: st1001.Analyzer, Port: dot_import.Analyzer}},
	{pair: parity.Pair{Name: "ST1019", Original: st1019.Analyzer, Port: duplicate_import.Analyzer}},
	{pair: parity.Pair{Name: "ST1018", Original: st1018.Analyzer, Port: invisible_character.Analyzer}},
}

// The exit codes: the two agree, or differ only in gains each recorded; the original found what the port did not, or
// the port what no gain records; the command line, the gains or the tree could not be read.
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
	root, rootError := cohereRoot()
	if rootError != nil {
		// Ignored: as above.
		_, _ = fmt.Fprintf(standardError, "cohere-parity: %v\n", rootError)
		return exitFailed
	}
	result, censusError := census(asked.pairs, root, asked.directory, asked.patterns)
	if censusError != nil {
		// Ignored: as above.
		_, _ = fmt.Fprintf(standardError, "cohere-parity: %v\n", censusError)
		return exitFailed
	}
	if _, writeError := io.WriteString(standardOutput, result.report); writeError != nil {
		// Ignored: as above.
		_, _ = fmt.Fprintf(standardError, "cohere-parity: printing the census: %v\n", writeError)
		return exitFailed
	}
	if result.regressions+result.unexplained > 0 {
		return exitRegression
	}
	return exitAgreed
}

// cohereModule is the module path of cohere's root module, whose directory the gains files are named relative to.
const cohereModule = "github.com/system-inc/cohere"

// cohereRoot is the root of the cohere checkout the census runs in: the nearest directory at or above the working
// directory whose go.mod is cohere's. The gains files are read from it rather than from where this binary was built,
// since the pool builds every binary with -trimpath, which keeps no such path.
func cohereRoot() (string, error) {
	directory, workingError := os.Getwd()
	if workingError != nil {
		return "", fmt.Errorf("reading the working directory: %w", workingError)
	}
	for {
		contents, readError := os.ReadFile(filepath.Join(directory, "go.mod"))
		if readError == nil && modfile.ModulePath(contents) == cohereModule {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("no directory at or above the working directory holds %s's go.mod, so the gains "+
				"files cannot be read; run cohere-parity from inside a cohere checkout", cohereModule)
		}
		directory = parent
	}
}

// commandLine is what the command line asks for: the pairs to run, the directory to load from, and the patterns to
// load.
type commandLine struct {
	pairs     []censusPair
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
	for _, each := range pairs {
		known = append(known, each.pair.Name)
		if len(named) == 0 || slices.Contains(named, each.pair.Name) {
			asked.pairs = append(asked.pairs, each)
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

// censusResult is a census's report, how many findings the originals made that the ports did not, and how many the
// ports made that no recorded gain explains.
type censusResult struct {
	report      string
	regressions int
	unexplained int
}

// census loads the packages patterns name from directory as the engine loads them, tests included, runs every
// selected pair's original and port in one checker over what the engine analyzes, and judges each finding only the
// port makes against its gains file, read relative to gainsRoot.
func census(selected []censusPair, gainsRoot string, directory string, patterns []string) (censusResult, error) {
	recordedGains := map[string][]gain{}
	analyzed := []parity.Pair{}
	for _, each := range selected {
		analyzed = append(analyzed, each.pair)
		if each.gains == "" {
			continue
		}
		gains, gainsError := readGains(filepath.Join(gainsRoot, filepath.FromSlash(each.gains)))
		if gainsError != nil {
			return censusResult{}, fmt.Errorf("%s: %w", each.pair.Name, gainsError)
		}
		recordedGains[each.pair.Name] = gains
	}
	workspace, workspaceError := project.ReadWorkspace(directory)
	if workspaceError != nil {
		return censusResult{}, fmt.Errorf("reading the modules of %s: %w", directory, workspaceError)
	}
	loaded, loadError := load.Packages(workspace.Root, patterns, workspace.Environment(), nil)
	if loadError != nil {
		return censusResult{}, loadError
	}
	// Nothing loaded is no census: every side would agree, and prove nothing.
	if len(loaded.Packages) == 0 {
		return censusResult{}, fmt.Errorf("%s in %s matched no packages", strings.Join(patterns, " "), workspace.Root)
	}
	outcomes, analyzeError := parity.Analyze(analyzed, loaded.Packages)
	if analyzeError != nil {
		return censusResult{}, analyzeError
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
	result := censusResult{}
	for _, outcome := range outcomes {
		originalFindings := relative(workspace.Root, outcome.Original.Findings)
		portFindings := relative(workspace.Root, outcome.Port.Findings)
		missed := parity.Difference(originalFindings, portFindings)
		gained, unexplained, notSeen := judgeGains(parity.Difference(portFindings, originalFindings),
			recordedGains[outcome.Pair.Name])
		result.regressions += len(missed)
		result.unexplained += len(unexplained)
		fmt.Fprintf(report, "%s: original %d, port %d, gains %d, unexplained %d\n", outcome.Pair.Name,
			len(originalFindings), len(portFindings), len(gained), len(unexplained))
		for _, finding := range missed {
			fmt.Fprintf(report, "  the original alone: %s\n", finding)
		}
		for _, finding := range unexplained {
			fmt.Fprintf(report, "  unexplained gain: %s\n", finding)
		}
		for _, each := range gained {
			fmt.Fprintf(report, "  gain: %s\n    because %s\n", each.finding, each.reason)
		}
		for _, recorded := range notSeen {
			fmt.Fprintf(report, "  gain not seen: %s\n", recorded.finding)
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
	if result.regressions > 0 {
		fmt.Fprintf(report, "the originals found %d findings the ports did not\n", result.regressions)
	}
	if result.unexplained > 0 {
		fmt.Fprintf(report, "the ports found %d findings no recorded gain explains\n", result.unexplained)
	}
	result.report = report.String()
	return result, nil
}

// judgeGains splits portAlone, the findings only the port makes, into the recorded gains that explain them, each
// explaining one finding printed exactly as it records, and the findings none does, and returns the recorded gains no
// finding matched.
func judgeGains(portAlone []parity.Finding, recorded []gain) (gained []gain, unexplained []parity.Finding,
	notSeen []gain) {
	// unmatched holds each recorded gain's reasons, by the line it records, until a finding takes one.
	unmatched := map[string][]gain{}
	for _, each := range recorded {
		unmatched[each.finding] = append(unmatched[each.finding], each)
	}
	for _, finding := range portAlone {
		if waiting := unmatched[finding.String()]; len(waiting) > 0 {
			gained = append(gained, waiting[0])
			unmatched[finding.String()] = waiting[1:]
			continue
		}
		unexplained = append(unexplained, finding)
	}
	for _, each := range recorded {
		if waiting := unmatched[each.finding]; len(waiting) > 0 {
			notSeen = append(notSeen, waiting[0])
			unmatched[each.finding] = waiting[1:]
		}
	}
	return gained, unexplained, notSeen
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
