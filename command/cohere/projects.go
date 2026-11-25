package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/command_line/sentence"
	"github.com/system-inc/cohere/internal/command_line/streams"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/types/program"
)

// projectKindVariable names the child a child run checks its --directory with. A directory holding both
// a tsconfig.json and a Package.swift is two projects, and --directory alone reads it as TypeScript's, so
// the run that checks its Swift says which language it is.
const projectKindVariable = "COHERE_PROJECT_KIND"

// runDepthVariable marks a run another cohere run started to check one of its projects (#excff9x).
//
// A project's run is this same binary, given `--directory` and the child (projectKindVariable), and a child whose
// guard against discovering failed would find the same projects and start a run for each, without end: on Nov 10 a
// review mutant that removed one such guard forked about 7,700 copies and filled the machine's process table. So the
// parent marks every child, and a run that finds the mark refuses to start runs of its own, which ends any recursion
// one level down, as a named refusal rather than a hang, however discovery is broken. The guard stays: this is the
// wall behind it.
const runDepthVariable = "COHERE_RUN_DEPTH"

// projectLabelVariable names the project a child run checks, by its path under the root, so the run's own
// footer can say which project it is about (#ytqqv8v).
const projectLabelVariable = "COHERE_PROJECT_LABEL"

// discoveredListings are the directory listings discovery read, when the one project it found runs in this
// process, for its build to serve the tsconfig's include enumeration from. Nil otherwise. See #bjv0tg4.
var discoveredListings *program.DirectoryListings

// narrowingFlags are the flags beside `--directory` that name one project or answer about one, so a run given
// any of them never discovers (discoveryApplies).
var narrowingFlags = []string{
	"tsconfig", "rules", "rules-enabled", "print-config", "version", "cache-dump", "explain", "stdin-filepath",
	"profile",
}

// discoveryApplies reports whether a run discovers its projects. Anything that names one project, a
// directory, a tsconfig or a path, narrows the run to it as it always did, and so do the listings, the
// explanations and the editor's save, which answer about one project.
//
// A lint config does not: it says which rules apply, not which project, so every project discovered runs
// under it. It used to narrow, and the quiet hundred's harness, which names one, checked 2 of TanStack
// Query's thousand files and refused prisma outright (#wvgxtey).
func discoveryApplies(given map[string]bool, arguments []string) bool {
	if len(arguments) > 0 || given["directory"] {
		return false
	}
	return !slices.ContainsFunc(narrowingFlags, func(name string) bool { return given[name] })
}

// namedDirectoryHoldsSeveral reports whether a run names a directory with `--directory`, and nothing else that
// narrows it, and that directory is more than one language's project, as adamic's root holds a tsconfig.json and a
// go.work (#pswhv5q). `--directory` is a `cd`, so such a run discovers from that directory and checks every project
// discovery finds there, nested ones included, as `cd` there and `cohere` would, where it once read the directory as
// TypeScript's alone and checked nothing else, in silence. A child run, which names the child, is one project as
// ever: it never discovers, so a project's run never starts runs of its own.
func namedDirectoryHoldsSeveral(
	given map[string]bool,
	arguments []string,
	location projectLocation,
	locateError error,
) bool {
	if !given["directory"] || len(arguments) > 0 || locateError != nil || os.Getenv(projectKindVariable) != "" {
		return false
	}
	if slices.ContainsFunc(narrowingFlags, func(name string) bool { return given[name] }) {
		return false
	}
	return len(kindsAt(location.Root)) > 1
}

