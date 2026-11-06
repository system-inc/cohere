import Foundation

/*
 The compiler's own errors and warnings for a SwiftPM package whose platforms leave out macOS, read from an
 xcodebuild of the package for the generic device of the first platform it names that this engine builds for.

 `swift build` builds for the macOS host at macOS's default deployment target, whatever the manifest names, so a
 package that names only iOS has its code judged against an SDK it never targets. Measured 2026-11-05 on a scratch
 package holding CapturePhone's `Task+SleepUnlessCancelled.swift`: "'Duration' is only available in macOS 13.0 or
 newer", a false type error that kept lint from running (#ee9ct1r).

 xcodebuild run in a package's folder builds the package as a workspace of its own. Measured 2026-11-06 on Xcode
 27.2, with the flags `XcodeTypesPhase` passes and `build-for-testing`, so the test targets compile too:

 - the libraries and tests compile for `arm64-apple-ios18.0` into the scratch's derived data, the Duration file
   comes back clean, and a type error planted in a test file is a `.dia` record at its line;
 - the records sit at `Build/Intermediates.noindex/<Package>.build/Debug-iphoneos/<Target>-{t,p}.build`, named as
   swiftbuild names them, so `TypesPhase.buildDirectories` finds them, and the task store and index store sit
   where `.xcode(derivedData:)` says, so the typed rules and `--unused` read them as at an Xcode root;
 - the scheme is named for the package when it has exactly one product, and `<Package>-Package` with none or with
   two. Write-ups for older Xcodes disagree, and `xcodebuild -list`, the only way to ask, writes into
   `~/Library/Developer/Xcode/DerivedData`, so the other name is tried when xcodebuild says the first is no
   scheme. A miss costs about a second;
 - the only writes into the package's folder are an empty `.swiftpm/xcode` and `.swiftpm/configuration`, which are
   removed again (`XcodeTypesPhase.removeFoldersMade`). Under `--no-fix` the build is held to the package's own
   Package.resolved (`-disableAutomaticPackageResolution`), measured to leave it byte-identical; without one, a
   package with a remote dependency fails to resolve and writes nothing, as `swift build` does, and the run says why.

 A package that names macOS, or names no platform, is `TypesPhase`'s, built for the host as it always was. One whose
 platforms name none this engine builds for (Mac Catalyst or DriverKit alone), or whose folder holds an Xcode project
 or workspace xcodebuild would build instead, is not built at all: every file is without a record, which makes the
 run incomplete, and the words say why. The root's schemes compile a local package's libraries but not its tests, so
 those test files are named the same way.
 */
struct XcodePackageTypesPhase {
    let package: PackageModel
    let files: [FileSet.OwnedFile]
    let scratchPath: URL
    let resolutionAllowed: Bool
    /* The toolchain `swift --version` named; an unknown one never reuses a build, as for a package. */
    let toolchain: String
    let runner: ProcessRunner

    /* A platform as SwiftPM names it, to the SDK a build for its generic device uses (`XcodeTypesPhase.platforms` names its destination). */
    static let sdkRoots = ["ios": "iphoneos", "tvos": "appletvos", "watchos": "watchos", "visionos": "xros"]

    /* Where one build goes: the SDK, which names its configuration folder, and the platform its destination names. */
    struct Destination: Equatable {
        var sdkRoot: String
        var platform: String
    }

    /* Whether the package's platforms leave out macOS: it names some, and macOS is not one of them. */
    static func leavesOutHost(_ package: PackageModel) -> Bool {
        !package.platforms.isEmpty && !package.platforms.contains("macos")
    }

    /* The first platform the package names that this engine builds for, or nil when it names none. */
    var destination: Destination? {
        package.platforms.lazy.compactMap { name -> Destination? in
            guard let sdkRoot = Self.sdkRoots[name], let platform = XcodeTypesPhase.platforms[sdkRoot] else {
                return nil
            }
            return Destination(sdkRoot: sdkRoot, platform: platform)
        }
        .first
    }

    var derivedData: URL {
        XcodeTypesPhase.derivedData(scratchPath: scratchPath)
    }

    var clonedPackages: URL {
        scratchPath.appendingPathComponent("xcode-packages", isDirectory: true)
    }

    var resolved: URL {
        package.root.appendingPathComponent("Package.resolved")
    }

