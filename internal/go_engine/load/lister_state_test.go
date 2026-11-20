package load

import (
	"bytes"
	"maps"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/project"
)

// ruleHost is a host's go env answer as modulesSharedWith reads it, every name of goEnvironmentNames answered, and
// ruleBuild a tag build's beside it that shares its build list: the same answer but its flags, the host's with its own
// -tags and -pgo=off twice, as the engine makes them (project.Build.Environment, ReadGoEnvironment).
func ruleHost() *GoEnvironment {
	answer := map[string]string{}
	for _, name := range goEnvironmentNames {
		answer[name] = "the host's " + name
	}
	answer["GOOS"], answer["GOARCH"], answer["CGO_ENABLED"] = "darwin", "arm64", "1"
	answer["GOFIPS140"], answer["GODEBUG"], answer["GOFLAGS"] = "off", "", "-p=4"
	return &GoEnvironment{workspace: "/workspace", answer: answer,
		reported: reportedEnvironment{goFlags: []string{"-p=4", "-pgo=off"}}}
}

// ruleBuild is ruleHost's tag build (see ruleHost).
func ruleBuild() *GoEnvironment {
	build := ruleHost()
	build.answer["GOFLAGS"] = "-p=4 -pgo=off -tags=cohere_extra"
	build.reported.goFlags = []string{"-p=4", "-pgo=off", "-tags=cohere_extra", "-pgo=off"}
	return build
}

// withFlag adds flag to goEnvironment's `GOFLAGS`, given and whole, as go env answers a flag set in the environment.
func withFlag(goEnvironment *GoEnvironment, flag string) {
	goEnvironment.answer["GOFLAGS"] += " " + flag
	goEnvironment.reported.goFlags = append(goEnvironment.reported.goFlags, flag)
}

// An extra build reads the host's build list only when every input go list -m reads is the host's, each compared
// alone (#sfjj2rh): a change to any one of them refuses, and a change to the target, cgo, the tags, the profile, the
// architecture level or the C toolchain, none of which the build list reads, still shares. Each refusal is one only
// its own comparison makes, so dropping any comparison turns a case red.
func TestABuildSharesTheHostsBuildListOnlyWhenEveryInputItReadsIsTheSame(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name string
		// change is what the case changes of the pair that shares, nil for nothing.
		change func(build *GoEnvironment, host *GoEnvironment)
		timed  bool
		shares bool
	}{
		{name: "a tag build", shares: true},
		{name: "another GOWORK", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GOWORK"] = "/elsewhere/go.work"
		}},
		{name: "another GOMODCACHE", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GOMODCACHE"] = "/elsewhere/modules"
		}},
		{name: "another GOVERSION", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GOVERSION"] = "go1.26.0"
		}},
		{name: "another GOROOT", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GOROOT"] = "/elsewhere/go"
		}},
		{name: "GOFIPS140 on in the build", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GOFIPS140"] = "v1.0.0"
		}},
		{name: "GOFIPS140 on in both", change: func(build *GoEnvironment, host *GoEnvironment) {
			build.answer["GOFIPS140"], host.answer["GOFIPS140"] = "v1.0.0", "v1.0.0"
		}},
		{name: "-mod=mod in both", change: func(build *GoEnvironment, host *GoEnvironment) {
			withFlag(build, "-mod=mod")
			withFlag(host, "-mod=mod")
		}},
		{name: "-modfile in both", change: func(build *GoEnvironment, host *GoEnvironment) {
			withFlag(build, "-modfile=x")
			withFlag(host, "-modfile=x")
		}},
		{name: "-overlay in both", change: func(build *GoEnvironment, host *GoEnvironment) {
			withFlag(build, "-overlay=x")
			withFlag(host, "-overlay=x")
		}},
		{name: "-mod=mod in the build", change: func(build *GoEnvironment, _ *GoEnvironment) {
			withFlag(build, "-mod=mod")
		}},
		{name: "a lister's flag beyond -tags and -pgo in the build", change: func(build *GoEnvironment,
			_ *GoEnvironment) {
			withFlag(build, "-trimpath")
		}},
		{name: "a name the list does not know in the build's answer", change: func(build *GoEnvironment,
			_ *GoEnvironment) {
			build.answer["GONEWNAME"] = "set"
		}},
		{name: "a name the list does not know in the host's answer", change: func(_ *GoEnvironment,
			host *GoEnvironment) {
			host.answer["GONEWNAME"] = "set"
		}},
		{name: "another workspace", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.workspace = "/elsewhere"
		}},
		{name: "a run under timing", timed: true},
		{name: "another GOOS", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GOOS"] = "linux"
		}, shares: true},
		{name: "another GOARCH", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GOARCH"] = "amd64"
		}, shares: true},
		{name: "cgo off", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["CGO_ENABLED"] = "0"
		}, shares: true},
		{name: "other -tags", change: func(build *GoEnvironment, _ *GoEnvironment) {
			withFlag(build, "--tags=cohere_other")
		}, shares: true},
		{name: "another -pgo", change: func(build *GoEnvironment, _ *GoEnvironment) {
			withFlag(build, "-pgo=auto")
		}, shares: true},
		{name: "another GOAMD64", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GOAMD64"] = "v3"
		}, shares: true},
		{name: "another GOARM", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GOARM"] = "7"
		}, shares: true},
		{name: "another GOARM64", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GOARM64"] = ""
		}, shares: true},
		{name: "another GO386", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GO386"] = "softfloat"
		}, shares: true},
		{name: "another CC", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["CC"] = "x86_64-linux-gnu-gcc"
		}, shares: true},
		{name: "another CXX", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["CXX"] = "x86_64-linux-gnu-g++"
		}, shares: true},
		{name: "another GCCGO", change: func(build *GoEnvironment, _ *GoEnvironment) {
			build.answer["GCCGO"] = "x86_64-linux-gnu-gccgo"
		}, shares: true},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			build, host := ruleBuild(), ruleHost()
			if each.change != nil {
				each.change(build, host)
			}
			if shares := modulesSharedWith(build, host, each.timed); shares != each.shares {
				test.Errorf("the build shares the host's build list: %t, want %t", shares, each.shares)
			}
		})
	}
}

