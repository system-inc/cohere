package incremental_test

import (
	"reflect"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/shim/incremental"
)

// upstreamPackagePath is where the incremental machinery's own types are declared, inside the CLI driver.
const upstreamPackagePath = "github.com/microsoft/TypeScript/tsc/internal/execute/incremental"

// TestShimSurfaceIsReachable proves the linkname surface resolves at build time and that the
// re-exported symbols are the upstream ones rather than empty stand-ins.
//
// This is a compile-and-reference test on purpose. Calling ReadBuildInfoProgram for real needs
// a compiler host and a parsed command line, which is the integration this shim exists to
// enable rather than something the shim itself should own. What can fail silently here is the
// shim drifting out of sync with upstream after a typescript-go bump, and that failure shows
// up as a build error the moment these references stop resolving.
func TestShimSurfaceIsReachable(test *testing.T) {
	test.Parallel()

	// Each assignment is a distinct way the shim can break: a renamed function, a moved type,
	// a changed signature. Binding the function to a typed variable checks the signature
	// without needing a live host, and it fails at compile time rather than here, which is
	// the point. A nil check would not have worked: these are package-level funcs, so the
	// compiler rejects `== nil` as always false, and a test that cannot fail proves nothing.
	var _ func(
		compiler.CompilerHost,
	) incremental.BuildInfoReader = incremental.NewBuildInfoReader
	var _ func(
		compiler.CompilerHost,
	) incremental.Host = incremental.CreateHost
	var _ func(
		*tsoptions.ParsedCommandLine,
		incremental.BuildInfoReader,
		compiler.CompilerHost,
	) *incremental.Program = incremental.ReadBuildInfoProgram

	// The type aliases must name real upstream types rather than local stand-ins. A stand-in the shim declared
	// itself would still satisfy every binding above, so this reads the package each type was declared in.
	aliasedTypes := []reflect.Type{reflect.TypeFor[incremental.Program](), reflect.TypeFor[incremental.BuildInfo]()}
	for _, aliasedType := range aliasedTypes {
		if aliasedType.PkgPath() != upstreamPackagePath {
			test.Errorf("%s is declared in %q, want upstream's %q", aliasedType.Name(), aliasedType.PkgPath(),
				upstreamPackagePath)
		}
	}
}

// staticBuildInfoReader answers ReadBuildInfo with a fixed build info in place of a read from disk. Each of
// ReadBuildInfoProgram's three nil answers returns before it touches the compiler host, so none of them needs one.
type staticBuildInfoReader struct {
	buildInfo *incremental.BuildInfo
}

func (reader staticBuildInfoReader) ReadBuildInfo(_ *tsoptions.ParsedCommandLine) *incremental.BuildInfo {
	return reader.buildInfo
}

// TestBuildInfoNilContract checks the one behaviour a caller is most likely to get wrong.
//
// ReadBuildInfoProgram returns nil for three different reasons that all mean "rebuild
// everything" rather than "an error occurred": no build info on disk, a version mismatch, and
// a build info that was not written incrementally (incremental.go:47). A caller that treats
// nil as a failure will report an error on every first run; one that treats it as an empty
// cache is correct.
//
// A returned *Program from this path has a nil `program` field; GetProgram panics on it
// (program.go:110). It is change detection, not a warm graph.
func TestBuildInfoNilContract(test *testing.T) {
	test.Parallel()

	// The version mismatch carries file names, so the version alone is what sends it back as nil.
	mismatchedVersion := &incremental.BuildInfo{Version: "0.0.0-not-this-compiler"}
	mismatchedVersion.FileNames = append(mismatchedVersion.FileNames, "index.ts")

	cases := []struct {
		name      string
		buildInfo *incremental.BuildInfo
	}{
		{name: "absent build info", buildInfo: nil},
		{name: "version mismatch", buildInfo: mismatchedVersion},
		{name: "not written incrementally", buildInfo: &incremental.BuildInfo{Version: core.Version()}},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			reader := staticBuildInfoReader{buildInfo: testCase.buildInfo}
			if program := incremental.ReadBuildInfoProgram(nil, reader, nil); program != nil {
				subtest.Errorf("ReadBuildInfoProgram returned a program, want nil so the caller rebuilds everything")
			}
		})
	}
}