    /* The scheme names to try, the one this Xcode was measured to make first (above). */
    var schemes: [String] {
        let named = [package.name, "\(package.name)-Package"]
        return package.productNames.count == 1 ? named : named.reversed()
    }

    /* An Xcode project or workspace in the package's folder, which an xcodebuild run there would build instead of the package. */
    var projectBeside: URL? {
        let entries = (try? FileManager.default.contentsOfDirectory(at: package.root, includingPropertiesForKeys: nil))
        return entries?.filter { ["xcodeproj", "xcworkspace"].contains($0.pathExtension) }.min { $0.path < $1.path }
    }

    /* Test files of a local package, which no scheme of the root's compiles. A file that names no package is the root's. */
    var localTestFiles: Set<String> {
        let rootPath = package.root.standardizedFileURL.path
        return Set(
            files.filter { file in
                file.targetKind == "test" && file.packageRoot.map { $0.standardizedFileURL.path != rootPath } ?? false
            }
            .map(\.url.path)
        )
    }

    func arguments(scheme: String, destination: Destination) -> [String] {
        var arguments = [
            "-scheme", scheme, "-destination", "generic/platform=\(destination.platform)", "-derivedDataPath",
            derivedData.path, "-clonedSourcePackagesDirPath", clonedPackages.path, "-configuration", "Debug",
        ]
        if !resolutionAllowed {
            arguments.append("-disableAutomaticPackageResolution")
        }
        return arguments + ["CODE_SIGNING_ALLOWED=NO", "ENABLE_USER_SCRIPT_SANDBOXING=YES", "build-for-testing"]
    }

    func run() throws -> TypesPhase.Result {
        let start = Date()
        let platforms = package.platforms.joined(separator: ", ")
        guard let destination else {
            return unbuilt(
                because:
                    "the package's platforms (\(platforms)) leave out macOS and name none this engine builds for, so no build this run makes targets it",
                start: start,
            )
        }
        if let projectBeside {
            return unbuilt(
                because:
                    "the package's platforms (\(platforms)) leave out macOS, and an xcodebuild in its folder would build \(projectBeside.lastPathComponent) instead of the package",
                start: start,
            )
        }
        let snapshot =
            toolchain.hasPrefix("unknown")
            ? nil
            : BuildInputSnapshot.take(
                roots: TypesPhase.inputRoots(of: package),
                resolved: resolved,
                toolchain: toolchain,
                arguments: schemes.flatMap { arguments(scheme: $0, destination: destination) },
            )
        let built =
            "xcodebuild of the package for \(destination.platform), the first platform it names, since it names no macOS"

        /* Nothing a build reads changed since the last build that succeeded, so its records are this build's records, as for a package. */
        if let snapshot, BuildInputSnapshot.stored(scratchPath: derivedData) == snapshot {
            let reused = try read(
                destination: destination,
                failure: nil,
                start: start,
                build:
                    "no input changed since the last successful xcodebuild, so its compiler records were read without building",
            )
            if Set(reused.record.filesWithoutRecord).isSubset(of: localTestFiles) {
                return reused
            }
        }

        let buildStart = Date()
        var scheme = ""
        var result: ProcessRunner.Result?
        for candidate in schemes {
            let attempt = try runner.run(
                "xcodebuild",
                arguments(scheme: candidate, destination: destination),
                in: package.root,
            )
            scheme = candidate
            result = attempt
            guard !attempt.succeeded, Self.isMissingScheme(attempt, candidate) else { break }
        }
        XcodeTypesPhase.removeFoldersMade(
            in: package.allPackages.flatMap { [$0.root] + $0.pathDependencyRoots }
                + XcodeTypesPhase.localPackages(stateIn: clonedPackages),
            since: buildStart,
        )
        guard let result else {
            throw TypesPhase.TypesFailure(description: "no scheme was tried for \(package.root.path)")
        }
        let failure = result.succeeded ? nil : result
        if let snapshot, failure == nil {
            snapshot.store(scratchPath: derivedData)
        }
        else {
            BuildInputSnapshot.forget(scratchPath: derivedData)
        }
        let directories = try targetDirectories(destination: destination)
        /* A build that left nothing failed before the compiler ran (a scheme, the packages), or wrote a layout this engine does not know. */
        guard !directories.isEmpty else {
            if let failure {
                throw TypesPhase.TypesFailure(
                    description:
                        "xcodebuild of \(package.root.path) for \(destination.platform) failed before compiling anything, so the compiler never ran: \(failedStep(of: failure))"
                )
            }
            throw TypesPhase.TypesFailure(
                description:
                    "the xcodebuild of \(package.root.path) left no target directories under \(intermediates.path), so this engine does not recognise its layout and cannot read what the compiler said (toolchain: \(toolchain))\n\(TypesPhase.tail(of: result))"
            )
        }
        return try read(
            destination: destination,
            failure: failure,
            start: start,
            build: "\(built), scheme \(scheme), derived data \(derivedData.path)",
        )
    }

