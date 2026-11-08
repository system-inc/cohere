package staticcheck_code_test

import (
	"fmt"
	"go/build/constraint"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// constraintTargets are the targets the constraints fixture is loaded for, since the go tool loads only the files a
// target builds, and each file of the fixture builds on one of these at least.
var constraintTargets = [][]string{
	{"GOOS=linux", "GOARCH=amd64"},
	{"GOOS=windows", "GOARCH=amd64"},
	{"GOOS=darwin", "GOARCH=arm64"},
}

// Both sides read alike the build constraint of every file of the constraints fixture, loaded for each target, its
// `//go:build` line, its name, both or neither. The test fails unless every file was read, so a fixture file no target
// loads cannot pass by asking nothing of it.
func TestBuildConstraintsMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	loads := []fixtureLoad{}
	for _, target := range constraintTargets {
		each := pathFixture("constraints")
		each.environment = append(slices.Clone(each.environment), append(target, "CGO_ENABLED=0")...)
		loads = append(loads, each)
	}
	met := compareHelper(test, "comparebuildconstraints", loads, compareBuildConstraints)
	files, globError := filepath.Glob(filepath.Join("testdata", "src", "constraints", "*.go"))
	if globError != nil || len(files) == 0 {
		test.Fatalf("finding the constraints fixture's files: %v, %d found", globError, len(files))
	}
	labels := []string{}
	for _, file := range files {
		labels = append(labels, "BuildConstraints of "+filepath.Base(file))
	}
	requireMet(test, met, labels)
}

// compareBuildConstraints asks both sides the build constraint of every file of the pass's package, each answer
// spelled as the constraint prints and whether there is one, since two constraints built alike are two pointers.
func compareBuildConstraints(pass *analysis.Pass, context *rule.Context) []question {
	questions := []question{}
	for _, file := range pass.Files {
		position := pass.Fset.Position(file.Pos())
		questions = append(questions, ask("BuildConstraints", position,
			"BuildConstraints of "+filepath.Base(position.Filename),
			func() string { return spelledConstraint(code.BuildConstraints(pass, file)) },
			func() string { return spelledConstraint(staticcheck_code.BuildConstraints(context, file)) }))
	}
	return questions
}

// spelledConstraint is a constraint and whether there is one, as the comparison spells them.
func spelledConstraint(expression constraint.Expr, hasConstraint bool) string {
	if expression == nil {
		return fmt.Sprintf("none, %t", hasConstraint)
	}
	return fmt.Sprintf("%s, %t", expression, hasConstraint)
}
