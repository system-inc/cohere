package workspace

import (
	"go/build"
	"path/filepath"
)

// Platform is an operating system and architecture a build-constrained file may need, as `GOOS` and `GOARCH` spell
// them.
type Platform struct {
	OperatingSystem string
	Architecture    string
}

func (platform Platform) String() string {
	return platform.OperatingSystem + "/" + platform.Architecture
}

// Platforms are the platforms tried, in order, for a file the host does not build: the first that builds it is the
// one it is loaded under. The first-class ports come first, so most files gather under a few loads.
var Platforms = []Platform{
	{"linux", "amd64"}, {"windows", "amd64"}, {"darwin", "amd64"}, {"freebsd", "amd64"},
	{"linux", "arm64"}, {"windows", "arm64"}, {"openbsd", "amd64"}, {"netbsd", "amd64"},
	{"dragonfly", "amd64"}, {"illumos", "amd64"}, {"solaris", "amd64"}, {"plan9", "amd64"},
	{"aix", "ppc64"}, {"android", "arm64"}, {"ios", "arm64"}, {"js", "wasm"}, {"wasip1", "wasm"},
	{"linux", "386"}, {"linux", "arm"}, {"linux", "riscv64"}, {"linux", "ppc64le"}, {"linux", "s390x"},
	{"linux", "loong64"}, {"linux", "mips64le"},
}

// PlatformFor is the first of Platforms that builds the file, by its name and its build constraint, with cgo
// off and no custom tags, or false when none does: a file behind a tag of its own (`//go:build cohere_poison`)
// is built by no platform.
func PlatformFor(filename string) (Platform, bool) {
	for _, platform := range Platforms {
		context := build.Default
		context.GOOS = platform.OperatingSystem
		context.GOARCH = platform.Architecture
		context.CgoEnabled = false
		context.BuildTags = nil
		matches, matchError := context.MatchFile(filepath.Dir(filename), filepath.Base(filename))
		if matchError == nil && matches {
			return platform, true
		}
	}
	return Platform{}, false
}