// checkDiscoveredProjects discovers the projects under the root and, when there is more than the one the
// walk up located, checks them all and reports checked with the merged exit code. With only that one it
// prints what discovery passed over and leaves the run to check it in this process.
//
// lintConfigFileName is the lint config the command line named, absolute, or empty: every project's run
// gets it, since a path typed here would resolve against each project's directory there. formats is whether
// the run formats, which decides whether a root no TypeScript project holds gets its format project. given is
// the flags the command line set, by name. choice is the languages the two flags named, nil for every one, whose
// projects alone run; the rest are named as left out (#s7gt4bj).
func checkDiscoveredProjects(
	location projectLocation,
	locateError error,
	workingDirectory string,
	lintConfigFileName string,
	formats bool,
	given map[string]bool,
	atNamedDirectory bool,
	choice languageChoice,
) (int, bool, error) {
	root := discoveryRoot(location, locateError, workingDirectory)
	if atNamedDirectory {
		root = location.Root
	}
	ignoreEntries, rootIgnoreEntriesError := rootIgnoreEntries(root)
	if rootIgnoreEntriesError != nil {
		return 0, false, rootIgnoreEntriesError
	}
	// Asked here because a Swift-only run loads no lint configuration, so this is the one read of the root's
	// file that every run makes. A file the command line named is cohere-swift's to read wherever it
	// sits, and the projects' runs are handed it (see configuration.CheckSwiftBlockHasAReader).
	if settings := filepath.Join(
		root,
		"CohereSettings.json",
	); settings != lintConfigFileName && isRegularFile(settings) {
		if checkSwiftBlockHasAReaderError := configuration.CheckSwiftBlockHasAReader(
			settings,
		); checkSwiftBlockHasAReaderError != nil {
			return 0, false, checkSwiftBlockHasAReaderError
		}
	}
	found, discoverProjectsError := discoverProjects(root, configuration.IgnorePatternsFrom(ignoreEntries))
	if discoverProjectsError != nil {
		return 0, false, discoverProjectsError
	}
	// The folder check's setting is read only when the walk found a folder to report, so a tree with none pays no
	// load of its chain, and a run that does not lint reports none (see folder_names.go).
	if len(found.Folders) > 0 && runLints(given) {
		var folderCheckSeverityError error
		if found.FolderSeverity, folderCheckSeverityError = folderCheckSeverity(
			root,
			lintConfigFileName,
		); folderCheckSeverityError != nil {
			return 0, false, folderCheckSeverityError
		}
	}
	if found.FolderSeverity == configuration.SeverityOff {
		found.Folders = nil
	}
	var resolveOwnershipError error
	if found.Ownership, resolveOwnershipError = resolveOwnership(&found); resolveOwnershipError != nil {
		return 0, false, resolveOwnershipError
	}
	// Before the one-project check, so a package and the dependency its run checks are the one project they
	// are, and run here as `cohere --directory` on the package would.
	found.SwiftCoverage = resolveSwiftCoverage(&found)
	// Only beside a project found, so a root with nothing to check still fails saying so, rather than
	// formatting a README and reading green.
	if formats && len(found.Projects) > 0 {
		found.addRootFormatProject()
	}
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
	// After the format project is added, so the choice takes it as it chooses any, and after a tree with no
	// project is refused, which it is whatever languages were named.
	found.chooseLanguages(choice, formats)
	if len(found.Projects) == 0 && len(found.NotFound) == len(choice) {
		return 0, false, fmt.Errorf("this run names %s, and nothing here is any of them: here is %s, so nothing was "+
			"checked", sayList(choice), sayList(found.LeftOutLanguages))
	}
	if len(found.Projects) == 0 {
		// A named language is here, and only a format walk would check it, which this run does not make.
		return 0, false, fmt.Errorf("this run names %s, which a run checks here only by formatting them, and this run "+
			"does not format, so nothing was checked: run it without --types, --lint or --no-format", sayList(choice))
	}
	// On standard error, where a run says what it was refused, so every view says it.
	if len(found.NotFound) > 0 {
		fmt.Fprintf(streams.Errors, "cohere: %s\n", sentence.Of(fmt.Sprintf(
			"this run names %s, and nothing here is any of them, so it checked nothing of them",
			sayList(found.NotFound))))
	}
	if refuseError := refuseGoOnlySwitchesWithoutGo(found, given, flagValue); refuseError != nil {
		return 0, false, refuseError
	}
	// Said by the one project's run at its end, or here before several projects' sections: see ignore_account.go.
	discoveredIgnores = append(
		ignoreAccountsOf(ignoreEntries, found.IgnoreMatched, found.Root),
		defaultIgnoreAccounts(found.DefaultMatched)...,
	)
	// A folder finding is this run's to print and count, after its project's run, which then runs in its own
	// process as one of several would, so the project's recorded run never holds a folder verdict.
	//
	// A run that named languages runs its projects as one of several would, even when one is left: the project's run
	// is then the plain run of that project, which replays as one, and this run, which names what it left out, never
	// does, since discovery decides afresh each time what there is to leave out (#s7gt4bj).
	if found.isTheLocatedProject(location, locateError) && len(found.Folders) == 0 && choice == nil {
		// The project runs here, and its build lists the directories discovery just did.
		discoveredListings = found.Listings
		for _, note := range found.notes() {
			fmt.Fprintf(accountOutput(streams.Errors), "cohere: %s\n", note)
		}
		showWarning()
		return 0, false, nil
	}
	// The one project's summary carries the list under `--json`. With several, each project's summary is its own,
	// so the list is a record of this run's, once.
	writeDiscoveredIgnores(streams.Output)
	if activeOutput.Mode == outputJson && len(discoveredIgnores) > 0 {
		writeJsonLine(streams.Output, ignorePatternsJson{Kind: "ignorePatterns", IgnorePatterns: discoveredIgnores})
	}
	if activeOutput.Mode == outputJson && choice != nil {
		for _, record := range choiceJsonsOf(choice, found) {
			writeJsonLine(streams.Output, record)
		}
	}
	showWarning()
	return runProjects(found, childArguments(os.Args[1:], lintConfigFileName, atNamedDirectory), streams.Output),
		true, nil
}

