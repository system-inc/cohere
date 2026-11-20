package load

import (
	"bytes"
	"crypto/sha256"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// contextInputs are what contextKey reads: the environment as given, the whole `GOFLAGS`, `CGO_ENABLED` and go env's
// answer, its toolchain root and go env file in a directory of the test's own.
type contextInputs struct {
	given      []string
	goFlags    []string
	cgoEnabled string
	answer     map[string]string
}

// contextInputsIn are an extra build's contextInputs in directory, which holds the toolchain root the answer names,
// with a go.env and a bin/go, and the go env file it names.
func contextInputsIn(test *testing.T, directory string) contextInputs {
	test.Helper()
	writeListingFiles(test, directory, map[string]string{
		"goroot/go.env": "GOTOOLCHAIN=auto\n",
		"goroot/bin/go": "a go binary\n",
		"env":           "GOPRIVATE=example.com\n",
	})
	return contextInputs{
		given: []string{"HOME=/home/someone", "PATH=/usr/bin", "GOWORK=off", "GOOS=linux", "GOARCH=amd64",
			"CGO_ENABLED=0", "GOFLAGS=-p=4 -pgo=off -tags=cohere_extra"},
		goFlags:    []string{"-p=4", "-pgo=off", "-tags=cohere_extra", "-pgo=off"},
		cgoEnabled: "0",
		answer: map[string]string{"GOROOT": filepath.Join(directory, "goroot"), "GOVERSION": "go1.27.0",
			"GOEXPERIMENT": "", "GOENV": filepath.Join(directory, "env"), "GOFIPS140": "off"},
	}
}

// contextKeyOf is contextKey of inputs.
func contextKeyOf(inputs contextInputs) ([sha256.Size]byte, bool) {
	return contextKey(inputs.given, inputs.goFlags, inputs.cgoEnabled, inputs.answer)
}

// The build context's key moves with each input that can change a line go list prints of it, each changed alone: the
// target, cgo, the tags, -race, the experiments, an architecture level as the process gives it, the C compiler, the
// toolchain's root and version, the go env file's bytes, the toolchain's go.env, and its go's size or time. It stays
// with what changes no line: `HOME`, `PATH` as given, which go never looks itself up on, -p, and `GOFLAGS` as given,
// which the whole flags stand for. None is kept under an overlay, with `GOFIPS140` on, or with no `GOROOT`. Each case
// is one input alone, so dropping any from the key turns its case red.
func TestTheBuildContextsKeyMovesWithEveryInputAlone(test *testing.T) {
	test.Parallel()
	given := func(variable string) func(*testing.T, *contextInputs, string) {
		return func(_ *testing.T, inputs *contextInputs, _ string) {
			inputs.given = append(inputs.given, variable)
		}
	}
	flag := func(flag string) func(*testing.T, *contextInputs, string) {
		return func(_ *testing.T, inputs *contextInputs, _ string) {
			inputs.goFlags = append(inputs.goFlags, flag)
		}
	}
	answer := func(name string, value string) func(*testing.T, *contextInputs, string) {
		return func(_ *testing.T, inputs *contextInputs, _ string) {
			inputs.answer[name] = value
		}
	}
	rewrite := func(name string, contents string) func(*testing.T, *contextInputs, string) {
		return func(test *testing.T, _ *contextInputs, directory string) {
			writeListingFiles(test, directory, map[string]string{name: contents})
		}
	}
	cases := []struct {
		name   string
		change func(test *testing.T, inputs *contextInputs, directory string)
		moves  bool
		off    bool
	}{
		{name: "GOOS", change: given("GOOS=freebsd"), moves: true},
		{name: "GOARCH", change: given("GOARCH=arm64"), moves: true},
		{name: "CGO_ENABLED", change: func(_ *testing.T, inputs *contextInputs, _ string) {
			inputs.cgoEnabled = "1"
		}, moves: true},
		{name: "-tags", change: flag("-tags=cohere_other"), moves: true},
		{name: "-race", change: flag("-race"), moves: true},
		{name: "GOEXPERIMENT", change: answer("GOEXPERIMENT", "jsonv2"), moves: true},
		{name: "GOAMD64 as given", change: given("GOAMD64=v3"), moves: true},
		{name: "GOARM64 as given", change: given("GOARM64=v9.0"), moves: true},
		{name: "GOPPC64 as given", change: given("GOPPC64=power10"), moves: true},
		{name: "CC", change: given("CC=clang"), moves: true},
		{name: "GOROOT", change: func(test *testing.T, inputs *contextInputs, directory string) {
			// Another root holding the same files, so the root's path alone moves the key.
			other := filepath.Join(directory, "other")
			writeListingFiles(test, other, map[string]string{"go.env": "GOTOOLCHAIN=auto\n",
				"bin/go": "a go binary\n"})
			inputs.answer["GOROOT"] = other
		}, moves: true},
		{name: "GOVERSION", change: answer("GOVERSION", "go1.27.1"), moves: true},
		{name: "the go env file's bytes", change: rewrite("env", "GOAMD64=v3\n"), moves: true},
		{name: "go.env's bytes", change: rewrite("goroot/go.env", "GOEXPERIMENT=jsonv2\n"), moves: true},
		{name: "the go binary's size", change: rewrite("goroot/bin/go", "a longer go binary\n"), moves: true},
		{name: "the go binary's time", change: func(test *testing.T, _ *contextInputs, directory string) {
			then := time.Unix(1, 0)
			binary := filepath.Join(directory, "goroot", "bin", "go")
			if timesError := os.Chtimes(binary, then, then); timesError != nil {
				test.Fatal(timesError)
			}
		}, moves: true},
		{name: "HOME", change: given("HOME=/home/another")},
		{name: "PATH as given", change: given("PATH=/elsewhere")},
		{name: "-p", change: flag("-p=8")},
		{name: "GOFLAGS as given", change: given("GOFLAGS=-p=8")},
		{name: "an overlay", change: flag("-overlay=overlay.json"), off: true},
		{name: "GOFIPS140 on", change: answer("GOFIPS140", "v1.0.0"), off: true},
		{name: "no GOROOT", change: answer("GOROOT", ""), off: true},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			directory := test.TempDir()
			inputs := contextInputsIn(test, directory)
			before, keptBefore := contextKeyOf(inputs)
			if !keptBefore {
				test.Fatal("the build context of the inputs as written is kept nowhere")
			}
			each.change(test, &inputs, directory)
			after, keptAfter := contextKeyOf(inputs)
			if keptAfter == each.off {
				test.Fatalf("the build context is kept: %t, want %t", keptAfter, !each.off)
			}
			if !each.off && (after != before) != each.moves {
				test.Errorf("the key moved: %t, want %t", after != before, each.moves)
			}
		})
	}
}

