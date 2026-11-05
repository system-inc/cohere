package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/sentence"
	"github.com/system-inc/cohere/internal/streams"
	"github.com/system-inc/cohere/internal/types/program"
)

// projectEngineVariable names the engine a child run checks its --directory with. A directory holding both
// a tsconfig.json and a Package.swift is two projects, and --directory alone reads it as TypeScript's, so
// the run that checks its Swift says which engine it is.
const projectEngineVariable = "COHERE_PROJECT_ENGINE"

// projectLabelVariable names the project a child run checks, by its path under the root, so the run's own
// footer can say which project it is about (#ytqqv8v).
const projectLabelVariable = "COHERE_PROJECT_LABEL"

// discoveredListings are the directory listings discovery read, when the one project it found runs in this
// process, for its build to serve the tsconfig's include enumeration from. Nil otherwise. See #bjv0tg4.
var discoveredListings *program.DirectoryListings

// discoveredUnclaimedSwift is the Swift no package holds that discovery found, when the one project it found
// runs in this process, for that run's footer to say. Nil otherwise: a project run as one of several says
// nothing of it, since the overall verdict line does.
var discoveredUnclaimedSwift []unclaimedSwiftDirectory

// discoveryApplies reports whether a run discovers its projects. Anything that names one project, a
// directory, a tsconfig or a path, narrows the run to it as it always did, and so do the listings, the
// explanations and the editor's save, which answer about one project.
//
// A lint config does not: it says which rules apply, not which project, so every project discovered runs
// under it. It used to narrow, and the quiet hundred's harness, which names one, checked 2 of TanStack
// Query's thousand files and refused prisma outright (#wvgxtey).
func discoveryApplies(given map[string]bool, arguments []string) bool {
	if len(arguments) > 0 {
		return false
	}
	for _, name := range []string{
		"directory", "tsconfig", "rules", "rules-enabled", "print-config",
		"version", "cache-dump", "explain", "stdin-filepath", "profile",
	} {
		if given[name] {
			return false
		}
	}
	return true
}

// checkDiscoveredProjects discovers the projects under the root and, when there is more than the one the
// walk up located, checks them all and reports checked with the merged exit code. With only that one it
// prints what discovery passed over and leaves the run to check it in this process.
//
// lintConfigFileName is the lint config the command line named, absolute, or empty: every project's run
// gets it, since a path typed here would resolve against each project's directory there.
func checkDiscoveredProjects(
	location projectLocation,
	locateError error,
	workingDirectory string,
	lintConfigFileName string,
) (int, bool, error) {
	root := discoveryRoot(location, locateError, workingDirectory)
	patterns, err := rootIgnorePatterns(root)
	if err != nil {
		return 0, false, err
	}
	// Asked here because a Swift-only run loads no lint configuration, so this is the one read of the root's
	// file that every run makes. A file the command line named is the Swift engine's to read wherever it
	// sits, and the projects' runs are handed it (see configuration.CheckSwiftBlockHasAReader).
	if settings := filepath.Join(root, "CohereSettings.json"); settings != lintConfigFileName && isRegularFile(settings) {
		if checkSwiftBlockHasAReaderError := configuration.CheckSwiftBlockHasAReader(settings); checkSwiftBlockHasAReaderError != nil {
			return 0, false, checkSwiftBlockHasAReaderError
		}
	}
	found, err := discoverProjects(root, patterns)
	if err != nil {
		return 0, false, err
	}
	if found.Ownership, err = resolveOwnership(&found); err != nil {
		return 0, false, err
	}
	// Before the one-project check, so a package and the dependency its run checks are the one project they
	// are, and run here as `cohere --directory` on the package would.
	found.SwiftCoverage = resolveSwiftCoverage(&found)
	if len(found.Projects) == 0 {
		// Nothing to check is a failure, never a green over nothing.
		solutions := ""
		if len(found.Ownership.Solutions) > 0 {
			solutions = fmt.Sprintf(" (%d solution tsconfigs found, %s, whose references reach no project here)",
				len(found.Ownership.Solutions), strings.Join(found.Ownership.Solutions, ", "))
		}
		return 0, false, fmt.Errorf("no %s, %s, %s or %s in %s, any directory above it, or any directory "+
			"below it that the repository keeps, so there is no project here to check%s (not searched: %d "+
			"directories .gitignore ignores, %d dependency, build, cache or fixture directories, %d nested "+
			"repositories): run from inside one, or name it with -tsconfig or -directory",
			projectMarker, swiftProjectMarker, goWorkspaceMarker, goModuleMarker, root, solutions, found.Ignored,
			found.NeverDescended, len(found.NestedRepositories)+found.Submodules)
	}
	if found.isTheLocatedProject(location, locateError) {
		// The project runs here, and its build lists the directories discovery just did. Its footer says the
		// Swift no package checks, since a TypeScript program at the root beside an app's Swift is this path.
		discoveredListings = found.Listings
		discoveredUnclaimedSwift = found.UnclaimedSwift
		for _, note := range found.notes() {
			fmt.Fprintf(accountOutput(streams.Errors), "cohere: %s\n", note)
		}
		showWarning()
		return 0, false, nil
	}
	return runProjects(found, childArguments(os.Args[1:], lintConfigFileName), streams.Output), true, nil
}

