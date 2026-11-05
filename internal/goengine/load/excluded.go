package load

import (
	"bytes"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Excluded is one workspace file the build constraints left out of every package, so no rule saw it.
type Excluded struct {
	File   string
	Reason string
}

// excludedFiles is the root packages' ignored files inside workspace, each once, sorted, with the target the
// constraints were read for. A package and its test variants list the same ignored files, and a pattern can
// name a package outside the workspace, whose files are not the workspace's to name.
func excludedFiles(workspace string, environment []string, roots []*packages.Package) ([]Excluded, error) {
	seen := map[string]bool{}
	var files []string
	for _, root := range roots {
		for _, file := range root.IgnoredFiles {
			if inside(workspace, file) && !seen[file] {
				seen[file] = true
				files = append(files, file)
			}
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	slices.Sort(files)
	target, targetError := goTarget(workspace, environment)
	if targetError != nil {
		return nil, targetError
	}
	excluded := make([]Excluded, 0, len(files))
	for _, file := range files {
		excluded = append(excluded, Excluded{File: file, Reason: "excluded by build constraints on " + target})
	}
	return excluded, nil
}

// goTarget is "GOOS/GOARCH" as go env reports them under the load's environment, which a `GOOS` given to the
// load or a go.env setting can change from the host's.
func goTarget(workspace string, environment []string) (string, error) {
	command := exec.Command("go", "env", "GOOS", "GOARCH")
	command.Dir = workspace
	command.Env = environment
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return "", fmt.Errorf("reading GOOS and GOARCH with go env in %s: %w: %s",
			workspace, runError, strings.TrimSpace(standardError.String()))
	}
	values := strings.Fields(string(output))
	if len(values) != 2 {
		return "", fmt.Errorf("reading GOOS and GOARCH with go env in %s: want two values, got %q", workspace, output)
	}
	return values[0] + "/" + values[1], nil
}
