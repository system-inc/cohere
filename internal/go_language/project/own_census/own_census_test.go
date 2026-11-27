// Package own_census holds the one check of project that reads cohere's whole tree on purpose: the census of
// cohere's own worktree, and the builds it needs. It has no code of its own. A census opens every directory of the
// module, and go's test cache keys a result on each directory's listing, so an edit anywhere in cohere, a document
// project never reads included, reruns it. It lives apart from project's own tests, which read only the fixtures
// they write, so that such an edit reruns this check alone and project's tests replay from the cache (#6m8d98z).
package own_census

import (
	"go/build"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/project"
	"github.com/system-inc/cohere/internal/testing/go_root"
)

// cohere's own census at the worktree's root, under the host go env reads and the toolchain's own targets: the
// builds are printed, the host's first and every one on its architecture, and every file is compiled by one or
// unbuilt with a reason. The host is this test binary's own target, which go env reads when the environment names
// none and go test runs the binary it built, so a wrong target read for it fails here rather than shaping only the
// builds printed (#z8m9dpg). Its cgo has no reading but go_root's, whose own tests hold it to cmd/go's rule.
func TestBuildsForCoheresOwnCensus(test *testing.T) {
	test.Parallel()
	root, rootError := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if rootError != nil {
		test.Fatal(rootError)
	}
	covered, readError := project.ReadWorkspace(root)
	if readError != nil {
		test.Fatal(readError)
	}
	census, censusError := project.TakeCensus(covered)
	if censusError != nil {
		test.Fatal(censusError)
	}
	ports, portsError := project.ReadPorts(root, nil)
	if portsError != nil {
		test.Fatal(portsError)
	}
	host := hostAt(test)
	if host.OperatingSystem != runtime.GOOS || host.Architecture != runtime.GOARCH {
		test.Fatalf("the host read is %s, and this test binary runs as %s/%s, go env's own target", host,
			runtime.GOOS, runtime.GOARCH)
	}
	builds, unbuilt, buildsError := project.BuildsFor(census, host, ports, nil, os.ReadFile)
	if buildsError != nil {
		test.Fatal(buildsError)
	}
	if len(builds) == 0 || builds[0].OperatingSystem != host.OperatingSystem ||
		builds[0].Architecture != host.Architecture || builds[0].CgoEnabled != host.CgoEnabled ||
		!slices.Equal(builds[0].Tags, host.Tags) {
		test.Fatalf("the builds are %v, and the host's %v is not first", builds, host)
	}
	for _, each := range builds {
		if each.Architecture != host.Architecture {
			test.Fatalf("the builds hold %v, on another architecture than the host's %s", each, host.Architecture)
		}
	}
	// Every file of ours outside unbuilt is compiled by at least one build, by go/build's own reading.
	for _, file := range census.Ours {
		if slices.ContainsFunc(unbuilt, func(each project.Unbuilt) bool { return each.File == file }) {
			continue
		}
		contents, contentsError := os.ReadFile(file)
		if contentsError != nil {
			test.Fatal(contentsError)
		}
		compiledBy := 0
		for _, each := range builds {
			compiled, compileError := each.Compiles(file, contents)
			if compileError != nil {
				test.Fatal(compileError)
			}
			if compiled {
				compiledBy++
			}
		}
		if compiledBy == 0 {
			test.Errorf("no build of %v compiles %s", builds, file)
		}
	}
	for _, each := range builds {
		test.Logf("build %s", each)
	}
	for _, each := range unbuilt {
		relative, relativeError := filepath.Rel(root, each.File)
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		test.Logf("unbuilt %s: %s", relative, each.Reason)
	}
}

// hostAt is the host's build as cohere-go's loader reads it from go env (load's constraintContext), read in this
// process: the target go/build.Default reads, as go env does from the environment or the binary's own, cgo as
// go_root decides it, and `GOFLAGS`, with its `-tags` as the context's tags. `GOFLAGS` is the environment's,
// which cohere-dev's test sets whole, so go env would read no go env file's in its place.
func hostAt(test *testing.T) project.Build {
	test.Helper()
	goFlags := strings.Fields(os.Getenv("GOFLAGS"))
	buildContext := build.Default
	buildContext.CgoEnabled = go_root.CgoEnabled(test)
	buildContext.BuildTags = nil
	for _, flag := range goFlags {
		if tags, isTags := strings.CutPrefix(strings.TrimLeft(flag, "-"), "tags="); isTags {
			buildContext.BuildTags = append(buildContext.BuildTags,
				strings.FieldsFunc(tags, func(character rune) bool { return character == ',' })...)
		}
	}
	return project.HostBuild(&buildContext, goFlags)
}
