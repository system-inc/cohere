package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/system-inc/cohere/internal/release/dispatch"
	"github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/internal/streams"
)

// Swift coverage is how a repository with several Swift packages gets each package's files in one report
// once (#sxs26s3). The Swift engine checks a root package's local path dependencies inside its directory as
// part of the root's run, so ahraos-presence's VRMKit, RigSolve and CaptureWire, each discovered as a project
// of its own too, were checked twice: VRMKit's 96 findings read as 192, and the footer counted its files twice.
//
// Before the runs start, each Swift project's engine is asked which packages a run at its root covers
// (`--covered-packages`), and a project another running project covers is not run: one the other's run
// checks, or one it lists as vendored and leaves unchecked. Coverage is the engine's answer, never nesting on
// disk, since a package below another that nothing depends on, like Presence's Tools/RigSimulator, is checked
// by no run but its own.
//
// A project whose query fails covers nothing, so everything it might have covered still runs: a package
// counted twice is visible, and a package hidden is not. One Swift project asks nothing, since there is
// nothing for it to cover.

// swiftCoverage is what asking the Swift projects' coverage decided, for the report.
type swiftCoverage struct {
	// Covered are the Swift projects not run because another project's run covers them, in discovery order.
	Covered []coveredProject

	// FailedQueries are the Swift projects whose coverage query failed, in discovery order. Each still runs,
	// and covers nothing.
	FailedQueries []failedCoverageQuery
}

// coveredProject is one Swift project another project's run covers.
type coveredProject struct {
	// Label is the covered project's, and CoveredBy the label of the project whose run covers it.
	Label     string
	CoveredBy string

	// Vendored is true when the covering run lists the package as vendored, its files not checked, rather
	// than checking them.
	Vendored bool
}

// failedCoverageQuery is one Swift project whose coverage query failed, and why.
type failedCoverageQuery struct {
	Label  string
	Reason string
}

// coveredPackagesRecord is the engine's answer to `--covered-packages`: the root, the packages a run there
// checks, the root among them, and the packages inside it the run lists as vendored. See swift/Contract.md.
type coveredPackagesRecord struct {
	Kind     string   `json:"kind"`
	Root     string   `json:"root"`
	Checked  []string `json:"checked"`
	Vendored []string `json:"vendored"`
}

// resolveSwiftCoverage asks each Swift project which packages its run covers, when there is more than one,
// and drops the projects a running project covers. found.Projects is rewritten to the projects that run.
//
// Mutates: found the caller runs the projects found holds, so its list becomes the ones that run here
func resolveSwiftCoverage(found *discovery) swiftCoverage {
	decided := swiftCoverage{}
	var swiftProjects []discoveredProject
	for _, project := range found.Projects {
		if project.Engine == engineSwift {
			swiftProjects = append(swiftProjects, project)
		}
	}
	if len(swiftProjects) < 2 {
		return decided
	}

	answers := querySwiftCoverage(found.Root, swiftProjects)
	for _, project := range swiftProjects {
		if answer := answers[project.Directory]; answer.err != nil {
			decided.FailedQueries = append(decided.FailedQueries, failedCoverageQuery{Label: projectLabel(project), Reason: answer.err.Error()})
		}
	}

	// A covering project is always above the one it covers, and discovery puts parents first, so by the time
	// a project is reached every project that could cover it has been decided. Only a project that runs
	// covers anything: one skipped itself is checked by a run above it, whose own answer decides.
	running := found.Projects[:0:0]
	var coverers []coveredPackages
	for _, project := range found.Projects {
		if project.Engine != engineSwift {
			running = append(running, project)
			continue
		}
		directory := resolvedPath(filepath.Join(found.Root, filepath.FromSlash(project.Directory)))
		if covered, isCovered := coveredByAny(coverers, directory, projectLabel(project)); isCovered {
			decided.Covered = append(decided.Covered, covered)
			continue
		}
		running = append(running, project)
		if answer := answers[project.Directory]; answer.err == nil {
			coverers = append(coverers, answer.covered)
		}
	}
	found.Projects = running
	return decided
}

// coveredPackages is one running project's answer, its paths resolved for comparing.
type coveredPackages struct {
	label     string
	directory string
	checked   map[string]bool
	vendored  map[string]bool
}

// coveredByAny is which running project, if any, covers the project at directory, resolved.
func coveredByAny(coverers []coveredPackages, directory string, label string) (coveredProject, bool) {
	for _, coverer := range coverers {
		// A run checks its own root, and that is no reason to skip anything.
		if directory == coverer.directory {
			continue
		}
		if coverer.checked[directory] {
			return coveredProject{Label: label, CoveredBy: coverer.label}, true
		}
		if coverer.vendored[directory] {
			return coveredProject{Label: label, CoveredBy: coverer.label, Vendored: true}, true
		}
	}
	return coveredProject{}, false
}

// coverageAnswer is one project's query, answered or failed.
type coverageAnswer struct {
	covered coveredPackages
	err     error
}