// A build context the system cache holds is go list's own answer, line for line, for the host and for a build of
// another system: a first load runs the command and keeps what it printed, and a second, under an answer read again,
// runs none and reads it, the same bytes a load that keeps nothing prints.
func TestAKeptBuildContextIsGoListsOwnAnswer(test *testing.T) {
	test.Parallel()
	workspace, environment := writeSharedBuildList(test, sharedBuildListFiles)
	builds := sharedBuildListBuilds(test, workspace, environment)
	host := goEnvironmentOf(test, workspace, environment)
	contexts, stop := WatchBuildContexts(workspace)
	defer stop()
	store := newMemoryStore()
	for _, environment := range [][]string{environment, slices.Concat(environment, builds[2].Environment())} {
		unkept := goEnvironmentOf(test, workspace, environment)
		unkeptOutputs, unkeptFailures := unkept.listerState.fetch(unkept, nil)
		if unkeptFailures[buildContext] != nil {
			test.Fatal(unkeptFailures[buildContext])
		}
		for _, round := range []struct {
			name string
			runs int64
		}{{name: "first", runs: 1}, {name: "second", runs: 0}} {
			kept := goEnvironmentOf(test, workspace, environment)
			if !kept.KeepContextIn(store, host, false) {
				test.Fatalf("the %s load under %q keeps no build context", round.name, environment)
			}
			contexts.Store(0)
			outputs, failures := kept.listerState.fetch(kept, nil)
			if failures[buildContext] != nil {
				test.Fatal(failures[buildContext])
			}
			if ran := contexts.Load(); ran != round.runs {
				test.Errorf("the %s load under %q ran %d build contexts, want %d", round.name, environment, ran,
					round.runs)
			}
			if !bytes.Equal(outputs[buildContext], unkeptOutputs[buildContext]) {
				test.Errorf("the %s load under %q read the build context\n%s\nwant go list's\n%s", round.name,
					environment, outputs[buildContext], unkeptOutputs[buildContext])
			}
		}
	}
	if count := store.count(); count != 2 {
		test.Errorf("the store keeps %d build contexts, want the host's and the other system's", count)
	}
}

// A build whose own go env contradicts the key predicted from the host's, as a toolchain root of its own would, runs
// its build context though the store holds one under the predicted key, and keeps nothing: the check of its own
// answer refuses it, while a build its answer agrees with reads what the store holds.
func TestAContradictedBuildContextRunsItsCommand(test *testing.T) {
	test.Parallel()
	workspace, environment := writeSharedBuildList(test, sharedBuildListFiles)
	builds := sharedBuildListBuilds(test, workspace, environment)
	host := goEnvironmentOf(test, workspace, environment)
	buildEnvironment := slices.Concat(environment, builds[2].Environment())
	store := newMemoryStore()
	planted := []byte("planted\n")
	predicted, keeps := predictedContextKey(host, buildEnvironment)
	if !keeps {
		test.Fatal("the build's build context is kept nowhere")
	}
	// Ignored: the memory store keeps everything.
	_ = store.Keep(predicted, planted)
	contexts, stop := WatchBuildContexts(workspace)
	defer stop()

	agreeing := goEnvironmentOf(test, workspace, buildEnvironment)
	agreeing.KeepContextIn(store, host, false)
	agreeingOutputs, _ := agreeing.listerState.fetch(agreeing, nil)
	if !bytes.Equal(agreeingOutputs[buildContext], planted) || contexts.Load() != 0 {
		test.Fatalf("a build its answer agrees with ran %d build contexts and read %q, want none and the store's",
			contexts.Load(), agreeingOutputs[buildContext])
	}

	contradicted := goEnvironmentOf(test, workspace, buildEnvironment)
	contradicted.KeepContextIn(store, host, false)
	contradicted.answer = maps.Clone(contradicted.answer)
	contradicted.answer["GOROOT"] = filepath.Join(test.TempDir(), "another-goroot")
	outputs, failures := contradicted.listerState.fetch(contradicted, nil)
	if failures[buildContext] != nil {
		test.Fatal(failures[buildContext])
	}
	if contexts.Load() != 1 || bytes.Equal(outputs[buildContext], planted) {
		test.Errorf("the contradicted build ran %d build contexts and read %q, want its own", contexts.Load(),
			outputs[buildContext])
	}
	if count := store.count(); count != 1 {
		test.Errorf("the store keeps %d build contexts, want the planted one alone", count)
	}
}