// childArguments are the arguments every project's run gets: this run's, with a named lint config made
// absolute, and without the named directory when the run is at one (namedDirectoryHoldsSeveral), since each
// project's run names its own. Never `--languages` or `--formats`: this run chose the projects, and each one's run
// is that project's plain run, so it replays as one (#s7gt4bj). Discovery runs only when no path was named, so every
// argument is a flag, and a flag's value is either joined to it with `=` or the next argument, since no boolean flag
// takes a separate one.
func childArguments(arguments []string, lintConfigFileName string, atNamedDirectory bool) []string {
	dropped := []string{languagesFlagName, formatsFlagName}
	if lintConfigFileName != "" {
		dropped = append(dropped, "lint-config")
	}
	if atNamedDirectory {
		dropped = append(dropped, "directory")
	}
	kept := withoutValuedFlags(arguments, dropped)
	if lintConfigFileName == "" {
		return kept
	}
	return append(kept, "--lint-config", lintConfigFileName)
}

// withoutValuedFlags is arguments less each flag named in dropped and its value, joined to it with `=` or the next
// argument. Every argument is a flag here (see childArguments).
func withoutValuedFlags(arguments []string, dropped []string) []string {
	kept := make([]string, 0, len(arguments)+2)
	for index := 0; index < len(arguments); index++ {
		name, _, joined := strings.Cut(strings.TrimLeft(arguments[index], "-"), "=")
		if slices.Contains(dropped, name) && strings.HasPrefix(arguments[index], "-") {
			if !joined {
				index++
			}
			continue
		}
		kept = append(kept, arguments[index])
	}
	return kept
}

// lintsLocatedProject reports whether a run discovery does not apply to lints its located project, whole or by named
// paths: not one of discovery's children, which its parent already judged, nor a run that only answers, explains
// one file or formats one from standard input (#zyq63kk).
func lintsLocatedProject(given map[string]bool, locateError error) bool {
	return locateError == nil && os.Getenv(projectKindVariable) == "" && runLints(given) && !answersOnly(given) &&
		!given["explain"] && !given["stdin-filepath"]
}

// lintsWholeLocatedProject reports whether such a run lints its project whole: one given `--directory` into one
// child's project, `--tsconfig`, or `--profile`, with no path argument or only the project's root as its path, which
// is the bare run (#xd2q11d). A path below the root narrows the run to those files, partial by its nature, so its
// folders are not judged, and the run says so (noteFolderCheckSkipped).
func lintsWholeLocatedProject(
	given map[string]bool,
	arguments []string,
	locateError error,
	location projectLocation,
) bool {
	return lintsLocatedProject(given, locateError) &&
		(len(arguments) == 0 || namesOnlyTheRoot(arguments, location.ArgumentBase, location.Root))
}