// querySwiftCoverage asks every Swift project's coverage, several at once, by the project's directory. An
// engine that cannot be resolved fails every query with the same reason, which the runs would meet too.
func querySwiftCoverage(root string, projects []discoveredProject) map[string]coverageAnswer {
	answers := map[string]coverageAnswer{}
	directories := make([]string, len(projects))
	for index, project := range projects {
		directories[index] = filepath.Join(root, filepath.FromSlash(project.Directory))
	}

	// Resolved the way a Swift run resolves it, so the answer comes from the engine that will check. A
	// development cohere may build it here, a step its progress shows and clears before the report. The progress
	// writes to the terminal itself, so what this run said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	dispatch.Report.SetVerbose(activeOutput.Mode == outputVerbose)
	binaryPath, _, resolveSwiftEngineBinaryError := resolveSwiftEngineBinary(projectLocation{Root: directories[0], Engine: engineSwift}, release.Current())
	dispatch.Report.Clear()
	if resolveSwiftEngineBinaryError != nil {
		for _, project := range projects {
			answers[project.Directory] = coverageAnswer{err: fmt.Errorf("resolving the Swift engine: %w", resolveSwiftEngineBinaryError)}
		}
		return answers
	}

	var lock sync.Mutex
	var group sync.WaitGroup
	querying := make(chan struct{}, projectRunsAtOnce())
	for index, project := range projects {
		group.Add(1)
		go func() {
			defer group.Done()
			querying <- struct{}{}
			record, queryCoveredPackagesError := queryCoveredPackages(binaryPath, directories[index])
			<-querying
			answer := coverageAnswer{err: queryCoveredPackagesError}
			if queryCoveredPackagesError == nil {
				answer.covered = coveredPackages{
					label:     projectLabel(project),
					directory: resolvedPath(directories[index]),
					checked:   resolvedPaths(record.Checked),
					vendored:  resolvedPaths(record.Vendored),
				}
			}
			lock.Lock()
			answers[project.Directory] = answer
			lock.Unlock()
		}()
	}
	group.Wait()
	return answers
}

// queryCoveredPackages runs the engine's `--covered-packages` at one package's root, which reads manifests
// and builds nothing, and returns its record. An engine that fails, or ends without the record, is an error.
//
// It stays in this process's group, unlike a checking run's engine, so an interrupt from the terminal reaches
// it and whatever SwiftPM command it started, before any project's run has begun.
func queryCoveredPackages(binaryPath string, directory string) (coveredPackagesRecord, error) {
	command := exec.Command(binaryPath, "--contract", fmt.Sprint(engineContractVersion), "--root", directory, "--covered-packages")
	command.Dir = directory
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	command.Stdout = &standardOutput
	command.Stderr = &standardError
	runError := command.Run()
	if runError != nil {
		reason := fmt.Sprintf("could not be started (%v)", runError)
		if command.ProcessState != nil {
			_, reason = describeExit(command.ProcessState, runError)
		}
		if said := lastLine(standardError.String()); said != "" {
			reason += ": " + said
		}
		return coveredPackagesRecord{}, fmt.Errorf("the engine %s", reason)
	}

	var found []coveredPackagesRecord
	scanner := bufio.NewScanner(&standardOutput)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		var record coveredPackagesRecord
		if decodeError := json.Unmarshal(scanner.Bytes(), &record); decodeError != nil {
			return coveredPackagesRecord{}, fmt.Errorf("the engine printed a line that is not a record: %q", lastLine(scanner.Text()))
		}
		if record.Kind == "coveredPackages" {
			found = append(found, record)
		}
	}
	if err := scanner.Err(); err != nil {
		return coveredPackagesRecord{}, fmt.Errorf("reading the engine's answer: %w", err)
	}
	switch len(found) {
	case 0:
		return coveredPackagesRecord{}, fmt.Errorf("the engine exited 0 and printed no coveredPackages record")
	case 1:
	default:
		return coveredPackagesRecord{}, fmt.Errorf("the engine printed %d coveredPackages records, not one", len(found))
	}
	return found[0], nil
}

// lastLine is the last line of what a process printed that holds anything, trimmed.
func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// resolvedPath is a path with its symbolic links resolved, the form both sides of a coverage comparison are
// put in: the engine standardizes its paths as Foundation spells them, which on macOS writes `/private/tmp`
// as `/tmp`, and this process may have been handed either. A path that cannot be resolved is compared as it
// is, cleaned, and simply matches nothing it should not.
func resolvedPath(name string) string {
	if resolved, resolveSymlinksError := filepath.EvalSymlinks(name); resolveSymlinksError == nil {
		return resolved
	}
	return filepath.Clean(name)
}

// resolvedPaths is a set of paths, each resolved.
func resolvedPaths(names []string) map[string]bool {
	resolved := make(map[string]bool, len(names))
	for _, name := range names {
		resolved[resolvedPath(name)] = true
	}
	return resolved
}
