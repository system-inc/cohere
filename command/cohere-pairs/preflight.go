package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// preflight is what the checks before any measurement established, which everything after them reads.
type preflight struct {
	// goBinary is the go every go command the pair starts runs, and goEnvironment the environment it runs in: held
	// off the network when measuring, fetching under --prepare.
	goBinary          string
	goEnvironment     []string
	goVersion         string
	goOperatingSystem string
	goArchitecture    string
	host              string
	cores             int

	// repositories are the private bare copies every build and corpus reads (copySources), never the sources.
	repositories sourceRepositories

	branchCommit           string
	branchTypeScriptCommit string
	baseCommit             string
	baseTypeScriptCommit   string

	corpora []corpus
	output  string
}

// runPreflight checks what a measurement rests on before anything is built or run, saying each check as it passes,
// and returns the first that fails. It writes only the private copies of the sources into --out: every commit is
// then read from them, the sides', the compiler's and the corpora's pins alike.
func runPreflight(asked commandLine, say progress) (preflight, error) {
	checked := preflight{output: asked.output, cores: runtime.NumCPU()}
	goBinary, lookPathError := exec.LookPath("go")
	if lookPathError != nil {
		return preflight{}, fmt.Errorf("finding go: %w", lookPathError)
	}
	checked.goBinary = goBinary
	checked.goEnvironment = offlineGoEnvironment(environmentWithout(os.Environ(), "GOFLAGS"))
	if asked.prepare {
		checked.goEnvironment = preparingGoEnvironment(os.Environ())
	}

	goVersion, goVersionError := runGo(checked.goBinary, "", checked.goEnvironment, "version")
	if goVersionError != nil {
		return preflight{}, goVersionError
	}
	checked.goVersion = goVersion
	say.say("%s\n", goVersion)
	platform, goEnvironmentError := runGo(checked.goBinary, "", checked.goEnvironment, "env", "GOOS", "GOARCH")
	if goEnvironmentError != nil {
		return preflight{}, goEnvironmentError
	}
	platformFields := strings.Fields(platform)
	if len(platformFields) != 2 {
		return preflight{}, fmt.Errorf("go env GOOS GOARCH printed %q, not two words", platform)
	}
	checked.goOperatingSystem, checked.goArchitecture = platformFields[0], platformFields[1]
	say.say("go env GOOS GOARCH: %s/%s\n", checked.goOperatingSystem, checked.goArchitecture)
	host, hostnameError := os.Hostname()
	if hostnameError != nil {
		return preflight{}, fmt.Errorf("reading the host's name: %w", hostnameError)
	}
	checked.host = host

	if checkOutputError := checkOutput(asked.output); checkOutputError != nil {
		return preflight{}, checkOutputError
	}
	say.say("out: %s is empty or new\n", asked.output)

	workingCheckout := currentCheckout()
	sources, resolveSourcesError := resolveSources(asked, workingCheckout)
	if resolveSourcesError != nil {
		return preflight{}, resolveSourcesError
	}
	branch, branchError := branchToMeasure(asked, workingCheckout)
	if branchError != nil {
		return preflight{}, branchError
	}
	say.say("branch: %s\n", branch)

	if createOutputError := os.MkdirAll(asked.output, 0o755); createOutputError != nil {
		return preflight{}, fmt.Errorf("creating %s: %w", asked.output, createOutputError)
	}
	repositories, copySourcesError := copySources(sources, asked.output, say)
	if copySourcesError != nil {
		return preflight{}, copySourcesError
	}
	checked.repositories = repositories

	checked, sidesError := checkSides(checked, asked.base, branch, say)
	if sidesError != nil {
		return preflight{}, sidesError
	}
	for _, pin := range []struct {
		repository string
		commit     string
	}{{repositories.adamic, adamicCommit}, {repositories.loom, loomCommit}} {
		if pinExistsError := commitExists(pin.repository, pin.commit); pinExistsError != nil {
			return preflight{}, fmt.Errorf("a corpus pin: %w", pinExistsError)
		}
	}
	corpora, planCorporaError := planCorpora(checked)
	if planCorporaError != nil {
		return preflight{}, planCorporaError
	}
	for _, plannedCorpus := range corpora {
		for _, layer := range plannedCorpus.layers {
			if layerExistsError := commitExists(layer.repository, layer.commit); layerExistsError != nil {
				return preflight{}, fmt.Errorf("corpus %s: %w", plannedCorpus.name, layerExistsError)
			}
			say.say("corpus %s: %s at %s, from %s\n", plannedCorpus.name, describeWithin(layer.within), layer.commit,
				layer.repository)
		}
	}
	checked.corpora = corpora
	return checked, nil
}