// inconsistentVendorFiles are a module whose vendor directory does not hold what its go.mod requires: go env answers
// there, and every go list fails.
var inconsistentVendorFiles = map[string]string{
	"go.mod":             "module example.com/vendored\n\ngo 1.24\n\nrequire example.com/nowhere v1.0.0\n",
	"vendored.go":        "package vendored\n",
	"vendor/modules.txt": "# example.com/other v1.0.0\n",
}

// A build context go list fails to print is never kept: the next load runs the command again.
func TestAFailedBuildContextIsNeverKept(test *testing.T) {
	test.Parallel()
	workspace, environment := writeSharedBuildList(test, inconsistentVendorFiles)
	environment = append(environment, "GOWORK=off")
	store := newMemoryStore()
	contexts, stop := WatchBuildContexts(workspace)
	defer stop()
	for _, round := range []string{"first", "second"} {
		goEnvironment := goEnvironmentOf(test, workspace, environment)
		if !goEnvironment.KeepContextIn(store, goEnvironment, false) {
			test.Fatalf("the %s load keeps no build context", round)
		}
		if _, failures := goEnvironment.listerState.fetch(goEnvironment, nil); failures[buildContext] == nil {
			test.Fatalf("the %s load's build context did not fail, so the fixture proves nothing", round)
		}
	}
	if count := store.count(); count != 0 {
		test.Errorf("the store keeps %d build contexts, want none", count)
	}
	if ran := contexts.Load(); ran != 2 {
		test.Errorf("the two loads ran %d build contexts, want one each", ran)
	}
}

// A build under an overlay, or in a run under `--timing`, or with no store keeps no build context.
func TestABuildContextIsKeptOnlyWhereItCanBe(test *testing.T) {
	test.Parallel()
	workspace, environment := writeSharedBuildList(test, sharedBuildListFiles)
	host := goEnvironmentOf(test, workspace, environment)
	overlaid := &GoEnvironment{environment: slices.Concat(environment, []string{"GOFLAGS=-overlay=overlay.json"}),
		listerState: &listerState{}}
	if overlaid.KeepContextIn(newMemoryStore(), host, false) {
		test.Error("a build under an overlay keeps its build context")
	}
	plain := goEnvironmentOf(test, workspace, environment)
	if plain.KeepContextIn(newMemoryStore(), host, true) {
		test.Error("a build in a run under timing keeps its build context")
	}
	if plain.KeepContextIn(nil, host, false) {
		test.Error("a build with no store keeps its build context")
	}
	if !plain.KeepContextIn(newMemoryStore(), host, false) {
		test.Error("a plain build keeps no build context")
	}
}

// The key every extra build a run checks at cohere's own root predicts from the host's go env, before its own, is the
// key of its own answer, and so is the host's of itself, under real go env answers: a toolchain whose answer for a
// build differs from the prediction, its flags, cgo or toolchain, turns this red, and a run would then run every
// build's context each time.
func TestCoheresOwnBuildsBuildContextKeysArePredicted(test *testing.T) {
	test.Parallel()
	workspace, builds := coheresBuilds(test)
	host := goEnvironmentOf(test, workspace.Root, workspace.Environment())
	requirePredicted := func(test *testing.T, goEnvironment *GoEnvironment) {
		test.Helper()
		predicted, predictedKeeps := predictedContextKey(host, goEnvironment.environment)
		own, ownKeeps := ownContextKey(goEnvironment)
		if !predictedKeeps || !ownKeeps || predicted != own {
			test.Errorf("the key predicted from the host's go env is kept %t and %x, its own answer's %t and %x",
				predictedKeeps, predicted, ownKeeps, own)
		}
	}
	requirePredicted(test, host)
	for _, extra := range builds[1:] {
		test.Run(strings.NewReplacer("/", "_", " ", "_", ",", "_").Replace(extra.String()), func(test *testing.T) {
			test.Parallel()
			requirePredicted(test, goEnvironmentOf(test, workspace.Root,
				slices.Concat(workspace.Environment(), extra.Environment())))
		})
	}
}