// childArguments are the arguments every project's run gets: this run's, with a named lint config made
// absolute. Discovery runs only when no path was named, so every argument is a flag, and a flag's value is
// either joined to it with `=` or the next argument, since no boolean flag takes a separate one.
func childArguments(arguments []string, lintConfigFileName string) []string {
	if lintConfigFileName == "" {
		return arguments
	}
	kept := make([]string, 0, len(arguments)+2)
	for index := 0; index < len(arguments); index++ {
		name, _, joined := strings.Cut(strings.TrimLeft(arguments[index], "-"), "=")
		if name == "lint-config" && strings.HasPrefix(arguments[index], "-") {
			if !joined {
				index++
			}
			continue
		}
		kept = append(kept, arguments[index])
	}
	return append(kept, "--lint-config", lintConfigFileName)
}

// isTheLocatedProject reports whether discovery found exactly the one project the walk up located, which
// a run then checks in this process, the way it always did.
func (found discovery) isTheLocatedProject(location projectLocation, locateError error) bool {
	if locateError != nil || len(found.Projects) != 1 {
		return false
	}
	project := found.Projects[0]
	return project.Engine == location.Engine && project.ConfigFile == "" &&
		filepath.Clean(filepath.Join(found.Root, project.Directory)) == filepath.Clean(location.Root)
}

