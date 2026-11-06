package release

// Targets is every platform a release ships.
//
// The list is explicit rather than derived from `go tool dist list`, because shipping is a promise:
// a platform in this list has a published package and a machine that installs it gets a working
// binary. Deriving the list would silently grow it to platforms nobody has ever run, and a package
// that exists but was never exercised is worse than an honest "no binary for this platform": it
// converts a clear failure at install time into an obscure one at runtime.
//
// Windows on arm64 is included because Go cross-compiles it for free from the same matrix, and a
// developer on that hardware otherwise gets an error naming a platform we could trivially have
// built. Linux arm64 is here for CI runners and containers, which are increasingly arm.
var Targets = []Target{
	{GoOperatingSystem: "darwin", GoArchitecture: "arm64"},
	{GoOperatingSystem: "darwin", GoArchitecture: "amd64"},
	{GoOperatingSystem: "linux", GoArchitecture: "arm64"},
	{GoOperatingSystem: "linux", GoArchitecture: "amd64"},
	{GoOperatingSystem: "windows", GoArchitecture: "arm64"},
	{GoOperatingSystem: "windows", GoArchitecture: "amd64"},
}
