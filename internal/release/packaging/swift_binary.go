package release

// SwiftBinaryFileName is cohere-swift's executable in a platform package, in `bin/` beside cohere's
// own. The release puts it there and a released cohere looks for it there and nowhere else, so both read
// this one spelling.
const SwiftBinaryFileName = "cohere-swift"

// SwiftReleaseStampFileName is the generated file that carries the release's commit into cohere-swift,
// beside ProgramVersion.swift, which reads it under COHERE_RELEASE_STAMP. swift_language/.gitignore names it.
const SwiftReleaseStampFileName = "ReleaseStamp.generated.swift"

// ShipsSwiftBinary reports whether a platform's package carries cohere-swift.
//
// macOS only. cohere-swift links the toolchain's libraries and reads its compiler's serialized
// diagnostics, so it ships where Xcode does. On every other platform cohere refuses a Swift package by
// name rather than checking nothing, and the release stages no binary there.
func ShipsSwiftBinary(operatingSystem string) bool {
	return operatingSystem == "darwin"
}
