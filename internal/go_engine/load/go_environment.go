package load

import (
	"go/build"
	"path/filepath"
	"slices"
)

// GoEnvironment is go env's answer for a workspace under an environment, as sourceContext reads it: the whole
// environment every go command of a load runs with, the build target, and the go environment the inputs record names.
// The project's census asks for it (ExcludedByConstraints), and a load handed it through Caching asks go env nothing,
// so a run asks once where it asked twice (#f53yra8). Nothing a run writes moves it: go env reads the environment,
// the go env files and the go.mod and go.work go finds, and a run rewrites only Go files.
type GoEnvironment struct {
	workspace       string
	environment     []string
	fullEnvironment []string
	buildContext    *build.Context
	reported        reportedEnvironment
}

// readGoEnvironment asks go env in directory under the current environment plus environment.
func readGoEnvironment(directory string, environment []string, held Held) (*GoEnvironment, error) {
	workspace, absoluteError := filepath.Abs(directory)
	if absoluteError != nil {
		return nil, absoluteError
	}
	fullEnvironment, buildContext, reported, contextError := sourceContext(workspace, environment, held)
	if contextError != nil {
		return nil, contextError
	}
	return &GoEnvironment{
		workspace:       workspace,
		environment:     slices.Clone(environment),
		fullEnvironment: fullEnvironment,
		buildContext:    buildContext,
		reported:        reported,
	}, nil
}

// answers reports whether the answer is for workspace, absolute, under environment, so a load asking for another
// never reads it.
func (goEnvironment *GoEnvironment) answers(workspace string, environment []string) bool {
	return goEnvironment != nil && goEnvironment.workspace == workspace &&
		slices.Equal(goEnvironment.environment, environment)
}

// contextFor is the build target as a build context that opens a file held holds from its held bytes, and any other
// on disk, whichever bytes the asker held.
func (goEnvironment *GoEnvironment) contextFor(held Held) *build.Context {
	buildContext := *goEnvironment.buildContext
	buildContext.OpenFile = held.openFile()
	return &buildContext
}
