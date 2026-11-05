package load

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Excluded is one workspace file the build constraints left out of every package, so no rule saw it.
type Excluded struct {
	File   string
	Reason string
}

// ExcludedByConstraints is each of files, absolute paths, that the build constraints leave out when go runs
// in directory with the current environment plus environment, sorted, with the target they were read for. It
// reads each file's name and build lines as go does, so it names a file in a directory whose every file is
// left out, which go list matches as no package at all and so never reports.
func ExcludedByConstraints(directory string, environment []string, files []string) ([]Excluded, error) {
	workspace, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		return nil, fmt.Errorf("resolving %s: %w", directory, absoluteError)
	}
	if len(files) == 0 {
		return nil, nil
	}
	buildContext, contextError := constraintContext(workspace, append(os.Environ(), environment...))
	if contextError != nil {
		return nil, contextError
	}
	excluded, _, matchError := matchConstraints(buildContext, files)
	return excluded, matchError
}

// excludedAndUnseen is what the load left out of the workspace. The root packages' ignored files inside
// workspace are excluded as go list reported them; a pattern can name a package outside the workspace, whose
// files are not the workspace's to name. Then each census file no package in the graph holds is read against
// the constraints: one they leave out is excluded too, and one they would build is unseen, since nothing
// type-checked it. Each file comes back once, sorted, and go env runs only when there is a file to read for.
func excludedAndUnseen(
	workspace string,
	environment []string,
	roots []*packages.Package,
	census []string,
) ([]Excluded, []string, error) {
	held := map[string]bool{}
	packages.Visit(roots, nil, func(visited *packages.Package) {
		for _, file := range append(append([]string{}, visited.GoFiles...), visited.IgnoredFiles...) {
			held[file] = true
		}
	})
	seen := map[string]bool{}
	var ignored []string
	for _, root := range roots {
		for _, file := range root.IgnoredFiles {
			if inside(workspace, file) && !seen[file] {
				seen[file] = true
				ignored = append(ignored, file)
			}
		}
	}
	var unheld []string
	for _, file := range census {
		if !held[file] && !seen[file] {
			seen[file] = true
			unheld = append(unheld, file)
		}
	}
	if len(ignored)+len(unheld) == 0 {
		return nil, nil, nil
	}

	buildContext, contextError := constraintContext(workspace, environment)
	if contextError != nil {
		return nil, nil, contextError
	}
	excluded, unseen, matchError := matchConstraints(buildContext, unheld)
	if matchError != nil {
		return nil, nil, matchError
	}
	for _, file := range ignored {
		excluded = append(excluded, Excluded{File: file, Reason: reasonFor(buildContext)})
	}
	slices.SortFunc(excluded, func(left, right Excluded) int { return strings.Compare(left.File, right.File) })
	return excluded, unseen, nil
}

// matchConstraints splits files into those the context's constraints leave out, as Excluded, and those they
// would build, each list sorted.
func matchConstraints(buildContext *build.Context, files []string) ([]Excluded, []string, error) {
	var excluded []Excluded
	var built []string
	for _, file := range files {
		directory, name := filepath.Split(file)
		matched, matchError := buildContext.MatchFile(directory, name)
		if matchError != nil {
			return nil, nil, fmt.Errorf("reading the build constraints of %s: %w", file, matchError)
		}
		if matched {
			built = append(built, file)
		} else {
			excluded = append(excluded, Excluded{File: file, Reason: reasonFor(buildContext)})
		}
	}
	slices.SortFunc(excluded, func(left, right Excluded) int { return strings.Compare(left.File, right.File) })
	slices.Sort(built)
	return excluded, built, nil
}

// reasonFor is why a file the context's constraints leave out was left out.
func reasonFor(buildContext *build.Context) string {
	return "excluded by build constraints on " + buildContext.GOOS + "/" + buildContext.GOARCH
}

// constraintContext is the build context go uses in workspace under environment, the whole environment, as go
// env reports it: the target, whether cgo is on, and the `-tags` of `GOFLAGS`, each of which a value given to
// the load or a go.env setting can change from the host's.
func constraintContext(workspace string, environment []string) (*build.Context, error) {
	command := exec.Command("go", "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS")
	command.Dir = workspace
	command.Env = environment
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, runError := command.Output()
	if runError != nil {
		return nil, fmt.Errorf("reading the build target with go env in %s: %w: %s",
			workspace, runError, strings.TrimSpace(standardError.String()))
	}
	var values struct {
		GOOS       string
		GOARCH     string
		CgoEnabled string `json:"CGO_ENABLED"`
		GOFLAGS    string
	}
	if decodeError := json.Unmarshal(output, &values); decodeError != nil {
		return nil, fmt.Errorf("reading the build target with go env in %s: %w", workspace, decodeError)
	}
	if values.GOOS == "" || values.GOARCH == "" {
		return nil, fmt.Errorf("reading the build target with go env in %s: no GOOS or GOARCH in %q", workspace, output)
	}
	buildContext := build.Default
	buildContext.GOOS = values.GOOS
	buildContext.GOARCH = values.GOARCH
	buildContext.CgoEnabled = values.CgoEnabled == "1"
	buildContext.BuildTags = tagsOf(values.GOFLAGS)
	return &buildContext, nil
}

// tagsOf is the build tags a `GOFLAGS` value sets: the last `-tags=` among its flags wins, as on a command line,
// and its tags are separated by commas.
func tagsOf(goFlags string) []string {
	var tags []string
	for _, flag := range strings.Fields(goFlags) {
		value, found := strings.CutPrefix(strings.TrimLeft(flag, "-"), "tags=")
		if !found {
			continue
		}
		tags = nil
		for _, tag := range strings.Split(value, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	return tags
}