    var intermediates: URL {
        derivedData.appendingPathComponent("Build/Intermediates.noindex", isDirectory: true)
    }

    func targetDirectories(destination: Destination) throws -> [(String, URL)] {
        try TypesPhase.buildDirectories(
            in: intermediates,
            configuration: "Debug-\(destination.sdkRoot)",
            package: package,
            files: files,
        )
    }

    /* xcodebuild's words when the scheme it was given is not one the package's workspace has. */
    static func isMissingScheme(_ result: ProcessRunner.Result, _ scheme: String) -> Bool {
        let text = String(decoding: result.standardOutput, as: UTF8.self) + result.standardError
        return text.contains("does not contain a scheme named \"\(scheme)\"")
    }

    /*
     What failed, in xcodebuild's words. Under `--no-fix` the build is held to the pins beside the manifest, so a
     resolve that failed names them, or, when there are none, says what SwiftPM's own build says of that.
     */
    func failedStep(of result: ProcessRunner.Result) -> String {
        let hasPins = FileManager.default.fileExists(atPath: resolved.path)
        let step = XcodeTypesPhase.failedStep(of: result, heldTo: !resolutionAllowed && hasPins ? resolved : nil)
        guard !resolutionAllowed, !hasPins, XcodeTypesPhase.isResolutionFailure(step) else { return step }
        return
            "\(step). Under --no-fix nothing is written into the tree, and a package's resolution is kept only in the Package.resolved beside its Package.swift, so a check cannot resolve this package: commit its Package.resolved, or run without --no-fix once"
    }

    /*
     The findings and the types record, judged by `CompilerRecords` as for any build. A failed build with no error in
     any file of ours failed where no file carries it, which is a finding at the manifest, as for a package.
     */
    func read(
        destination: Destination,
        failure: ProcessRunner.Result?,
        start: Date,
        build: String,
    ) throws -> TypesPhase.Result {
        let reading = try CompilerRecords(files: files, runner: runner).read(
            targetDirectories: try targetDirectories(destination: destination)
        )
        var findings = reading.findings
        var words = build
        if let failure {
            let step = failedStep(of: failure)
            words += "; the build failed: \(step)"
            if !findings.contains(where: { $0.severity == .error }) {
                findings.append(
                    FindingRecord(
                        source: .compiler,
                        file: package.root.appendingPathComponent("Package.swift").path,
                        line: 1,
                        column: 1,
                        severity: .error,
                        rule: "",
                        messageId: "buildFailed",
                        message:
                            "xcodebuild of the package for \(destination.platform) failed outside any file this run checks, so the package does not build: \(step)",
                    )
                )
            }
        }
        if !localTestFiles.isEmpty {
            words +=
                "; \(localTestFiles.count) test files of local packages were not built, because the root's schemes compile a local package's libraries and not its tests"
        }
        findings.sort { ($0.file, $0.line, $0.column) < ($1.file, $1.line, $1.column) }
        let record = TypesRecord(
            diagnostics: findings.count,
            files: files.count,
            elapsedMilliseconds: Pipeline.milliseconds(since: start),
            filesWithoutRecord: reading.filesWithoutRecord.sorted(),
            build: words,
        )
        return TypesPhase.Result(
            findings: findings,
            record: record,
            hasErrors: findings.contains { $0.severity == .error },
        )
    }

    /* No build at all: no finding, every file without a record, and why, so the run is incomplete and never reads as clean. */
    func unbuilt(because reason: String, start: Date) -> TypesPhase.Result {
        let record = TypesRecord(
            diagnostics: 0,
            files: files.count,
            elapsedMilliseconds: Pipeline.milliseconds(since: start),
            filesWithoutRecord: files.map(\.url.path).sorted(),
            build:
                "not built: \(reason), and the macOS host's SDK is never this package's judge, so no file's diagnostics are known",
        )
        return TypesPhase.Result(findings: [], record: record, hasErrors: false)
    }
}