// checkSides resolves both sides in the private copy of cohere, reads each one's compiler pin from its tree, and
// checks the copy of the compiler holds it, returning checked with all four commits filled in.
func checkSides(checked preflight, base string, branch string, say progress) (preflight, error) {
	for _, side := range []struct {
		name             string
		asked            string
		commit           *string
		typeScriptCommit *string
	}{
		{sideBase, base, &checked.baseCommit, &checked.baseTypeScriptCommit},
		{sideBranch, branch, &checked.branchCommit, &checked.branchTypeScriptCommit},
	} {
		commit, resolveError := gitOutput(checked.repositories.cohere, "rev-parse", "--verify", "--end-of-options",
			side.asked+"^{commit}")
		if resolveError != nil {
			return preflight{}, fmt.Errorf("--%s %s does not resolve to a commit: %w", side.name, side.asked,
				resolveError)
		}
		typeScriptCommit, gitlinkError := gitlinkOf(checked.repositories.cohere, commit, "TypeScript")
		if gitlinkError != nil {
			return preflight{}, gitlinkError
		}
		if typeScriptExistsError := commitExists(
			checked.repositories.typeScript,
			typeScriptCommit,
		); typeScriptExistsError != nil {
			return preflight{}, fmt.Errorf("%s's compiler: %w", side.name, typeScriptExistsError)
		}
		*side.commit, *side.typeScriptCommit = commit, typeScriptCommit
		say.say("%s: %s, its TypeScript at %s\n", side.name, commit, typeScriptCommit)
	}
	return checked, nil
}

// currentCheckout is the root of the git checkout the command runs in, or empty when it runs in none: on a rented
// machine the tree came by git archive and has no `.git` anywhere.
func currentCheckout() string {
	workingDirectory, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		return ""
	}
	topLevel, topLevelError := gitOutput(workingDirectory, "rev-parse", "--show-toplevel")
	if topLevelError != nil {
		return ""
	}
	return topLevel
}

// branchToMeasure is the commit measured: --branch when it was given, and otherwise the `HEAD` of the checkout the
// command runs in, whose tracked files must then be clean, since one who measures `HEAD` means what is on disk. With
// no checkout there is no `HEAD`, so --branch is required. The build reads the private copy either way, never the
// tree the command runs in.
func branchToMeasure(asked commandLine, workingCheckout string) (string, error) {
	if asked.branch != "" {
		return asked.branch, nil
	}
	if workingCheckout == "" {
		return "", errors.New("--branch is required: the command runs in no git checkout, so there is no HEAD to " +
			"measure")
	}
	head, headError := gitOutput(workingCheckout, "rev-parse", "--verify", "HEAD^{commit}")
	if headError != nil {
		return "", headError
	}
	// The submodule is left out: what is built is the commit, and a compiler with files of its own changed is not
	// what the branch's binary is built from either way.
	changed, statusError := gitOutput(workingCheckout, "status", "--porcelain", "--untracked-files=no",
		"--ignore-submodules=all")
	if statusError != nil {
		return "", statusError
	}
	if changed != "" {
		return "", fmt.Errorf("%s's tracked files have changes, so its HEAD is not what is on disk; commit them or "+
			"name --branch:\n%s", workingCheckout, changed)
	}
	return head, nil
}

// checkOutput refuses an output directory that holds anything, so nothing a pair writes lands beside what it did not.
func checkOutput(output string) error {
	entries, readDirectoryError := os.ReadDir(output)
	if readDirectoryError != nil && !errors.Is(readDirectoryError, fs.ErrNotExist) {
		return fmt.Errorf("reading --out %s: %w", output, readDirectoryError)
	}
	if len(entries) > 0 {
		return fmt.Errorf("--out %s holds %d entries, and the command writes only into a new or empty directory",
			output, len(entries))
	}
	return nil
}

// commandOutput runs a command and returns what it printed, trimmed, carrying its stderr into the error.
func commandOutput(name string, arguments ...string) (string, error) {
	output, runError := exec.Command(name, arguments...).Output()
	if runError != nil {
		var exitError *exec.ExitError
		if errors.As(runError, &exitError) {
			return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(arguments, " "), runError,
				strings.TrimSpace(string(exitError.Stderr)))
		}
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(arguments, " "), runError)
	}
	return strings.TrimSpace(string(output)), nil
}

// sameDirectory reports whether two paths name one directory once every link in them is resolved, as macOS's /tmp
// and git's answers for it differ.
func sameDirectory(first string, second string) bool {
	firstResolved, firstError := filepath.EvalSymlinks(first)
	secondResolved, secondError := filepath.EvalSymlinks(second)
	return firstError == nil && secondError == nil && firstResolved == secondResolved
}

// describeWithin names where in a corpus a layer goes, for the preflight's lines.
func describeWithin(within string) string {
	if within == "" {
		return "the root"
	}
	return within + "/"
}
