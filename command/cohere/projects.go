package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// projectEngineVariable names the engine a child run checks its --directory with. A directory holding both
// a tsconfig.json and a Package.swift is two projects, and --directory alone reads it as TypeScript's, so
// the run that checks its Swift says which engine it is.
const projectEngineVariable = "COHERE_PROJECT_ENGINE"

// projectLabelVariable names the project a child run checks, by its path under the root, so the run's own
// footer can say which project it is about (#ytqqv8v).
const projectLabelVariable = "COHERE_PROJECT_LABEL"

// discoveryApplies reports whether a run discovers its projects. Anything that names one project, a
// directory, a tsconfig, a lint config or a path, narrows the run to it as it always did, and so do the
// listings, the explanations and the editor's save, which answer about one project.
func discoveryApplies(given map[string]bool, arguments []string) bool {
	if len(arguments) > 0 {
		return false
	}
	for _, name := range []string{
		"directory", "tsconfig", "lint-config", "rules", "rules-enabled", "print-config",
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
func checkDiscoveredProjects(location projectLocation, locateError error, workingDirectory string) (int, bool, error) {
	root := discoveryRoot(location, locateError, workingDirectory)
	patterns, err := rootIgnorePatterns(root)
	if err != nil {
		return 0, false, err
	}
	found, err := discoverProjects(root, patterns)
	if err != nil {
		return 0, false, err
	}
	if len(found.Projects) == 0 {
		// Nothing to check is a failure, never a green over nothing.
		return 0, false, fmt.Errorf("no %s or %s in %s, any directory above it, or any directory below it that the repository keeps, "+
			"so there is no project here to check (not searched: %d directories .gitignore ignores, %d dependency, build, cache "+
			"or fixture directories, %d nested repositories): run from inside one, or name it with -tsconfig or -directory",
			projectMarker, swiftProjectMarker, root, found.Ignored, found.NeverDescended,
			len(found.NestedRepositories)+found.Submodules)
	}
	if found.isTheLocatedProject(location, locateError) {
		for _, note := range found.notes() {
			fmt.Fprintf(os.Stderr, "cohere: %s\n", note)
		}
		return 0, false, nil
	}
	return runProjects(found, os.Args[1:], os.Stdout), true, nil
}

// isTheLocatedProject reports whether discovery found exactly the one project the walk up located, which
// a run then checks in this process, the way it always did.
func (found discovery) isTheLocatedProject(location projectLocation, locateError error) bool {
	if locateError != nil || len(found.Projects) != 1 {
		return false
	}
	project := found.Projects[0]
	return project.Engine == location.Engine &&
		filepath.Clean(filepath.Join(found.Root, project.Directory)) == filepath.Clean(location.Root)
}

// notes are the lines a run carries about markers discovery found and did not run, which a reader could have
// expected checked: those the root's ignorePatterns refuse. Ignored and dependency directories are not
// named, since skipping them is what everyone expects, and neither is a nested repository: one inside a
// program, such as Structure or Base, is checked as part of it, its drift reported read-only, as it always
// was. They are counted on the summary line.
func (found discovery) notes() []string {
	var lines []string
	for _, marker := range found.Refused {
		lines = append(lines, fmt.Sprintf("%s not checked: the root's ignorePatterns match it", marker))
	}
	return lines
}

// projectRun is one project's run, as its child reported it.
type projectRun struct {
	project  discoveredProject
	output   []byte
	exitCode int
	err      error
	elapsed  time.Duration
}

// runProjects checks every discovered project, each in its own run of this binary, all at once, and
// reports them as one: a section per project in the order discovery found them, then one summary line.
// It exits with the worst exit code among them, so the run is green only when every project is, and a
// project whose run crashed or could not start fails it.
//
// Each project is a child process rather than a goroutine, because a run is a whole program: its own
// type graph, its own settings and cache table, and its own memory ceiling. A child is exactly the run
// `cohere --directory <project>` makes alone, so a project's section reads the same as its own run's, and
// its exit code is the one that run alone would give.
func runProjects(found discovery, arguments []string, out io.Writer) int {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(out, "cohere: finding this binary to run each project: %v\n", err)
		return 1
	}

	runs := make([]projectRun, len(found.Projects))
	var group sync.WaitGroup
	for index, project := range found.Projects {
		group.Add(1)
		go func() {
			defer group.Done()
			runs[index] = runProject(executable, found.Root, project, arguments)
		}()
	}
	group.Wait()

	worst := 0
	for _, checked := range runs {
		fmt.Fprintf(out, "%s\n", sectionHeading(checked.project))
		out.Write(checked.output)
		if len(checked.output) > 0 && checked.output[len(checked.output)-1] != '\n' {
			fmt.Fprintln(out)
		}
		if checked.err != nil {
			fmt.Fprintf(out, "cohere: the %s run did not finish: %v\n", checked.project.Engine, checked.err)
		}
		fmt.Fprintln(out)
		if checked.exitCode > worst {
			worst = checked.exitCode
		}
	}

	fmt.Fprintf(out, "projects under %s:\n", found.Root)
	for _, checked := range runs {
		verdict := "green"
		if checked.exitCode != 0 {
			verdict = fmt.Sprintf("failed, exit %d", checked.exitCode)
		}
		fmt.Fprintf(out, "  %s (%s): %s in %s\n", projectLabel(checked.project), checked.project.Engine, verdict, round(checked.elapsed))
	}
	for _, note := range found.notes() {
		fmt.Fprintf(out, "  %s\n", note)
	}
	fmt.Fprintln(out, summaryLine(found, runs))
	return worst
}

// runProject runs one project's check as a child and keeps everything it printed.
func runProject(executable string, root string, project discoveredProject, arguments []string) projectRun {
	directory := filepath.Join(root, filepath.FromSlash(project.Directory))
	child := exec.Command(executable, append([]string{"--directory", directory}, arguments...)...)
	// Started in the project, so its report does not add that it checked somewhere other than where it
	// started: the section's heading already says which project it is.
	child.Dir = directory
	// The verdict descriptor is this run's: a child's verdict reaches this process as its exit code, and
	// only the merged one may answer the caller.
	child.Env = append(withoutVariable(os.Environ(), VerdictVariable),
		projectEngineVariable+"="+string(project.Engine), projectLabelVariable+"="+projectLabel(project))
	var output bytes.Buffer
	child.Stdout = &output
	child.Stderr = &output

	started := time.Now()
	err := child.Run()
	run := projectRun{project: project, output: output.Bytes(), elapsed: time.Since(started)}
	var exited *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exited) && exited.ExitCode() > 0:
		run.exitCode = exited.ExitCode()
	default:
		// Killed by a signal, or never started: a project that was not checked is a failure.
		run.exitCode = 1
		run.err = err
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

// projectLabel names a project by its directory, the root by ".".
func projectLabel(project discoveredProject) string {
	return project.Directory
}

// sectionHeading opens a project's section.
func sectionHeading(project discoveredProject) string {
	return fmt.Sprintf("== %s (%s) ==", projectLabel(project), project.Engine)
}

// summaryLine closes the report: how many projects were checked, of which engines, how many failed, and
// what the walk did not enter.
func summaryLine(found discovery, runs []projectRun) string {
	counts := map[projectEngine]int{}
	failed := 0
	for _, checked := range runs {
		counts[checked.project.Engine]++
		if checked.exitCode != 0 {
			failed++
		}
	}
	engines := []string{}
	for _, engine := range []projectEngine{engineTypeScript, engineSwift} {
		if counts[engine] > 0 {
			engines = append(engines, fmt.Sprintf("%d %s", counts[engine], engine))
		}
	}
	verdict := "every one green"
	if failed > 0 {
		verdict = fmt.Sprintf("%d failed", failed)
	}
	return fmt.Sprintf("projects: %d checked (%s), %s; not searched for projects: %d directories .gitignore ignores, "+
		"%d dependency, build, cache or fixture directories, %d nested repositories",
		len(runs), strings.Join(engines, ", "), verdict, found.Ignored, found.NeverDescended,
		len(found.NestedRepositories)+found.Submodules)
}
