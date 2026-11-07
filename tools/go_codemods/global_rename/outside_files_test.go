package global_rename

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

// A file behind a tag of its own is built by no platform, so nothing type-checks it and its text keeps the name.
func TestNameInAFileNoPlatformBuildsRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

func openURL() string { return "" }

func Run() string { return openURL() }
`,
		"internal/a/a_planted.go": `//go:build codemods_planted

package a

func other() string { return openURL() }
`,
	})
	loaded := fixture.load(test)
	result := Plan(
		loaded,
		propose(test, loaded, target{PackageSuffix: "internal/a", Name: "openURL", Kind: KindFunc, NewName: "openUrl"}),
		Options{},
	)
	requireRefused(test, result, "openURL", "outside the load")
	if !strings.Contains(
		strings.Join(result.Notes, "\n"),
		"built by no platform, so read as text: internal/a/a_planted.go",
	) {
		test.Fatalf("the unbuilt file was not reported: %v", result.Notes)
	}
}

// A file only another platform builds is loaded under that platform and read with types: its call of the
// function is the function, and is renamed with it, then proven under that platform before anything is written.
func TestNameInAnotherPlatformsFileRenamedThere(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

func openURL() string { return "" }

func Run() string { return openURL() }
`,
		"internal/a/a_plan9.go": `//go:build plan9

package a

func other() string { return openURL() }
`,
	})
	loaded := fixture.load(test)
	result := Plan(
		loaded,
		propose(test, loaded, target{PackageSuffix: "internal/a", Name: "openURL", Kind: KindFunc, NewName: "openUrl"}),
		Options{},
	)
	requireAccepted(test, result, 1)
	var output bytes.Buffer
	report := bufio.NewWriter(&output)
	mismatches, applyError := Apply(
		loaded,
		fixture.root,
		nil,
		result,
		report,
	)
	if flushError := report.Flush(); flushError != nil {
		test.Fatalf("writing the report: %v", flushError)
	}
	if applyError != nil || mismatches != 0 {
		test.Fatalf("applying: %d mismatches, %v\n%s", mismatches, applyError, output.String())
	}
	if !strings.Contains(output.String(), "re-verification as plan9/amd64:") {
		test.Fatalf("the rewrite was not verified under plan9:\n%s", output.String())
	}
	want := `//go:build plan9

package a

func other() string { return openUrl() }
`
	if source := fixture.read(test, "internal/a/a_plan9.go"); source != want {
		test.Fatalf("the plan9 file reads\n%s\nwant\n%s", source, want)
	}
}