// checkNarrowedProjectFolders judges the folders of a located project a narrowed run lints whole, as discovery
// judges a root's (#zyq63kk): the same walk from the project's root, its gaps, and judgeFolder. A person running
// cohere in their project with `--directory` or `--tsconfig` expects its folders judged, and before this such a run
// printed green over a docs/. With a finding standing, the project runs in its own process, as one of several
// projects would, and this run prints the folders, counts them in the footer and fails, exactly as the discovery
// path does; reported is then true. With none, the run goes on in this process as it always did.
//
// flags are the run's arguments less its paths, which are the root's alone when it has any, and which the project's
// run, named by its directory, needs no more. given is the flags the run was typed, read for a switch only a Go run
// takes, which a project that is no Go one's refuses here rather than run without (refuseGoOnlySwitchesWithoutGo):
// the project's run is handed none of them, and would read as one that did what was asked.
func checkNarrowedProjectFolders(
	location projectLocation,
	lintConfigFileName string,
	flags []string,
	given map[string]bool,
) (int, bool, error) {
	ignoreEntries, rootIgnoreEntriesError := rootIgnoreEntries(location.Root)
	if rootIgnoreEntriesError != nil {
		return 0, false, rootIgnoreEntriesError
	}
	walked, discoverProjectsError := discoverProjects(location.Root, configuration.IgnorePatternsFrom(ignoreEntries))
	if discoverProjectsError != nil {
		return 0, false, discoverProjectsError
	}
	if len(walked.Folders) == 0 {
		return 0, false, nil
	}
	severity, folderCheckSeverityError := folderCheckSeverity(location.Root, location.LintConfigFileName)
	if folderCheckSeverityError != nil {
		return 0, false, folderCheckSeverityError
	}
	if severity == configuration.SeverityOff {
		return 0, false, nil
	}
	located := discoveredProject{Directory: ".", Kind: location.Kind}
	if base := filepath.Base(
		location.ConfigFileName,
	); location.Kind == projectKindTypeScriptProgram && base != projectMarker {
		located.ConfigFile = base
	}
	// Only the located project and the folders: what else the walk found is not this run's to check or name.
	found := discovery{
		Root:           location.Root,
		Projects:       []discoveredProject{located},
		Folders:        walked.Folders,
		FolderSeverity: severity,
	}
	if refuseError := refuseGoOnlySwitchesWithoutGo(found, given, flagValue); refuseError != nil {
		return 0, false, refuseError
	}
	// The project's run is named by the directory and tsconfig runProject gives it, so this run's own go. A profiled
	// run's project profiles itself to a file of its own, named `project` (contractProfilePath), since this process
	// profiles itself to the file the caller named, and two processes writing one file leave neither profile whole
	// (#r33n96f).
	arguments := childArguments(withoutValuedFlags(flags, []string{"directory", "tsconfig", "profile"}),
		lintConfigFileName, false)
	if given["profile"] && flagValue("profile") != "" {
		arguments = append(arguments, "--profile",
			contractProfilePath(location.WorkingDirectory, flagValue("profile"), "project"))
	}
	return runProjects(found, arguments, streams.Output), true, nil
}

// goOnlySwitches are the switches only a Go run takes (goFlags), each with what it asks of a Go run, which its refusal
// says: every other language's child is run without them, so one command line at a root holding several projects
// narrows the Go one's check to the host's build, or maps the Go one's run, where Swift's run would refuse the switch
// and TypeScript's has no other build to leave out and no child's phases to map (#3v2zp7g).
var goOnlySwitches = map[string]string{
	"host-build-only": "narrows which builds of a Go project's files are checked",
	"timeline":        "maps when each phase and each build of a Go run began and ended",
}

// refuseGoOnlySwitches refuses a run that is no Go project's and was typed one of goOnlySwitches, rather than
// ignoring it, since a flag accepted and ignored reads as a run that did what was asked. Discovery hands them to a Go
// project's run alone (withoutGoOnlySwitches) and refuses them at a root with no Go project
// (refuseGoOnlySwitchesWithoutGo), so here only a TypeScript run discovery leaves alone, such as one naming
// --directory, meets this; Swift's refuses them as the child's arguments are built (contractArguments). Sorted, so a
// command line naming two of them is refused for the same one every time.
func refuseGoOnlySwitches(given map[string]bool, value func(string) string) error {
	for _, name := range slices.Sorted(maps.Keys(goOnlySwitches)) {
		if given[name] && value(name) == "true" {
			return fmt.Errorf("--%s %s, and this is no Go project, so nothing was checked", name,
				goOnlySwitches[name])
		}
	}
	return nil
}

