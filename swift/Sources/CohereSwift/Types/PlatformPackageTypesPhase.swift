import Foundation

/*
 The compiler's own errors and warnings for a SwiftPM package whose platforms leave out macOS, from a check for the
 first platform it names that this engine checks for, never against the macOS host.

 `swift build` and a build server started plainly target the macOS host at macOS's default deployment target,
 whatever the manifest names, so a package that names only iOS has its code judged against an SDK it never targets.
 Measured 2026-11-05 on a scratch package holding CapturePhone's `Task+SleepUnlessCancelled.swift`: "'Duration' is
 only available in macOS 13.0 or newer", a false type error that kept lint from running (#ee9ct1r).

 So the check (`TypesPhase`) is started with `--triple` and `--sdk` for that platform (#11fs5xp), in place of the
 xcodebuild this phase ran before. Measured 2026-11-06 on Swift 6.4.2: `--triple arm64-apple-ios` with the iPhoneOS
 SDK gives every target `-target arm64-apple-ios18.0`, the package's own deployment target, so the triple names only
 the architecture and the system. Everything else is a package's check: the scratch, the records, the index the
 typed rules and `--unused` read, and a local package's tests, checked in their own session as for any package.

 A package that names macOS, or names no platform, is checked for the host. One whose platforms name none this
 engine checks for (Mac Catalyst or DriverKit alone), or whose platform's SDK is not installed, is not checked at
 all: every file is without a record, which makes the run incomplete, and the words say why.
 */
struct PlatformPackageTypesPhase {
    let package: PackageModel
    let files: [FileSet.OwnedFile]
    let scratchPath: URL
    let resolutionAllowed: Bool
    /* The toolchain `swift --version` named; an unknown one never reuses a check, as for a package. */
    let toolchain: String
    let runner: ProcessRunner

    /* A platform as SwiftPM names it, to the SDK its device uses (`XcodeTypesPhase.platforms` names its destination). */
    static let sdkRoots = ["ios": "iphoneos", "tvos": "appletvos", "watchos": "watchos", "visionos": "xros"]

    /* Each device SDK to the system its triple names. */
    static let tripleSystems = ["iphoneos": "ios", "appletvos": "tvos", "watchos": "watchos", "xros": "xros"]

    /* Where one check goes: the SDK, the platform the words name, and the triple the build server is given. */
    struct Destination: Equatable {
        var sdkRoot: String
        var platform: String

        var triple: String { "arm64-apple-\(PlatformPackageTypesPhase.tripleSystems[sdkRoot] ?? sdkRoot)" }
    }

    /* Whether the package's platforms leave out macOS: it names some, and macOS is not one of them. */
    static func leavesOutHost(_ package: PackageModel) -> Bool {
        !package.platforms.isEmpty && !package.platforms.contains("macos")
    }

    /* The first platform the package names that this engine checks for, or nil when it names none. */
    var destination: Destination? {
        package.platforms.lazy.compactMap { name -> Destination? in
            guard let sdkRoot = Self.sdkRoots[name], let platform = XcodeTypesPhase.platforms[sdkRoot] else {
                return nil
            }
            return Destination(sdkRoot: sdkRoot, platform: platform)
        }
        .first
    }

    func run() throws -> TypesPhase.Result {
        let start = Date()
        let platforms = package.platforms.joined(separator: ", ")
        guard let destination else {
            return unchecked(
                because:
                    "the package's platforms (\(platforms)) leave out macOS and name none this engine checks for, so no check this run makes targets it",
                start: start,
            )
        }
        let sdk = try runner.run("xcrun", ["--sdk", destination.sdkRoot, "--show-sdk-path"], in: package.root)
        let sdkPath = String(decoding: sdk.standardOutput, as: UTF8.self).trimmingCharacters(
            in: .whitespacesAndNewlines
        )
        guard sdk.succeeded, !sdkPath.isEmpty else {
            return unchecked(
                because:
                    "the package's platforms (\(platforms)) leave out macOS, and this Xcode has no \(destination.sdkRoot) SDK to check it for \(destination.platform)",
                start: start,
            )
        }
        var result = try TypesPhase(
            package: package,
            files: files,
            scratchPath: scratchPath,
            resolutionAllowed: resolutionAllowed,
            toolchain: toolchain,
            runner: runner,
            platform: TypesPhase.Platform(triple: destination.triple, sdk: sdkPath),
        )
        .run()
        result.record.build =
            "for \(destination.platform), the first platform the package names, since it names no macOS: \(result.record.build)"
        return result
    }

    /* No check at all: no finding, every file without a record, and why, so the run is incomplete and never reads as clean. */
    func unchecked(because reason: String, start: Date) -> TypesPhase.Result {
        let record = TypesRecord(
            diagnostics: 0,
            files: files.count,
            elapsedMilliseconds: Pipeline.milliseconds(since: start),
            filesWithoutRecord: files.map(\.url.path).sorted(),
            build:
                "not checked: \(reason), and the macOS host's SDK is never this package's judge, so no file's diagnostics are known",
        )
        return TypesPhase.Result(findings: [], record: record, hasErrors: false)
    }
}