// notes are the lines a run carries about markers discovery found and did not run, which a reader could have
// expected checked: those the root's ignorePatterns refuse, solution roots, projects left with no file of
// their own, and Swift packages another package's run covers. So is a Go module that runs on its own below a
// workspace, which a reader could have expected the workspace to build. Ignored and dependency directories are
// not named, since skipping them is what everyone expects, and neither is a nested repository: one inside a
// program, such as Structure or Base, is checked as part of it, its drift reported read-only, as it always was.
// They are counted on the summary line. A Swift coverage query that failed is named too, since its packages may
// then be checked twice, and so is each directory of Swift files no package holds, since nothing checks them.
func (found discovery) notes() []string {
	var lines []string
	for _, marker := range found.Refused {
		lines = append(lines, fmt.Sprintf("%s not checked: the root's ignorePatterns match it", marker))
	}
	for _, label := range found.Ownership.Solutions {
		lines = append(lines, fmt.Sprintf(
			"%s not run: a solution tsconfig, which includes no file and references the projects checked instead",
			label,
		))
	}
	for _, label := range found.Ownership.Yielding {
		lines = append(lines, fmt.Sprintf("%s not run: every file it includes belongs to a nearer tsconfig", label))
	}
	for _, workspace := range found.BrokenWorkspaces {
		lines = append(lines, fmt.Sprintf("%s claims no module, so each module below it runs on its own: %s",
			path.Join(workspace.Directory, goWorkspaceMarker), workspace.Reason))
	}
	for _, module := range found.UnusedModules {
		lines = append(lines, fmt.Sprintf("%s runs as its own module: %s does not use it",
			path.Join(module.Directory, goModuleMarker), path.Join(module.Workspace, goWorkspaceMarker)))
	}
	for _, covered := range found.SwiftCoverage.Covered {
		if covered.Vendored {
			lines = append(lines, fmt.Sprintf(
				"%s not run: vendored in %s, whose run lists it as not checked",
				covered.Label,
				covered.CoveredBy,
			))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s not run: checked as part of %s", covered.Label, covered.CoveredBy))
	}
	for _, failed := range found.SwiftCoverage.FailedQueries {
		lines = append(lines, fmt.Sprintf(
			"%s: the Swift coverage query failed, so every package it might check runs as its own project too: %s",
			failed.Label,
			failed.Reason,
		))
	}
	for _, unclaimed := range found.UnclaimedSwift {
		lines = append(lines, fmt.Sprintf(
			"%s: %s no Swift package checks",
			unclaimed.Directory,
			counted(unclaimed.Files, "Swift file", "Swift files"),
		))
	}
	// Last, since only these are sorted: a map has no order of its own.
	for label := range found.Ownership.SharedDirectory {
		lines = append(lines, fmt.Sprintf(
			"%s ran with the cache off: another tsconfig's run keeps its cache in the same directory",
			label,
		))
	}
	sort.Strings(lines[len(lines)-len(found.Ownership.SharedDirectory):])
	return lines
}

// projectRun is one project's run, as its child reported it.
type projectRun struct {
	open    *openReport
	project discoveredProject
	output  []byte
	// standardError is the child's stderr when it is kept apart from output, under `--json`, and empty
	// otherwise, when the two share output in the order they were written.
	standardError []byte
	exitCode      int
	err           error
	elapsed       time.Duration
}

// runProjects checks every discovered project, each in its own run of this binary, all at once, and
// reports them as one: a section per project in the order discovery found them, then one summary line.
// It exits with the worst exit code among them, so the run is green only when every project is, and a
// project whose run crashed or could not start fails it.
//
// Each project is a child process rather than a goroutine, because a run is a whole program: its own
// type graph, its own settings and cache table, and its own memory ceiling. A child is exactly the run
// `cohere --directory <project>` makes alone, so a project's section reads the same as its own run's, and
// its exit code is the one that run alone would give, less what it yields to nearer projects.
//
// At most projectRunsAtOnce run together. Each builds a whole type graph and checks it on every core, so
// TanStack Query's 95 projects started at once were 95 graphs in memory, and more children than that buys no
// time the cores do not already give one.
func runProjects(found discovery, arguments []string, out *bufio.Writer) int {
	// failures is where this run's own failure lines go: with the report in the human views, and on stderr
	// under `--json`, whose stdout is records only.
	failures := out
	if activeOutput.Mode == outputJSON {
		failures = streams.Errors
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(failures, "cohere: %s\n",
			sentence.FromError(fmt.Errorf("finding this binary to run each project: %w", err)))
		return 1
	}
	yieldDirectory, err := os.MkdirTemp("", "cohere-yield-")
	if err != nil {
		fmt.Fprintf(failures, "cohere: %s\n",
			sentence.FromError(fmt.Errorf("making a place for what each project yields: %w", err)))
		return 1
	}
	// Ignored: a temporary directory that outlives the run costs only its space, and nothing reads it again.
	defer os.RemoveAll(yieldDirectory)

	children := forwardSignalsToChildren()
	defer children.stop()

	runs := make([]projectRun, len(found.Projects))
	var group sync.WaitGroup
	running := make(chan struct{}, projectRunsAtOnce())
	for index, project := range found.Projects {
		label := projectLabel(project)
		// A Swift project and a TypeScript project in one directory share a label, and what one yields is
		// nothing to the other, so a Swift project's yield is its own.
		yield := found.Ownership.Yields[label]
		if project.Engine == engineSwift {
			yield = found.swiftYield(project)
		}
		yieldFile, writeProjectYieldError := writeProjectYield(yield, yieldDirectory)
		if writeProjectYieldError != nil {
			runs[index] = projectRun{
				project:  project,
				exitCode: 1,
				err:      fmt.Errorf("writing what it yields: %w", writeProjectYieldError),
			}
			continue
		}
		projectArguments := arguments
		// TypeScript runs alone: the cache it turns off is cohere's table in the directory, which a Swift run
		// never reads or writes, so a Swift run in the same directory keeps its own cache.
		if found.Ownership.SharedDirectory[label] && project.Engine == engineTypeScript {
			projectArguments = append(slices.Clone(arguments), "--no-cache")
		}
		openPath := ""
		if activeOpen != nil {
			openPath = filepath.Join(yieldDirectory, fmt.Sprintf("open-%d.json", index))
		}
		group.Add(1)
		go func() {
			defer group.Done()
			running <- struct{}{}
			defer func() { <-running }()
			runs[index] = runProject(executable, found.Root, project, projectArguments, yieldFile, openPath, children,
				activeOutput.Mode == outputJSON)
		}()
	}
	group.Wait()

	// In the default view a project's own run ends on its footer, which leads with its path, so the projects
	// follow one another with no heading between them; `--verbose` keeps the sections and their account.
	account := accountOutput(out)
	worst := 0
	if activeOpen != nil {
		activeOpen.Checked = true
	}
	for _, checked := range runs {
		if activeOpen != nil {
			if checked.open == nil || !checked.open.Checked || checked.err != nil {
				activeOpen.Checked = false
			}
			if checked.open != nil {
				activeOpen.HasFindings = activeOpen.HasFindings || checked.open.HasFindings
				for _, path := range checked.open.Paths {
					activeOpen.add(path)
				}
			}
		}
		fmt.Fprintf(account, "%s\n", sectionHeading(checked.project))
		out.Write(checked.output)
		if len(checked.output) > 0 && checked.output[len(checked.output)-1] != '\n' {
			fmt.Fprintln(out)
		}
		// Under `--json` stdout is records only, so what a child wrote to stderr, an engine's note or a
		// file it could not format, goes to stderr, where a single project's run puts it too.
		streams.Errors.Write(checked.standardError)
		if checked.err != nil {
			fmt.Fprintf(failures, "cohere: %s\n",
				sentence.FromError(fmt.Errorf("the %s run did not finish: %w", checked.project.Engine, checked.err)))
		}
		// Each project's stderr where its section is, as when it was written straight to the terminal.
		showWarning()
		fmt.Fprintln(account)
		if checked.exitCode > worst {
			worst = checked.exitCode
		}
	}

	fmt.Fprintf(account, "projects under %s:\n", found.Root)
	for _, checked := range runs {
		verdict := "green"
		if checked.exitCode != 0 {
			verdict = fmt.Sprintf("failed, exit %d", checked.exitCode)
		}
		fmt.Fprintf(
			account,
			"  %s (%s): %s in %s\n",
			projectLabel(checked.project),
			checked.project.Engine,
			verdict,
			round(checked.elapsed),
		)
	}
	for _, note := range found.notes() {
		fmt.Fprintf(account, "  %s\n", note)
	}
	fmt.Fprintln(account, summaryLine(found, runs))
	// The overall verdict, last, in the human views. Under `--json` each project's own summary line, with its
	// label, is the record, and a program reads the worst from them.
	facts := overallFactsOf(runs, time.Since(processStart))
	facts.CoverageUnknown = len(found.SwiftCoverage.FailedQueries)
	facts.UnclaimedSwift = found.UnclaimedSwift
	if activeOutput.Mode != outputJSON {
		fmt.Fprintln(out, overallFooter(facts, activeOutput.Style))
	}
	// A run stopped by a signal ends the way the signal would have ended it, after every child has, so the
	// caller that sent it reads it as stopped and not as a verdict.
	if received := children.received(); received != 0 {
		return 128 + received
	}
	return worst
}

// swiftYield is what a Swift project's run leaves to others: the directories of the other running Swift
// projects strictly below it, absolute, each checked by its own run. A nested package that runs on its own and
// an Xcode project inside a package's directory are both there, and the run's not-checked notes leave their
// files out, since those runs account for them. A package another run covers is not, since the covering run
// checks its files itself, and found.Projects holds only the projects that run.
func (found discovery) swiftYield(project discoveredProject) projectYield {
	yield := projectYield{}
	for _, other := range found.Projects {
		if other.Engine == engineSwift && isBelow(other.Directory, project.Directory) {
			yield.Directories = append(yield.Directories, filepath.Join(found.Root, filepath.FromSlash(other.Directory)))
		}
	}
	sort.Strings(yield.Directories)
	return yield
}

// overallFactsOf is what the overall verdict says of the projects' runs: how many of each engine, the ones
// that failed with their exits, and the ones whose run did not finish, which checked nothing.
func overallFactsOf(runs []projectRun, total time.Duration) overallFacts {
	facts := overallFacts{Total: total, ProjectsByEngine: map[string]int{}}
	for _, checked := range runs {
		facts.ProjectsByEngine[string(checked.project.Engine)]++
		switch {
		case checked.err != nil:
			facts.Unfinished++
		case checked.exitCode != 0:
			facts.Failed = append(
				facts.Failed,
				fmt.Sprintf("%s (exit %d)", projectLabel(checked.project), checked.exitCode),
			)
		}
	}
	return facts
}

// projectRunsAtOnce is how many projects' runs share the machine: a quarter of its cores, and at least one.
// Each run checks its own program on every core already, so the bound costs little time, and it is what
// keeps a laptop alive on a repository with a hundred tsconfigs.
func projectRunsAtOnce() int {
	return max(1, runtime.NumCPU()/4)
}

// runProject runs one project's check as a child and keeps everything it printed. yieldFile, when set,
// names what it yields to nearer projects (see ownership.go). The child is held in children while it runs,
// so a signal to this run reaches it.
//
// separateStandardError keeps the child's stderr apart from its stdout, for `--json`, whose stdout a program
// reads one record per line. The human views keep the two together, in the order the child wrote them.
func runProject(
	executable string,
	root string,
	project discoveredProject,
	arguments []string,
	yieldFile string,
	openPath string,
	children *projectChildren,
	separateStandardError bool,
) projectRun {
	directory := filepath.Join(root, filepath.FromSlash(project.Directory))
	prefix := []string{"--directory", directory}
	if project.ConfigFile != "" {
		prefix = append(prefix, "--tsconfig", project.ConfigFile)
	}
	child := exec.Command(executable, append(prefix, arguments...)...)
	// Started in the project, so its report does not add that it checked somewhere other than where it
	// started: the section's heading already says which project it is.
	child.Dir = directory
	// The verdict descriptor is this run's: a child's verdict reaches this process as its exit code, and
	// only the merged one may answer the caller.
	child.Env = append(withoutVariable(withoutVariable(os.Environ(), VerdictVariable), projectYieldVariable),
		projectEngineVariable+"="+string(project.Engine), projectLabelVariable+"="+projectLabel(project))
	if yieldFile != "" {
		child.Env = append(child.Env, projectYieldVariable+"="+yieldFile)
	}
	if openPath != "" {
		child.Env = append(withoutVariable(child.Env, openReportVariable), openReportVariable+"="+openPath)
	}
	var output bytes.Buffer
	var standardError bytes.Buffer
	child.Stdout = &output
	child.Stderr = &output
	if separateStandardError {
		child.Stderr = &standardError
	}

	started := time.Now()
	runError := children.run(child)
	run := projectRun{
		project:       project,
		output:        output.Bytes(),
		standardError: standardError.Bytes(),
		elapsed:       time.Since(started),
	}
	if openPath != "" {
		run.open = readOpenReport(openPath)
	}
	var exited *exec.ExitError
	switch {
	case runError == nil:
	case errors.As(runError, &exited) && exited.ExitCode() > 0:
		run.exitCode = exited.ExitCode()
	default:
		// Killed by a signal, or never started: a project that was not checked is a failure.
		run.exitCode = 1
		run.err = runError
	}
	return run
}

// withoutVariable is an environment with one variable removed.
func withoutVariable(environment []string, name string) []string {
	kept := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, name+"=") {
			kept = append(kept, entry)
		}
	}
	return kept
}