// A field's name spelled in another platform's file by a local of the same name is the local's: the field is
// renamed and the file is left as it was. This is cohere's err field beside memory_linux.go's err.
func TestFieldNameSpelledByALocalInAnotherPlatformsFileIsRenamed(test *testing.T) {
	test.Parallel()
	twin := `//go:build plan9

package a

import "errors"

func read() error {
	err := errors.New("no clock")
	return err
}
`
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

type answer struct{ err error }

func Read(value answer) error { return value.err }
`,
		"internal/a/a_plan9.go": twin,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "err", Kind: KindField, Owner: "answer", NewName: "readError"},
	), Options{})
	requireAccepted(test, result, 1)
	if len(result.TwinEdits) != 0 {
		test.Fatalf("the local was given edits: %+v", result.TwinEdits)
	}
	applyClean(test, fixture, loaded, result)
	if source := fixture.read(test, "internal/a/a_plan9.go"); source != twin {
		test.Fatalf("the plan9 file was touched:\n%s", source)
	}
	if source := fixture.read(test, "internal/a/a.go"); !strings.Contains(source, "value.readError") {
		test.Fatalf("the field was not renamed:\n%s", source)
	}
}

// A type that another platform's file gives a method of the new name keeps its field's name.
func TestFieldWhoseNewNameIsAnotherPlatformsMethodRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

type answer struct{ err error }

func Read(value answer) error { return value.err }
`,
		"internal/a/a_plan9.go": `//go:build plan9

package a

func (value answer) readError() error { return value.err }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "err", Kind: KindField, Owner: "answer", NewName: "readError"},
	), Options{})
	requireRefused(test, result, "err", "only another platform builds")
}

// A platform twin read with types: the host's declaration and the platform's are each in a file the other does
// not build, and the twin, its doc comment and its own file's use are renamed together.
func TestPlatformTwinReadWithTypesRenamedWithItsUses(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

func Run() int { return threadCPU() }
`,
		"internal/a/a_other.go": `//go:build !plan9

package a

// threadCPU reads the clock.
func threadCPU() int { return 1 }
`,
		"internal/a/a_plan9.go": `//go:build plan9

package a

// threadCPU has no clock here.
func threadCPU() int { return 0 }

func twice() int { return threadCPU() + threadCPU() }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "threadCPU", Kind: KindFunc, NewName: "threadCpu"},
	), Options{})
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
	want := `//go:build plan9

package a

// threadCpu has no clock here.
func threadCpu() int { return 0 }

func twice() int { return threadCpu() + threadCpu() }
`
	if source := fixture.read(test, "internal/a/a_plan9.go"); source != want {
		test.Fatalf("the twin reads\n%s\nwant\n%s", source, want)
	}
}

// Apply proves the rewrite under every platform the plan read: a plan that leaves the platform's use of a renamed
// field behind does not type-check there, and nothing is written.
func TestApplyWritesNothingWhenAnotherPlatformsRewriteFails(test *testing.T) {
	test.Parallel()
	source := `package a

type answer struct{ err error }

func Read(value answer) error { return value.err }
`
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": source,
		"internal/a/a_plan9.go": `//go:build plan9

package a

func again(value answer) error { return value.err }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "err", Kind: KindField, Owner: "answer", NewName: "readError"},
	), Options{})
	requireAccepted(test, result, 1)
	if len(result.TwinEdits) != 1 {
		test.Fatalf("want the plan9 use renamed, got %+v", result.TwinEdits)
	}
	result.TwinEdits = nil
	var output bytes.Buffer
	report := bufio.NewWriter(&output)
	mismatches, applyError := Apply(loaded, fixture.root, nil, result, report)
	if flushError := report.Flush(); flushError != nil {
		test.Fatalf("writing the report: %v", flushError)
	}
	if applyError != nil {
		test.Fatalf("applying: %v\n%s", applyError, output.String())
	}
	if mismatches == 0 ||
		!strings.Contains(output.String(), "type-checks as plan9/amd64 before the rewrite and not after") {
		test.Fatalf("mismatches %d, want the plan9 package refused:\n%s", mismatches, output.String())
	}
	if got := fixture.read(test, "internal/a/a.go"); got != source {
		test.Fatalf("a.go was written despite the mismatch:\n%s", got)
	}
}

// An unexported field is selected only inside its package: a build-constrained file of another package spelling
// its name, as a parameter, leaves the rename alone, and one of its own package keeps the name.
func TestUnexportedFieldSpelledOutsideTheLoad(test *testing.T) {
	test.Parallel()
	files := map[string]string{
		"internal/a/a.go": `package a

type walker struct{ ctx int }

func Run() int { return walker{ctx: 1}.ctx }
`,
		"internal/b/b_plan9.go": `//go:build plan9

package b

import "fixture/internal/a"

func Other(ctx int) int { return ctx + a.Run() }
`,
	}
	fixture := newFixture(test, files)
	loaded := fixture.load(test)
	result := Plan(
		loaded,
		propose(test, loaded, target{PackageSuffix: "internal/a", Name: "ctx", Kind: KindField, NewName: "count"}),
		Options{},
	)
	requireAccepted(test, result, 1)
	requireModelClean(test, result)

	files["internal/a/a_plan9.go"] = `//go:build plan9

package a

func other() int { return walker{}.ctx }
`
	samePackage := newFixture(test, files)
	loaded = samePackage.load(test)
	// The same package's plan9 file is no longer read as text: the platform load type-checks it under plan9 and
	// renames its selector with the field (platform_reading.go), so the rename is accepted.
	result = Plan(
		loaded,
		propose(test, loaded, target{PackageSuffix: "internal/a", Name: "ctx", Kind: KindField, NewName: "count"}),
		Options{},
	)
	requireAccepted(test, result, 1)
}