// refuseGoOnlySwitchesWithoutGo refuses a run at a discovered root that was typed one of goOnlySwitches when no
// project found is a Go one. runProjects hands those switches to a Go project's run alone, so at a root of only
// TypeScript and Swift projects they would reach no run, and the run would read as one that did what was asked.
// A root holding a Go project takes them, and its other projects run without them.
func refuseGoOnlySwitchesWithoutGo(found discovery, given map[string]bool, value func(string) string) error {
	for _, project := range found.Projects {
		if project.Kind == projectKindGoModule {
			return nil
		}
	}
	return refuseGoOnlySwitches(given, value)
}

// withoutGoOnlySwitches is arguments less each of goOnlySwitches.
func withoutGoOnlySwitches(arguments []string) []string {
	return withoutSwitches(arguments, slices.Collect(maps.Keys(goOnlySwitches)))
}

// swiftUntimedSwitches are the switches a Swift project's run at a discovered root is run without: `--timing`, which
// cohere-swift does not implement and refuses, so one command line at a root holding Swift beside TypeScript or
// Go times those and says the Swift project ran untimed (timingSkipsSwiftNote), rather than refusing every project's
// run over one language's gap (#3apgjer).
var swiftUntimedSwitches = []string{"timing"}

// timingSkipsSwiftNote is what a run at a discovered root says when it was typed `--timing` and holds a Swift project,
// which runs without it: the labels of those projects, or empty when there is nothing to say.
func timingSkipsSwiftNote(found discovery, labels []string, arguments []string) string {
	if !switchOn(arguments, "timing") {
		return ""
	}
	var untimed []string
	for index, project := range found.Projects {
		if project.Kind == projectKindSwiftPackage {
			untimed = append(untimed, labels[index])
		}
	}
	if len(untimed) == 0 {
		return ""
	}
	return fmt.Sprintf("--timing measures the TypeScript and Go projects only, since cohere-swift does not "+
		"implement it yet, so %s ran without it", strings.Join(untimed, ", "))
}

// switchOn reports whether arguments turn the switch name on, as `--name` or `--name=true`, one dash or two.
func switchOn(arguments []string, name string) bool {
	on := false
	for _, argument := range arguments {
		spelled, value, valued := strings.Cut(strings.TrimLeft(argument, "-"), "=")
		if strings.HasPrefix(argument, "-") && spelled == name {
			on = !valued || value == "true"
		}
	}
	return on
}

// withoutSwitches is arguments less each of names, as `--name` or `--name=value`, one dash or two. Every argument is a
// flag here (see childArguments), and a boolean flag takes no separate value.
func withoutSwitches(arguments []string, names []string) []string {
	kept := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		name, _, _ := strings.Cut(strings.TrimLeft(argument, "-"), "=")
		if strings.HasPrefix(argument, "-") && slices.Contains(names, name) {
			continue
		}
		kept = append(kept, argument)
	}
	return kept
}