// sharedBuildListFiles are a workspace whose build list names paths: a go.work using two modules and replacing a
// third with a directory beside them, the first module's go.mod naming a toolchain no newer than any go the tests run
// under, and its directory holding a file only a tag builds, which imports the second.
var sharedBuildListFiles = map[string]string{
	"go.work": "go 1.24\n\nuse (\n\t./first\n\t./second\n)\n\nreplace example.com/beside => ./beside\n",
	"first/go.mod": "module example.com/first\n\ngo 1.24\n\ntoolchain go1.25.0\n\n" +
		"require example.com/beside v0.0.0\n",
	"first/first.go": "package first\n\nimport \"example.com/beside\"\n\n// Value is beside's.\n" +
		"const Value = beside.Value\n",
	"first/tagged.go":  "//go:build shared_build_list_tag\n\npackage first\n\nimport _ \"example.com/second\"\n",
	"second/go.mod":    "module example.com/second\n\ngo 1.24\n",
	"second/second.go": "package second\n",
	"beside/go.mod":    "module example.com/beside\n\ngo 1.24\n",
	"beside/beside.go": "package beside\n\n// Value is one.\nconst Value = 1\n",
}

// writeSharedBuildList writes files to a workspace of its own and is it and the environment its go commands run under:
// `GOENV=off`, so no go env file of the person's, as one setting `GOFLAGS`, moves what go answers; `GOTOOLCHAIN=local`,
// so the toolchain line never has go switch to another toolchain, which it would download; no proxy, so nothing is
// fetched; and an empty `GOWORK`, so go finds the workspace's go.work.
func writeSharedBuildList(test *testing.T, files map[string]string) (string, []string) {
	test.Helper()
	directory, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	workspace := filepath.Join(directory, "workspace")
	writeListingFiles(test, workspace, files)
	return workspace, []string{"GOENV=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off",
		"GOMODCACHE=" + filepath.Join(directory, "modules"), "GOWORK="}
}

// sharedBuildListBuilds are the host's build in workspace under environment, as the engine reads it, and builds beside
// it the engine could make: a tag build, another operating system and architecture, and cgo off.
func sharedBuildListBuilds(test *testing.T, workspace string, environment []string) []project.Build {
	test.Helper()
	host := project.HostBuild(goEnvironmentOf(test, workspace, environment).Target())
	tagged := host
	tagged.Tags = append(slices.Clone(host.Tags), "shared_build_list_tag")
	otherSystem := host
	otherSystem.OperatingSystem, otherSystem.Architecture, otherSystem.CgoEnabled = "linux", "amd64", false
	if runtime.GOOS == "linux" {
		otherSystem.OperatingSystem = "freebsd"
	}
	cgoOff := host
	cgoOff.CgoEnabled = false
	return []project.Build{host, tagged, otherSystem, cgoOff}
}

