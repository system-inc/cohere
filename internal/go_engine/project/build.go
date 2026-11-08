package project

import (
	"bytes"
	"fmt"
	"go/build"
	"io"
	"path/filepath"
	"slices"
	"strings"
)

// Build is one target the workspace is loaded and checked under, as go env spells it.
type Build struct {
	OperatingSystem string
	Architecture    string
	CgoEnabled      bool

	// Tags are the build's `-tags`, sorted, each once.
	Tags []string

	// Flags are the host's `GOFLAGS` but its `-tags`, which every build runs with, so an extra build differs from the
	// host's only in what it was made for.
	Flags []string
}

// HostBuild is the host's build as the engine reads it: buildContext is the context load reads from go env
// (`GOOS`, `GOARCH`, `CGO_ENABLED`, and the `-tags` of `GOFLAGS` as its BuildTags), and goFlags are that `GOFLAGS`'s
// flags, whose `-tags` the build holds as Tags instead.
func HostBuild(buildContext *build.Context, goFlags []string) Build {
	return Build{
		OperatingSystem: buildContext.GOOS,
		Architecture:    buildContext.GOARCH,
		CgoEnabled:      buildContext.CgoEnabled,
		Tags:            sortedSet(slices.Clone(buildContext.BuildTags)),
		Flags:           withoutTags(goFlags),
	}
}

// withoutTags is goFlags without any `-tags=` flag, the spelling go's `GOFLAGS` takes it in, one dash or two.
func withoutTags(goFlags []string) []string {
	kept := []string{}
	for _, flag := range goFlags {
		if !strings.HasPrefix(strings.TrimLeft(flag, "-"), "tags=") {
			kept = append(kept, flag)
		}
	}
	return kept
}

// String is the build as a record names it: `windows/arm64`, with ` cgo` when cgo is on and its tags after
// ` tags `, as `darwin/arm64 cgo tags cohere_poison`.
func (target Build) String() string {
	label := target.OperatingSystem + "/" + target.Architecture
	if target.CgoEnabled {
		label += " cgo"
	}
	if len(target.Tags) > 0 {
		label += " tags " + strings.Join(target.Tags, ",")
	}
	return label
}

// CheckedByBuildPrefix begins the reason the project record gives a file the host's build leaves out and another
// build of the run checks (CheckedByBuild). The front door reads a reason that begins with it as a file checked, and
// every other constraint reason as a gap.
const CheckedByBuildPrefix = "checked by the "

// CheckedByBuild is the project record's reason for a file the host's build leaves out and target, an extra build of
// a run under `--other-builds`, compiles and checks: "checked by the windows/arm64 build".
func CheckedByBuild(target Build) string {
	return CheckedByBuildPrefix + target.String() + " build"
}

// Compiles reports whether the build compiles file, read from contents, as BuildsFor reads it (see compiles).
func (target Build) Compiles(file string, contents []byte) (bool, error) {
	return compiles(target, source{file: file, contents: contents, importsC: importsC(file, contents)})
}

// Environment is the entries a load under the build appends to the workspace's environment: the target, cgo, and
// `GOFLAGS` as the host's flags with the build's own `-tags`, set whole since one given in the environment replaces
// the go env file's.
func (target Build) Environment() []string {
	cgoEnabled := "0"
	if target.CgoEnabled {
		cgoEnabled = "1"
	}
	flags := slices.Clone(target.Flags)
	if len(target.Tags) > 0 {
		flags = append(flags, "-tags="+strings.Join(target.Tags, ","))
	}
	return []string{
		"GOOS=" + target.OperatingSystem,
		"GOARCH=" + target.Architecture,
		"CGO_ENABLED=" + cgoEnabled,
		"GOFLAGS=" + strings.Join(flags, " "),
	}
}

// context is the build as go/build matches a file under it, the file's bytes read from read rather than opened
// again.
func (target Build) context(read source) *build.Context {
	context := build.Default
	context.GOOS = target.OperatingSystem
	context.GOARCH = target.Architecture
	context.CgoEnabled = target.CgoEnabled
	context.BuildTags = target.Tags
	context.OpenFile = func(path string) (io.ReadCloser, error) {
		if path != read.file {
			return nil, fmt.Errorf("opening %s: only %s is held", path, read.file)
		}
		return io.NopCloser(bytes.NewReader(read.contents)), nil
	}
	return &context
}

// sameAs reports whether two builds are one target.
func (target Build) sameAs(other Build) bool {
	return target.OperatingSystem == other.OperatingSystem && target.Architecture == other.Architecture &&
		target.CgoEnabled == other.CgoEnabled && slices.Equal(target.Tags, other.Tags)
}

// compiles reports whether the build compiles a file: go/build's MatchFile reads its name and constraint lines, and
// a file that imports "C" is dropped with cgo off, as only go/build's Import drops it (load's exclusionReason reads
// it alike).
func compiles(target Build, read source) (bool, error) {
	matched, matchError := target.context(read).MatchFile(filepath.Dir(read.file), filepath.Base(read.file))
	if matchError != nil {
		return false, fmt.Errorf("reading the build constraints of %s: %w", read.file, matchError)
	}
	return matched && (target.CgoEnabled || !read.importsC), nil
}