// projectLabel names a project by its directory, the root by ".", or by its tsconfig's path when that is
// not the directory's tsconfig.json.
func projectLabel(project discoveredProject) string {
	if project.ConfigFile != "" {
		return path.Join(project.Directory, project.ConfigFile)
	}
	return project.Directory
}

// sectionHeading opens a project's section.
func sectionHeading(project discoveredProject) string {
	return fmt.Sprintf("== %s (%s) ==", projectLabel(project), project.Engine)
}

// summaryLine closes the report: how many projects were checked, of which engines, how many failed, what
// was found and not run or not checked, and what the walk did not enter.
func summaryLine(found discovery, runs []projectRun) string {
	counts := map[projectEngine]int{}
	failed := 0
	checkedCount := 0
	for _, checked := range runs {
		checkedCount++
		counts[checked.project.Engine]++
		if checked.exitCode != 0 {
			failed++
		}
	}
	engines := []string{}
	for _, engine := range []projectEngine{engineTypeScript, engineSwift, engineGo} {
		if counts[engine] > 0 {
			engines = append(engines, fmt.Sprintf("%d %s", counts[engine], engine))
		}
	}
	verdict := "every one green"
	if failed > 0 {
		verdict = fmt.Sprintf("%d failed", failed)
	}
	var notRunParts []string
	if len(found.Ownership.Solutions) > 0 || len(found.Ownership.Yielding) > 0 {
		notRunParts = append(
			notRunParts,
			fmt.Sprintf("%d solution tsconfigs, %d whose every file a nearer tsconfig owns",
				len(found.Ownership.Solutions), len(found.Ownership.Yielding)),
		)
	}
	if len(found.SwiftCoverage.Covered) > 0 {
		notRunParts = append(
			notRunParts,
			fmt.Sprintf("%d Swift packages another package's run covers", len(found.SwiftCoverage.Covered)),
		)
	}
	notRun := ""
	if len(notRunParts) > 0 {
		notRun = "; not run: " + strings.Join(notRunParts, ", ")
	}
	// Beside what was not run, since these files were not checked either, and nothing would have.
	notChecked := ""
	if len(found.UnclaimedSwift) > 0 {
		files := 0
		for _, unclaimed := range found.UnclaimedSwift {
			files += unclaimed.Files
		}
		notChecked = fmt.Sprintf(
			"; not checked: %s no Swift package checks, in %s",
			counted(files, "Swift file", "Swift files"),
			counted(len(found.UnclaimedSwift), "directory", "directories"),
		)
	}
	return fmt.Sprintf(
		"projects: %d checked (%s), %s%s%s; not searched for projects: %d directories .gitignore ignores, "+
			"%d dependency, build, cache or fixture directories, %d nested repositories",
		checkedCount,
		strings.Join(engines, ", "),
		verdict,
		notRun,
		notChecked,
		found.Ignored,
		found.NeverDescended,
		len(found.NestedRepositories)+found.Submodules,
	)
}