// A build-constrained file selecting `runtime.GOOS` spells the runtime package's name, not the field's.
func TestForeignPackageSelectorOutsideTheLoadIsNotOurName(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

type Target struct{ GOOS string }

func Read(target Target) string { return target.GOOS }
`,
		"internal/a/a_plan9.go": `//go:build plan9

package a

import "runtime"

func host() string { return runtime.GOOS }
`,
	})
	loaded := fixture.load(test)
	result := Plan(
		loaded,
		propose(test, loaded, target{PackageSuffix: "internal/a", Name: "GOOS", Kind: KindField, NewName: "Goos"}),
		Options{},
	)
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
	if source := fixture.read(test, "internal/a/a_plan9.go"); !strings.Contains(source, "runtime.GOOS") {
		test.Fatalf("the runtime selector was touched:\n%s", source)
	}
}

// The same selector on a local named like an import is the local's field: read as text, in a file no platform
// builds, it keeps the name.
func TestLocalSelectorOutsideTheLoadStillRefuses(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

type Target struct{ GOOS string }

func Read(target Target) string { return target.GOOS }
`,
		"internal/a/a_planted.go": `//go:build codemods_planted

package a

import "runtime"

func processors() int { return runtime.NumCPU() }

func host() string {
	runtime := Target{}
	return runtime.GOOS
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(
		loaded,
		propose(test, loaded, target{PackageSuffix: "internal/a", Name: "GOOS", Kind: KindField, NewName: "Goos"}),
		Options{},
	)
	requireRefused(test, result, "GOOS", "outside the load")
}

// Read with types under the platform that builds it, the selector on the local is the field and is renamed, while
// the runtime package's selector beside it stays.
func TestLocalSelectorInAnotherPlatformsFileRenamed(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

type Target struct{ GOOS string }

func Read(target Target) string { return target.GOOS }
`,
		"internal/a/a_plan9.go": `//go:build plan9

package a

import "runtime"

func processors() string { return runtime.GOOS }

func host() string {
	runtime := Target{}
	return runtime.GOOS
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(
		loaded,
		propose(test, loaded, target{PackageSuffix: "internal/a", Name: "GOOS", Kind: KindField, NewName: "Goos"}),
		Options{},
	)
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
	want := `//go:build plan9

package a

import "runtime"

func processors() string { return runtime.GOOS }

func host() string {
	runtime := Target{}
	return runtime.Goos
}
`
	if source := fixture.read(test, "internal/a/a_plan9.go"); source != want {
		test.Fatalf("the plan9 file reads\n%s\nwant\n%s", source, want)
	}
}

// A package-level function is seen only by its own package and its importers: a build-constrained file of
// another package that imports neither spells some other threadCPU.
func TestPackageLevelNameInAnUnrelatedOutsideFileIsRenamed(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

func threadCPU() int { return 1 }

func Run() int { return threadCPU() }
`,
		"internal/b/b.go": `package b

func Other() int { return 0 }
`,
		"internal/b/b_plan9.go": `//go:build plan9

package b

func threadCPU() int { return 0 }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "threadCPU", Kind: KindFunc, NewName: "threadCpu"},
	), Options{})
	requireAccepted(test, result, 1)
	if len(result.TwinEdits) != 0 {
		test.Fatalf("another package's file was given twin edits: %+v", result.TwinEdits)
	}
	applyClean(test, fixture, loaded, result)
	if source := fixture.read(test, "internal/b/b_plan9.go"); !strings.Contains(source, "func threadCPU()") {
		test.Fatalf("another package's declaration was renamed:\n%s", source)
	}
}

// A platform twin, its own package's build-constrained declaration of the same function, is renamed with it,
// doc comment and all.
func TestPlatformTwinRenamedWithItsDeclaration(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

// threadCPU reads the clock.
func threadCPU() int { return 1 }

func Run() int { return threadCPU() }
`,
		"internal/a/a_plan9.go": `//go:build plan9

package a

// threadCPU has no clock here; see threadCPU in a.go.
func threadCPU() int { return 0 }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "threadCPU", Kind: KindFunc, NewName: "threadCpu"},
	), Options{})
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
	want := `//go:build plan9

package a

// threadCpu has no clock here; see threadCpu in a.go.
func threadCpu() int { return 0 }
`
	if source := fixture.read(test, "internal/a/a_plan9.go"); source != want {
		test.Fatalf("the twin reads\n%s\nwant\n%s", source, want)
	}
}

// A twin that also uses the name, or a file of the package outside the load that spells the new name, keeps it.
func TestPlatformTwinWithUseOrTakenNameRefused(test *testing.T) {
	test.Parallel()
	for name, twin := range map[string]string{
		"a use": `//go:build plan9

package a

func threadCPU() int { return 0 }

func other() int { return threadCPU() }
`,
		"the new name taken": `//go:build plan9

package a

func threadCPU() int { return 0 }

func threadCpu() int { return 0 }
`,
	} {
		fixture := newFixture(test, map[string]string{
			"internal/a/a.go": `package a

func threadCPU() int { return 1 }

func Run() int { return threadCPU() }
`,
			"internal/a/a_plan9.go": twin,
		})
		loaded := fixture.load(test)
		result := Plan(loaded, propose(
			test,
			loaded,
			target{PackageSuffix: "internal/a", Name: "threadCPU", Kind: KindFunc, NewName: "threadCpu"},
		), Options{})
		if len(result.Accepted) != 0 || len(result.TwinEdits) != 0 {
			test.Fatalf("%s: accepted %+v with twin edits %+v", name, result.Accepted, result.TwinEdits)
		}
		requireRefused(test, result, "threadCPU", "outside the load")
	}
}

// An unexported field is selected only inside its own package, so a file outside every package that spells its
// name (a testdata fixture importing the package) spells something else, and the field is renamed; an exported
// field named there keeps its name, and so does an unexported one named in a file of its own directory that no
// platform builds.
func TestUnexportedFieldKeptOnlyByAnOutsideFileOfItsOwnDirectory(test *testing.T) {
	test.Parallel()
	sources := map[string]string{
		"internal/a/a.go": `package a

type result struct{ err error }

type Result struct{ Err error }

func Run() (error, error) { return result{}.err, Result{}.Err }
`,
		"internal/b/testdata/planted.go": `package planted

import "fixture/internal/a"

func Other() (error, error) { a.Run(); var err, Err error; return err, Err }
`,
	}
	fixture := newFixture(test, sources)
	loaded := fixture.load(test)
	result := Plan(loaded, append(
		propose(test, loaded, target{PackageSuffix: "internal/a", Name: "err", Kind: KindField, NewName: "runError"}),
		propose(
			test,
			loaded,
			target{PackageSuffix: "internal/a", Name: "Err", Kind: KindField, NewName: "RunError"},
		)...,
	), Options{})
	requireAccepted(test, result, 1)
	if result.Accepted[0].OldName != "err" {
		test.Fatalf("accepted %+v, want the unexported field", result.Accepted)
	}
	requireRefused(test, result, "Err", "outside the load")

	sources["internal/a/a_planted.go"] = `//go:build codemods_planted

package a

func other() error { return result{}.err }
`
	fixture = newFixture(test, sources)
	loaded = fixture.load(test)
	result = Plan(
		loaded,
		propose(test, loaded, target{PackageSuffix: "internal/a", Name: "err", Kind: KindField, NewName: "runError"}),
		Options{},
	)
	requireRefused(test, result, "err", "outside the load")
}
