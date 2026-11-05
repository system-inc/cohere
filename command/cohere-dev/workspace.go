package main

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/system-inc/cohere/internal/goengine/project"
)

// The landing gate and the whole-module test run cover every module the repository builds, not only the
// root (#ejcnkja). `go test ./...` from the root reaches the root module's packages and stops at any
// directory with a go.mod of its own, so the TypeScript shims and static_single_assignment, each a module
// so another repository can import it, were never vetted or tested by any gate.
//
// The list is go.work's use directives, read by internal/goengine/project, which the Go engine reads too.
// A module inside a submodule is not gated: its tests are its upstream's. Everything gated runs in one go
// invocation, so it shares the token's -p budget and Go's test cache, and the gate says what it covered.
//
// A module go.work doesn't name is refused rather than left out: a go.mod anywhere in the tree outside a
// submodule must be one of go.work's uses, since a module missing from that list is a module no gate tests,
// which is how the shims went untested.

// readGatedWorkspace reads the modules a run in root gates, refusing a module go.work doesn't use. Without a
// go.work it is the root module alone, the gate as it was.
func readGatedWorkspace(root string) (project.Workspace, error) {
	covered, readError := project.ReadWorkspace(root)
	if readError != nil {
		return project.Workspace{}, readError
	}
	if refusal := covered.RefuseUnlistedModules(); refusal != nil {
		return project.Workspace{}, refusal
	}
	return covered, nil
}

// reportWorkspace says what a run covers, so a module a gate missed shows as missing from its output.
func reportWorkspace(covered project.Workspace, output *bufio.Writer) {
	if !covered.FromWorkspace {
		return
	}
	directories := make([]string, 0, len(covered.Modules))
	for _, module := range covered.Modules {
		directories = append(directories, module.Directory)
	}
	fmt.Fprintf(output, "cohere-dev: gating %d modules from go.work: %s\n",
		len(directories), strings.Join(directories, ", "))
	if len(covered.InSubmodule) > 0 {
		fmt.Fprintf(output, "cohere-dev: not gated, inside a submodule: %s\n", strings.Join(covered.InSubmodule, ", "))
	}
}
