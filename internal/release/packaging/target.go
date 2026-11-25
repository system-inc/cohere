package release

import "strings"

// Target is one platform a release ships a binary for.
type Target struct {
	// GoOperatingSystem and GoArchitecture are what `go build` is asked for, as `GOOS` and `GOARCH`.
	GoOperatingSystem string
	GoArchitecture    string
}

// PackageName is the npm package this target publishes as.
func (target Target) PackageName() string {
	return PlatformPackageName(target.GoOperatingSystem, target.GoArchitecture)
}

// DirectoryName is the on-disk directory for this target's package, like "cohere-darwin-arm64".
//
// It is the package name without the scope, so that a staged release directory reads the same way
// `node_modules/@system-inc/` does, the dispatcher's "cohere" beside each platform's.
func (target Target) DirectoryName() string {
	return strings.TrimPrefix(target.PackageName(), PackageScope+"/")
}

// BinaryFileName is the executable's name inside this target's package.
func (target Target) BinaryFileName() string {
	return BinaryFileName(target.GoOperatingSystem)
}

// String names the target the way Go does, for build output and error messages.
func (target Target) String() string {
	return target.GoOperatingSystem + "/" + target.GoArchitecture
}

// GoArm64Level is the arm64 feature level this target compiles for, as `GOARM64`, or "" for Go's default, v8.0.
//
// v8.4 on darwin/arm64 alone: every Apple silicon Mac implements it, and it took 0.34 user-s off cohere-go's
// bar row at cohere's root on Shackler, findings identical (#b2t9xmf, #r6sqd2c). Linux and Windows on arm64 run
// on v8.0 cores still in service, a Raspberry Pi 4 and the first Graviton among them, where a v8.4 binary would
// die on an illegal instruction, so they keep the default.
func (target Target) GoArm64Level() string {
	if target.GoOperatingSystem == "darwin" && target.GoArchitecture == "arm64" {
		return "v8.4"
	}
	return ""
}

// IsMacOs reports whether this target needs signing and notarization.
//
// Gatekeeper blocks an unsigned binary downloaded from the network, and an npm tarball counts. It
// does not block one built locally, which is exactly why this is easy to miss: it works on the
// machine that built it and fails on every machine that installs it.
func (target Target) IsMacOs() bool {
	return target.GoOperatingSystem == "darwin"
}