// projectRun is one project's run, as its child reported it.
type projectRun struct {
	open    *openReport
	project discoveredProject
	// label is the project's name in this run, unique among its projects (see runLabels).
	label  string
	output []byte
	// standardError is the child's stderr when it is kept apart from output, under `--json`, and empty
	// otherwise, when the two share output in the order they were written.
	standardError []byte
	exitCode      int
	runError      error
	elapsed       time.Duration
	// summary is what the project's run handed back for the root's one line, nil when it handed none or was not
	// asked to (combined_footer.go).
	summary *projectSummary
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
	if depth := os.Getenv(runDepthVariable); depth != "" {
		// Exit 2, "did not run", so the run above reports this project as not checked.
		failRunProjects(out, fmt.Errorf("this run was started by another cohere run to check one project (%s=%s) "+
			"and was about to start runs for %d projects of its own, so it refused, and nothing was checked: its "+
			"discovery found several projects where it was given one, which is a cohere bug to report",
			runDepthVariable, depth, len(found.Projects)))
		return 2
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		failRunProjects(out, fmt.Errorf("finding this binary to run each project: %w", executableError))
		return 1
	}
	yieldDirectory, temporaryDirectoryError := os.MkdirTemp("", "cohere-yield-")
	if temporaryDirectoryError != nil {
		failRunProjects(out, fmt.Errorf("making a place for what each project yields: %w", temporaryDirectoryError))
		return 1
	}
	// Ignored: a temporary directory that outlives the run costs only its space, and nothing reads it again.
	defer os.RemoveAll(yieldDirectory)

	children := forwardSignalsToChildren()
	defer children.stop()

	runs := make([]projectRun, len(found.Projects))
	labels := runLabels(found.Projects)
	// The default view ends on one line for every project, so each hands its summary back instead of printing a
	// footer. --phases keeps each project's own, since phase times don't sum across projects side by side.
	oneLine := activeOutput.Mode == outputHuman && !activeOutput.Phases
	// On standard error, where a run says what it was refused, so every view, `--json` included, says it.
	if note := timingSkipsSwiftNote(found, labels, arguments); note != "" {
		fmt.Fprintf(streams.Errors, "cohere: %s\n", note)
	}
	var launches []projectLaunch
	for index, project := range found.Projects {
		label := projectLabel(project)
		// A Swift project and a TypeScript project in one directory share a label, and what one yields is
		// nothing to the other, so a Swift project's yield is its own.
		yield := found.Ownership.Yields[label]
		switch project.Kind {
		case projectKindSwiftPackage:
			yield = found.swiftYield(project)
		case projectKindFolder:
			yield = found.formatYield(project)
		}
		// Every format walk leaves out the files of the languages the two flags did not name, which the yield's
		// fingerprint then carries into the project run's replay key (#s7gt4bj).
		if project.Kind == projectKindTypeScriptProgram || project.Kind == projectKindFolder {
			yield.LeftOutExtensions = found.Choice.leftOutExtensions()
		}
		yieldFile, writeProjectYieldError := writeProjectYield(yield, yieldDirectory)
		if writeProjectYieldError != nil {
			runs[index] = projectRun{
				project:  project,
				label:    labels[index],
				exitCode: 1,
				runError: fmt.Errorf("writing what it yields: %w", writeProjectYieldError),
			}
			continue
		}
		projectArguments := arguments
		if project.Kind != projectKindGoModule {
			projectArguments = withoutGoOnlySwitches(arguments)
		}
		if project.Kind == projectKindSwiftPackage {
			projectArguments = withoutSwitches(projectArguments, swiftUntimedSwitches)
		}
		// TypeScript runs alone: the cache it turns off is cohere's table in the directory, which a Swift run
		// never reads or writes, so a Swift run in the same directory keeps its own cache.
		if found.Ownership.SharedDirectory[label] && project.Kind == projectKindTypeScriptProgram {
			projectArguments = append(slices.Clone(projectArguments), "--no-cache")
		}
		openPath := ""
		if activeOpen != nil {
			openPath = filepath.Join(yieldDirectory, fmt.Sprintf("open-%d.json", index))
		}
		summaryPath := ""
		if oneLine {
			summaryPath = filepath.Join(yieldDirectory, fmt.Sprintf("summary-%d.json", index))
		}
		launches = append(launches, projectLaunch{index, project, projectArguments, yieldFile, openPath, summaryPath})
	}
	makeProjectCacheDirectories(found.Root, launches)
	var group sync.WaitGroup
	running := make(chan struct{}, projectRunsAtOnce())
	for _, launch := range launches {
		group.Add(1)
		go func() {
			defer group.Done()
			running <- struct{}{}
			defer func() { <-running }()
			runs[launch.index] = runProject(executable, found.Root, launch.project, labels[launch.index],
				launch.arguments, launch.yieldFile, launch.openPath, launch.summaryPath, children,
				activeOutput.Mode == outputJson)
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
			if checked.open == nil || !checked.open.Checked || checked.runError != nil {
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
		// Under `--json` stdout is records only, so what a child wrote to stderr, a child's note or a
		// file it could not format, goes to stderr, where a single project's run puts it too.
		streams.Errors.Write(checked.standardError)
		if checked.runError != nil {
			unfinished := fmt.Errorf("the %s run did not finish: %w", checked.project.Kind.label(), checked.runError)
			failureLine(out, unfinished)
			// Under `--json` the project's record says it too, since a run that never finished printed none.
			if activeOutput.Mode == outputJson {
				unfinishedSummary := runSummary{Label: checked.label, Total: checked.elapsed}
				unfinishedSummary.Gaps.PhasesNotRun = unfinishedSummary.phasesNotRun()
				line := summaryAsJson(unfinishedSummary)
				line.Verdict = "fail"
				line.Error = sentence.FromError(unfinished)
				writeJsonLine(out, line)
			}
		}
		// Each project's stderr where its section is, as when it was written straight to the terminal.
		showWarning()
		fmt.Fprintln(account)
		if checked.exitCode > worst {
			worst = checked.exitCode
		}
	}
	// The folders are the root's own findings, after every project's, and fail the run as a project's finding does.
	printFolderFindings(out, found)
	folderSummary := folderSummaryOf(found)
	if folderSummary.failed() {
		worst = max(worst, 1)
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
			checked.label,
			checked.project.Kind.label(),
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
	facts.UnusedIgnores = unusedIgnoreCount(discoveredIgnores)
	facts.CoverageUnknown = len(found.SwiftCoverage.FailedQueries)
	facts.FolderFindings, facts.FolderWarnings = folderSummary.Findings, folderSummary.WarningFindings
	facts.ProgramBuilds = builtPrograms()
	facts.LeftOut = found.LeftOutLanguages
	switch {
	case oneLine:
		fmt.Fprintln(out, combinedFooter(runs, facts, facts.Total, activeOutput.Style))
	case activeOutput.Mode != outputJson:
		fmt.Fprintln(out, overallFooter(facts, activeOutput.Style))
	default:
		// A build this run made before its projects ran, as a coverage query does, is a record of its own.
		fmt.Fprint(out, programBuildLines(facts.ProgramBuilds))
	}
	// A run stopped by a signal ends the way the signal would have ended it, after every child has, so the
	// caller that sent it reads it as stopped and not as a verdict.
	if received := children.received(); received != 0 {
		return 128 + received
	}
	return worst
}

// projectLaunch is one project's run as runProjects prepared it, before any project's run starts: the project, by its
// index among the runs, the arguments its run gets, and the files it is handed.
type projectLaunch struct {
	index       int
	project     discoveredProject
	arguments   []string
	yieldFile   string
	openPath    string
	summaryPath string
}

// makeProjectCacheDirectories makes the cache directory of every project whose run keeps a run cache, before any
// project's run starts (#d46ks7h). Each run makes its own as it starts, and a project's directory can be one another
// project's run reads: a Swift package below a Go module is a directory the Go census lists, so the Swift run's
// `.cache` appearing there mid-run made the Go run refuse to record, as it should, and a fresh tree's first two runs
// were both cold. Made here, every such directory is settled before any run reads it. Only where the project's own run
// would make it: a run the cache is off for, or whose arguments no run cache admits, makes none, and neither does
// this. A directory that can't be made is the project's run's to say, which tries again.
func makeProjectCacheDirectories(root string, launches []projectLaunch) {
	if cacheOff {
		return
	}
	for _, launch := range launches {
		if !runCacheAdmits(runCacheArguments(launch.arguments, true)) {
			continue
		}
		// Ignored: the project's run makes it again as it starts, and says so when it can't.
		_ = cache_directory.Make(cache_directory.Of(filepath.Join(root, filepath.FromSlash(launch.project.Directory))))
	}
}

// failRunProjects says why the projects could not be run at all: as a failure line, and under `--json` as a
// failed summary line too, so the stream still ends on one.
func failRunProjects(out *bufio.Writer, runError error) {
	failureLine(out, runError)
	writeFailedRunSummary(out, runError)
}

// failureLine is one of this run's own failure lines, as the sentence a person reads: with the report in the
// human views, and on stderr under `--json`, whose stdout is records only.
func failureLine(out *bufio.Writer, runError error) {
	failures := out
	if activeOutput.Mode == outputJson {
		failures = streams.Errors
	}
	fmt.Fprintf(failures, "cohere: %s\n", sentence.FromError(runError))
}

// overallFactsOf is what the overall verdict says of the projects' runs: how many of each language, the ones
// that failed with their exits, and the ones whose run did not finish, which checked nothing.
func overallFactsOf(runs []projectRun, total time.Duration) overallFacts {
	facts := overallFacts{Total: total, ProjectsByKind: map[string]int{}}
	for _, checked := range runs {
		facts.ProjectsByKind[checked.project.Kind.label()]++
		switch {
		case checked.runError != nil:
			facts.Unfinished++
		case checked.exitCode != 0:
			facts.Failed = append(
				facts.Failed,
				fmt.Sprintf("%s (exit %d)", checked.label, checked.exitCode),
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
	label string,
	arguments []string,
	yieldFile string,
	openPath string,
	summaryPath string,
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
		projectKindVariable+"="+string(project.Kind), projectLabelVariable+"="+label, runDepthVariable+"=1")
	if yieldFile != "" {
		child.Env = append(child.Env, projectYieldVariable+"="+yieldFile)
	}
	if openPath != "" {
		child.Env = append(withoutVariable(child.Env, openReportVariable), openReportVariable+"="+openPath)
	}
	child.Env = withoutVariable(child.Env, projectSummaryVariable)
	if summaryPath != "" {
		child.Env = append(child.Env, projectSummaryVariable+"="+summaryPath)
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
		label:         label,
		output:        output.Bytes(),
		standardError: standardError.Bytes(),
		elapsed:       time.Since(started),
	}
	if openPath != "" {
		run.open = readOpenReport(openPath)
	}
	if summaryPath != "" {
		run.summary = readProjectSummary(summaryPath)
	}
	var exited *exec.ExitError
	switch {
	case runError == nil:
	case errors.As(runError, &exited) && exited.ExitCode() > 0:
		run.exitCode = exited.ExitCode()
	default:
		// Killed by a signal, or never started: a project that was not checked is a failure.
		run.exitCode = 1
		run.runError = runError
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

// runLabels names each project of the run uniquely, in the run's order: its label, and for a project whose
// directory an earlier one of the run shares, the child beside it, as `. (Formats)` beside the Go workspace at `.`
// (#vyjvz9k). The first keeps the label alone, so a directory holding one project names it as it always did, and a
// summary, a failure line and a run-cache record each name one project.
func runLabels(projects []discoveredProject) []string {
	labels := make([]string, len(projects))
	taken := map[string]bool{}
	for index, project := range projects {
		label := projectLabel(project)
		if taken[label] {
			label = fmt.Sprintf("%s (%s)", label, project.Kind.label())
		}
		taken[label] = true
		labels[index] = label
	}
	return labels
}

// sectionHeading opens a project's section.
func sectionHeading(project discoveredProject) string {
	return fmt.Sprintf("== %s (%s) ==", projectLabel(project), project.Kind.label())
}

// summaryLine closes the report: how many projects were checked, of which kinds, how many failed, what
// was found and not run or not checked, and what the walk did not enter.
func summaryLine(found discovery, runs []projectRun) string {
	counts := map[projectKind]int{}
	failed := 0
	checkedCount := 0
	for _, checked := range runs {
		checkedCount++
		counts[checked.project.Kind]++
		if checked.exitCode != 0 {
			failed++
		}
	}
	kinds := []string{}
	for _, kind := range []projectKind{
		projectKindTypeScriptProgram,
		projectKindSwiftPackage,
		projectKindGoModule,
		projectKindFolder,
	} {
		if counts[kind] > 0 {
			kinds = append(kinds, fmt.Sprintf("%d %s", counts[kind], kind.label()))
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
	if len(found.LeftOutLanguages) > 0 {
		notRunParts = append(notRunParts, "left out: "+strings.Join(found.LeftOutLanguages, ", "))
	}
	notRun := ""
	if len(notRunParts) > 0 {
		notRun = "; not run: " + strings.Join(notRunParts, ", ")
	}
	return fmt.Sprintf(
		"projects: %d checked (%s), %s%s; not searched for projects: %d directories .gitignore ignores, "+
			"%d dependency, build, cache or fixture directories, %d nested repositories",
		checkedCount,
		strings.Join(kinds, ", "),
		verdict,
		notRun,
		found.Ignored,
		found.NeverDescended,
		len(found.NestedRepositories)+found.Submodules,
	)
}