// `go list -m -json all` prints the same bytes under the host's environment and under each extra build's, a tag
// build's, another system's and cgo off's, over a workspace whose build list names a replacement directory and holds
// a file only the tag builds, each run as the lister runs it, beside its own guard's copy of the go.work: the build
// list reads neither the target, cgo nor the tags (#sfjj2rh), and the rule says each may share it.
func TestTheBuildListIsTheSameBytesUnderEveryBuild(test *testing.T) {
	test.Parallel()
	workspace, environment := writeSharedBuildList(test, sharedBuildListFiles)
	builds := sharedBuildListBuilds(test, workspace, environment)
	host := goEnvironmentOf(test, workspace, environment)
	hostOutputs, hostFailures := host.listerState.fetch(host, nil)
	if hostFailures[buildList] != nil {
		test.Fatal(hostFailures[buildList])
	}
	for _, named := range []string{"example.com/second", filepath.Join(workspace, "beside")} {
		if !bytes.Contains(hostOutputs[buildList], []byte(named)) {
			test.Fatalf("the host's build list names no %s, so the fixture proves nothing:\n%s", named,
				hostOutputs[buildList])
		}
	}
	for _, extra := range builds[1:] {
		build := goEnvironmentOf(test, workspace, slices.Concat(environment, extra.Environment()))
		outputs, failures := build.listerState.fetch(build, nil)
		if failures[buildList] != nil {
			test.Errorf("the %s build's build list failed: %v", extra, failures[buildList])
			continue
		}
		if !bytes.Equal(outputs[buildList], hostOutputs[buildList]) {
			test.Errorf("the %s build's build list is\n%s\nwant the host's\n%s", extra, outputs[buildList],
				hostOutputs[buildList])
		}
		if !modulesSharedWith(build, host, false) {
			test.Errorf("the %s build lists its own build list, which prints the host's", extra)
		}
	}
}

// Every extra build a run checks at cohere's own root, under real go env answers, shares the host's build list, and
// holds -pgo=off once more than the host, as modulesSharedWith says: a toolchain whose go env answers a name per
// target that the rule compares turns this red, naming it.
func TestCoheresOwnBuildsShareTheHostsBuildList(test *testing.T) {
	test.Parallel()
	workspace, builds := coheresBuilds(test)
	host := goEnvironmentOf(test, workspace.Root, slices.Concat(workspace.Environment(), noGoEnvironmentFile))
	for _, extra := range builds[1:] {
		test.Run(strings.NewReplacer("/", "_", " ", "_", ",", "_").Replace(extra.String()), func(test *testing.T) {
			test.Parallel()
			build := goEnvironmentOf(test, workspace.Root, slices.Concat(workspace.Environment(), noGoEnvironmentFile,
				extra.Environment()))
			if !modulesSharedWith(build, host, false) {
				differing := []string{}
				for _, name := range slices.Sorted(maps.Keys(build.answer)) {
					if build.answer[name] != host.answer[name] {
						differing = append(differing, name+"="+build.answer[name]+" (the host's "+host.answer[name]+")")
					}
				}
				test.Errorf("the build lists its own build list; go env answers %s", strings.Join(differing, ", "))
			}
			built, hosted := countFlag(build.reported.goFlags, "-pgo=off"), countFlag(host.reported.goFlags, "-pgo=off")
			if built != hosted+1 {
				test.Errorf("the build holds -pgo=off %d times and the host %d, want one more", built, hosted)
			}
		})
	}
}

// countFlag is how many of flags are flag.
func countFlag(flags []string, flag string) int {
	count := 0
	for _, each := range flags {
		if each == flag {
			count++
		}
	}
	return count
}

// A build that shares the host's build list runs none of its own: whichever fetches first, the build here, runs the
// host's, once, under the host's environment and guard, and both read the one slice. A build whose `GOFLAGS` holds a
// flag the host's does not runs its own, which prints the same bytes.
func TestABuildThatSharesRunsNoBuildListOfItsOwn(test *testing.T) {
	test.Parallel()
	workspace, environment := writeSharedBuildList(test, sharedBuildListFiles)
	builds := sharedBuildListBuilds(test, workspace, environment)
	lists, stop := WatchBuildLists(workspace)
	defer stop()
	host := goEnvironmentOf(test, workspace, environment)
	tagged := goEnvironmentOf(test, workspace, slices.Concat(environment, builds[1].Environment()))
	trimmed := builds[0]
	trimmed.Flags = append(slices.Clone(trimmed.Flags), "-trimpath")
	ownFlags := goEnvironmentOf(test, workspace, slices.Concat(environment, trimmed.Environment()))
	if !tagged.ShareModulesOf(host, false) {
		test.Fatal("the tag build lists its own build list")
	}
	if ownFlags.ShareModulesOf(host, false) {
		test.Fatal("the build with -trimpath shares the host's build list")
	}
	taggedOutputs, taggedFailures := tagged.listerState.fetch(tagged, nil)
	hostOutputs, hostFailures := host.listerState.fetch(host, nil)
	if taggedFailures[buildList] != nil || hostFailures[buildList] != nil {
		test.Fatalf("the build list failed: %v, %v", taggedFailures[buildList], hostFailures[buildList])
	}
	if count := lists.Load(); count != 1 {
		test.Errorf("the host and the tag build ran %d build lists, want the host's alone", count)
	}
	if len(hostOutputs[buildList]) == 0 || &taggedOutputs[buildList][0] != &hostOutputs[buildList][0] {
		test.Error("the tag build reads a build list other than the host's own slice")
	}
	// The build context stays the build's own, whose build tags name the tag the host's lacks.
	if bytes.Equal(taggedOutputs[1], hostOutputs[1]) {
		test.Error("the tag build reads the host's build context")
	}
	ownOutputs, ownFailures := ownFlags.listerState.fetch(ownFlags, nil)
	if ownFailures[buildList] != nil {
		test.Fatal(ownFailures[buildList])
	}
	if count := lists.Load(); count != 2 {
		test.Errorf("the build with -trimpath left %d build lists run in all, want its own beside the host's", count)
	}
	if !bytes.Equal(ownOutputs[buildList], hostOutputs[buildList]) {
		test.Errorf("the build with -trimpath listed\n%s\nwant the host's\n%s", ownOutputs[buildList],
			hostOutputs[buildList])
	}
}

