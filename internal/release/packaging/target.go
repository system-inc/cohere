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

// IsMacOs reports whether this target needs signing and notarization.
//
// Gatekeeper blocks an unsigned binary downloaded from the network, and an npm tarball counts. It
// does not block one built locally, which is exactly why this is easy to miss: it works on the
// machine that built it and fails on every machine that installs it.
func (target Target) IsMacOs() bool {
	return target.GoOperatingSystem == "darwin"
}
