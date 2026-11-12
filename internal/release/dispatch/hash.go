// Package dispatch resolves which cohere binary to run, rebuilding it when its inputs change.
//
// Rules are compiled into the binary, which is what makes them free to run. The cost of that choice
// is that adding a rule means rebuilding. This package pays that cost automatically and exactly
// once per change: the binary is named for a hash of everything that went into it, so a changed
// rule produces a different name, misses the cache, and rebuilds, while an unchanged tree hits and
// execs immediately.
//
// The correctness stake is the hash. A hash that misses an input serves a stale binary running old
// rules, and that looks precisely like success: the gate prints green, the rule file on disk says
// the rule is enforced, and neither is lying about what it knows. So the hash covers every input
// Go itself considers an input, discovered by asking the toolchain rather than by globbing paths.
package dispatch

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// hashLength is how much of the digest ends up in the filename. Eight bytes is 64 bits, which is
// far past collision risk for a cache holding tens of entries, and short enough to read.
const hashLength = 16

// CollectInputs asks the Go toolchain what the binary at packagePath is built from.
//
// The file list comes from `go list -deps` rather than from walking directories, because the
// toolchain's answer is the correct one by construction: it follows imports, honors build tags,
// and excludes tests. A directory walk would both miss inputs reached through an import we did not
// think of and include files that never compile in, and either error produces a hash that is wrong
// in a way nothing would catch.
//
// The tree's commit and state are read here too, from git, because the binary is stamped with them and
// reports them (developmentTreeState).
func CollectInputs(moduleDirectory string, packagePath string, buildFlags []string) (Inputs, error) {
	goVersion, goEnvironmentError := goEnvironment(moduleDirectory)
	if goEnvironmentError != nil {
		return Inputs{}, goEnvironmentError
	}

	commit, typeScriptCommitError := typeScriptCommit(moduleDirectory)
	if typeScriptCommitError != nil {
		return Inputs{}, typeScriptCommitError
	}

	sourceFiles, listSourceFilesError := listSourceFiles(moduleDirectory, packagePath)
	if listSourceFilesError != nil {
		return Inputs{}, listSourceFilesError
	}
	if len(sourceFiles) == 0 {
		// An empty input set hashes to a perfectly stable digest for a tree containing nothing,
		// and every later run would hit that cache entry. A sweep that measured nothing must never
		// be able to look like a sweep that found nothing.
		return Inputs{}, fmt.Errorf("go list reported no source files for %s, which cannot be right", packagePath)
	}

	selfCommit, sourceTreeModified := developmentTreeState(moduleDirectory, commit)
	return Inputs{
		GoVersion:          goVersion,
		TypeScriptGoCommit: commit,
		BuildFlags:         buildFlags,
		SourceFiles:        sourceFiles,
		SelfCommit:         selfCommit,
		SourceTreeModified: sourceTreeModified,
	}, nil
}

// goEnvironment identifies the toolchain: version, operating system, architecture.
func goEnvironment(moduleDirectory string) (string, error) {
	command := exec.Command("go", "env", "GOVERSION", "GOOS", "GOARCH")
	command.Dir = moduleDirectory

	output, outputError := command.Output()
	if outputError != nil {
		return "", fmt.Errorf("reading the Go toolchain version: %w", outputError)
	}

	return strings.Join(strings.Fields(string(output)), " "), nil
}

// typeScriptCommit reads the commit the vendored compiler's checkout is at, from its own files.
func typeScriptCommit(moduleDirectory string) (string, error) {
	commit, readHeadError := readHead(filepath.Join(moduleDirectory, "TypeScript"))
	if readHeadError != nil {
		return "", fmt.Errorf("reading the pinned TypeScript commit: %w", readHeadError)
	}
	return commit, nil
}

// listSourceFiles returns every non-stdlib file compiled into packagePath, transitively.
//
// Standard-library packages are excluded because they are already covered by the toolchain version:
// including them would hash thousands of files to learn something `GOVERSION` already said.
func listSourceFiles(moduleDirectory string, packagePath string) ([]string, error) {
	const template = `{{if not .Standard}}{{$directory := .Dir}}` +
		`{{range .GoFiles}}{{$directory}}/{{.}}{{"\n"}}{{end}}` +
		`{{range .EmbedFiles}}{{$directory}}/{{.}}{{"\n"}}{{end}}` +
		`{{end}}`

	command := exec.Command("go", "list", "-deps", "-f", template, packagePath)
	command.Dir = moduleDirectory

	output, outputError := command.Output()
	if outputError != nil {
		var exitError *exec.ExitError
		if errors.As(outputError, &exitError) {
			return nil, fmt.Errorf("listing build inputs for %s: %w\n%s", packagePath, outputError, exitError.Stderr)
		}
		return nil, fmt.Errorf("listing build inputs for %s: %w", packagePath, outputError)
	}

	files := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			files = append(files, trimmed)
		}
	}
	return files, nil
}