// A build that shares the host's build list shares its failure: a module requiring one nothing can fetch fails go list
// -m for the host, and the tag build reads the same failure, so its lister doubts as the host's does.
func TestABuildThatSharesTheBuildListSharesItsFailure(test *testing.T) {
	test.Parallel()
	workspace, environment := writeSharedBuildList(test, map[string]string{
		"go.mod":  "module example.com/missing\n\ngo 1.24\n\nrequire example.com/nowhere v1.0.0\n",
		"main.go": "package missing\n",
	})
	environment = append(environment, "GOWORK=off")
	builds := sharedBuildListBuilds(test, workspace, environment)
	host := goEnvironmentOf(test, workspace, environment)
	tagged := goEnvironmentOf(test, workspace, slices.Concat(environment, builds[1].Environment()))
	if !tagged.ShareModulesOf(host, false) {
		test.Fatal("the tag build lists its own build list")
	}
	_, hostFailures := host.listerState.fetch(host, nil)
	_, taggedFailures := tagged.listerState.fetch(tagged, nil)
	if hostFailures[buildList] == nil {
		test.Fatal("the host's build list did not fail, so the fixture proves nothing")
	}
	if taggedFailures[buildList] != hostFailures[buildList] {
		test.Errorf("the tag build's build list failed with %v, want the host's own failure, %v",
			taggedFailures[buildList], hostFailures[buildList])
	}
	_, hostDoubt := readGoState(host, nil)
	_, taggedDoubt := readGoState(tagged, nil)
	if hostDoubt == "" || taggedDoubt != hostDoubt {
		test.Errorf("the tag build's lister doubts %q, want the host's %q", taggedDoubt, hostDoubt)
	}
}

// An extra build is predicted to read the host's build list, before its go env answers, from its entries and the
// host's answer alone: a tag build is, and each input the prediction reads, changed alone, refuses.
func TestABuildIsPredictedToShareTheHostsBuildListFromItsEntries(test *testing.T) {
	test.Parallel()
	tagged := []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=-p=4 -pgo=off -tags=cohere_extra"}
	cases := []struct {
		name        string
		workspace   string
		environment []string
		change      func(host *GoEnvironment)
		timed       bool
		shares      bool
	}{
		{name: "a tag build", environment: tagged, shares: true},
		{name: "another -pgo", environment: append(slices.Clone(tagged), "GOFLAGS=-p=4 -pgo=auto"), shares: true},
		{name: "a run under timing", environment: tagged, timed: true},
		{name: "another workspace", workspace: "/elsewhere", environment: tagged},
		{name: "GOFIPS140 on in the host", environment: tagged, change: func(host *GoEnvironment) {
			host.answer["GOFIPS140"] = "v1.0.0"
		}},
		{name: "no flags given", environment: append(slices.Clone(tagged), "GOFLAGS=")},
		{name: "a lister's flag beyond -tags and -pgo", environment: append(slices.Clone(tagged),
			"GOFLAGS=-p=4 -trimpath")},
		{name: "-mod=mod in both", environment: append(slices.Clone(tagged), "GOFLAGS=-p=4 -mod=mod"),
			change: func(host *GoEnvironment) {
				withFlag(host, "-mod=mod")
			}},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			host := ruleHost()
			if each.change != nil {
				each.change(host)
			}
			workspace := host.workspace
			if each.workspace != "" {
				workspace = each.workspace
			}
			if shares := ModulesPredictedShared(host, workspace, each.environment, each.timed); shares != each.shares {
				test.Errorf("the build is predicted to share the host's build list: %t, want %t", shares, each.shares)
			}
		})
	}
}
