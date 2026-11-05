import Foundation

/*
 The compiler's own errors and warnings for every file an Xcode project's targets compile, read from what an
 xcodebuild build recorded, the way `TypesPhase` reads a SwiftPM build's.

 Each native target that owns a file of ours is built through the scheme Xcode makes for it, named for the
 target, for the generic device of the platform its `SDKROOT` names: `build`, or `build-for-testing` for a test
 bundle, whose files compile only for testing. Every write goes to the engine's scratch, measured 2026-11-05 on
 copies of ahraos-presence: `-derivedDataPath` holds the build, its records and its index, and
 `-clonedSourcePackagesDirPath` the packages it fetched. Two other ways of asking wrote into
 `~/Library/Developer/Xcode/DerivedData` (`-list`, and `-target` with `SYMROOT` and `OBJROOT`), which is why the
 schemes are named rather than listed. The device platform, never the simulator: a generic simulator build adds
 x86_64, and a binary package with an arm64-only simulator slice (MediaPipe) then fails to link.

 A check runs no project code on the developer's machine. Signing is off (`CODE_SIGNING_ALLOWED=NO`), and
 `ENABLE_USER_SCRIPT_SANDBOXING=YES` holds every run-script phase to the inputs and outputs it declares.

 What the records say is judged by `CompilerRecords`, as for a package: a record older than its file vouches for
 nothing, and an owned file without a fresh record is named in `filesWithoutRecord`, which makes the run
 incomplete. A target this phase could not build (its `SDKROOT` names no platform it knows) is read from no
 record at all, so its files are named the same way and the words say why.
 */
struct XcodeTypesPhase {
    let package: PackageModel
    let project: XcodeProject.Loaded
    let files: [FileSet.OwnedFile]
    let scratchPath: URL
    let resolutionAllowed: Bool
    /* The toolchain `swift --version` named; an unknown one never reuses a build, as for a package. */
    let toolchain: String
    let runner: ProcessRunner

    /* `SDKROOT` to the platform `-destination generic/platform=` names. */
    static let platforms = [
        "iphoneos": "iOS", "macosx": "macOS", "appletvos": "tvOS", "watchos": "watchOS", "xros": "visionOS",
    ]

    /* One xcodebuild of one target's scheme. */
    struct Build {
        var target: String
        var sdkRoot: String
        var platform: String
        var action: String
    }

    /* A target of ours this phase does not build, with why, in the words the types record prints. */
    struct Unbuilt {
        var target: String
        var reason: String
    }

    var derivedData: URL {
        scratchPath.appendingPathComponent("xcode-build", isDirectory: true)
    }

    var clonedPackages: URL {
        scratchPath.appendingPathComponent("xcode-packages", isDirectory: true)
    }

    /*
     Where xcodebuild reads and writes the project's package pins. A generated project's bundle is made afresh on
     every run (`XcodeGeneration`), so its pins are kept beside the build (`keptResolved`) and put back before
     building: without them a plain run resolves every package again, which may move a version a range allows,
     and the build-input snapshot below would have no pins to compare.
     */
    var resolvedInBundle: URL {
        project.bundle.appendingPathComponent("project.xcworkspace/xcshareddata/swiftpm/Package.resolved")
    }

    var keptResolved: URL {
        scratchPath.appendingPathComponent("xcode-Package.resolved")
    }

    /* The targets to build, application and library targets before the test bundles that host in them, and the ones that cannot be. */
    var buildPlan: (builds: [Build], unbuilt: [Unbuilt]) {
        let owned = Set(files.map(\.targetName))
        var builds: [Build] = []
        var unbuilt: [Unbuilt] = []
        for target in project.projectFile.targets where owned.contains(target.name) {
            guard let sdkRoot = target.sdkRoot, !sdkRoot.isEmpty else {
                unbuilt.append(
                    Unbuilt(target: target.name, reason: "it sets no SDKROOT, so there is no platform to build it for")
                )
                continue
            }
            guard let platform = Self.platforms[sdkRoot] else {
                unbuilt.append(
                    Unbuilt(
                        target: target.name,
                        reason: "its SDKROOT \(sdkRoot) names no platform this engine builds for",
                    )
                )
                continue
            }
            let isTest = XcodeProject.kind(of: target.productType) == "test"
            builds.append(
                Build(
                    target: target.name,
                    sdkRoot: sdkRoot,
                    platform: platform,
                    action: isTest ? "build-for-testing" : "build",
                )
            )
        }
        let ordered = builds.sorted {
            ($0.action == "build" ? 0 : 1, $0.target) < ($1.action == "build" ? 0 : 1, $1.target)
        }
        return (ordered, unbuilt)
    }

    func arguments(for build: Build) -> [String] {
        var arguments = [
            "-project", project.bundle.path, "-scheme", build.target, "-destination",
            "generic/platform=\(build.platform)", "-derivedDataPath", derivedData.path, "-clonedSourcePackagesDirPath",
            clonedPackages.path, "-configuration", "Debug",
        ]
        if !resolutionAllowed {
            arguments.append("-disableAutomaticPackageResolution")
        }
        return arguments + ["CODE_SIGNING_ALLOWED=NO", "ENABLE_USER_SCRIPT_SANDBOXING=YES", build.action]
    }

    /*
     The project's folder and every local package a build of it reads: the ones its project file links, and the
     ones those link in turn, which only the last build's package state names (CapturePhone links CaptureWire,
     which links NumberText). A package first seen by a build is walked from the run after it, which then builds
     once more, since the walk it compares against did not hold it.
     */
    var inputRoots: [URL] {
        let roots = [package.root] + project.projectFile.localPackages + Self.localPackages(stateIn: clonedPackages)
        var seen = Set<String>()
        return roots.filter { root in
            !roots.contains { other in PackageModel.isInside(root, other) }
                && seen.insert(root.resolvingSymlinksInPath().path).inserted
        }
    }

    /* The file-system packages in the package state xcodebuild keeps beside the packages it cloned. None when it has not written one. */
    static func localPackages(stateIn clonedPackages: URL) -> [URL] {
        guard let data = try? Data(contentsOf: clonedPackages.appendingPathComponent("workspace-state.json")),
            let state = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
            let object = state["object"] as? [String: Any],
            let dependencies = object["dependencies"] as? [[String: Any]]
        else { return [] }
        return dependencies.compactMap { dependency in
            guard let reference = dependency["packageRef"] as? [String: Any],
                ["fileSystem", "local"].contains(reference["kind"] as? String),
                let location = reference["location"] as? String
            else { return nil }
            return URL(fileURLWithPath: location, isDirectory: true).standardizedFileURL
        }
    }

    func run() throws -> TypesPhase.Result {
        let start = Date()
        let plan = buildPlan
        let unbuiltFiles = Set(
            files.filter { file in plan.unbuilt.contains { $0.target == file.targetName } }.map(\.url.path)
        )
        if package.projectKind == .xcodeGen {
            restoreResolved()
        }
        let snapshot =
            toolchain.hasPrefix("unknown")
            ? nil
            : BuildInputSnapshot.take(
                roots: inputRoots,
                resolved: resolvedInBundle,
                toolchain: toolchain,
                arguments: plan.builds.flatMap(arguments(for:)),
                contents: [project.bundle.appendingPathComponent("project.pbxproj")],
            )

        /*
         Nothing a build reads changed since the last build that succeeded, so its records are this build's records,
         kept only if every file it built has one newer than its source, as for a package.
         */
        if let snapshot, BuildInputSnapshot.stored(scratchPath: derivedData) == snapshot {
            let reused = try read(
                builds: plan.builds,
                unbuilt: plan.unbuilt,
                failures: [],
                start: start,
                build:
                    "no input changed since the last successful xcodebuild, so its compiler records were read without building",
            )
            if Set(reused.record.filesWithoutRecord).isSubset(of: unbuiltFiles) {
                return reused
            }
        }

        let createdInBundle = directoriesXcodebuildMayCreate.filter {
            !FileManager.default.fileExists(atPath: $0.path)
        }
        var failures: [(build: Build, result: ProcessRunner.Result)] = []
        for build in plan.builds {
            let result = try runner.run("xcodebuild", arguments(for: build), in: package.root)
            if !result.succeeded {
                failures.append((build, result))
            }
        }
        if package.projectKind == .xcodeGen {
            keepResolved()
        }
        else if !resolutionAllowed {
            removeEmpty(createdInBundle)
        }
        if let snapshot, failures.isEmpty {
            snapshot.store(scratchPath: derivedData)
        }
        else {
            BuildInputSnapshot.forget(scratchPath: derivedData)
        }
        let platforms = Set(plan.builds.map(\.platform)).sorted().joined(separator: " and ")
        return try read(
            builds: plan.builds,
            unbuilt: plan.unbuilt,
            failures: failures,
            start: start,
            build:
                "xcodebuild of \(plan.builds.count) targets for \(platforms.isEmpty ? "no platform" : platforms), derived data \(derivedData.path)",
        )
    }

    /* Each built target's build directory: `Build/Intermediates.noindex/<Project>.build/Debug-<sdk>/<Target>.build`, where macOS has no suffix. */
    func targetDirectories(builds: [Build]) -> [(String, URL)] {
        let projectDirectory = derivedData.appendingPathComponent(
            "Build/Intermediates.noindex/\(project.bundle.deletingPathExtension().lastPathComponent).build",
            isDirectory: true,
        )
        var seen = Set<String>()
        return builds.compactMap { build in
            guard seen.insert(build.target).inserted else { return nil }
            let configuration = build.sdkRoot == "macosx" ? "Debug" : "Debug-\(build.sdkRoot)"
            let directory = projectDirectory.appendingPathComponent(
                "\(configuration)/\(build.target).build",
                isDirectory: true,
            )
            return FileManager.default.fileExists(atPath: directory.path) ? (build.target, directory) : nil
        }
    }

    /*
     The findings and the types record. A failed build is judged like a package's: every file with a fresh record
     is read, and a failure no file of ours carries an error for (linking, signing, a resource, the packages) is a
     finding at the project's definition, with the failed step in the record's words. A build that left no record
     is never read as clean: its files are without one, and the words say what xcodebuild said.
     */
    func read(
        builds: [Build],
        unbuilt: [Unbuilt],
        failures: [(build: Build, result: ProcessRunner.Result)],
        start: Date,
        build: String,
    ) throws -> TypesPhase.Result {
        let directories = targetDirectories(builds: builds)
        let reading = try CompilerRecords(files: files, runner: runner).read(targetDirectories: directories)
        var findings = reading.findings
        var words = build
        for failure in failures {
            words += "; the build of \(failure.build.target) failed: \(failedStep(of: failure.result))"
        }
        if let failure = failures.first, !findings.contains(where: { $0.severity == .error }) {
            findings.append(
                FindingRecord(
                    source: .compiler,
                    file: project.definition.path,
                    line: 1,
                    column: 1,
                    severity: .error,
                    rule: "",
                    messageId: "buildFailed",
                    message:
                        "xcodebuild of \(failure.build.target) failed outside any file this run checks, so the project does not build: \(failedStep(of: failure.result))",
                )
            )
        }
        if directories.isEmpty && !builds.isEmpty {
            words +=
                "; no build left a compiler record under \(derivedData.appendingPathComponent("Build/Intermediates.noindex").path), so no file's diagnostics are known"
        }
        for target in unbuilt {
            words += "; \(target.target) was not built, because \(target.reason)"
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

    /*
     What failed, in xcodebuild's words: the build commands it lists as failed, or, when it stopped before building
     (a scheme it could not find, packages it could not resolve), its error and the lines that explain it. Under
     `--no-fix` a resolution that needs the network says so, since that is the one a plain run repairs.
     */
    func failedStep(of result: ProcessRunner.Result) -> String {
        let text = String(decoding: result.standardOutput, as: UTF8.self) + "\n" + result.standardError
        let lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        if let header = lines.firstIndex(where: { $0.hasPrefix("The following build commands failed:") }) {
            let commands = lines[(header + 1)...].prefix { $0.hasPrefix("\t") }
                .map { $0.trimmingCharacters(in: .whitespaces) }
            if !commands.isEmpty {
                return commands.prefix(5).joined(separator: " / ")
            }
        }
        if let error = lines.firstIndex(where: { $0.hasPrefix("xcodebuild: error:") }) {
            let explained = [lines[error]] + lines[(error + 1)...].prefix { $0.hasPrefix(" ") }
            let said = explained.map { $0.trimmingCharacters(in: .whitespaces) }.joined(separator: " ")
            if !resolutionAllowed && said.contains("Could not resolve package dependencies") {
                return
                    "\(said) (under --no-fix packages are never resolved, and this scratch holds no resolution to build from; a run without --no-fix resolves them once)"
            }
            return said
        }
        return TypesPhase.tail(of: result)
    }

    /* A generated bundle's pins, put back from the last build's copy. A bundle that already has pins keeps them. */
    func restoreResolved() {
        guard FileManager.default.fileExists(atPath: keptResolved.path),
            !FileManager.default.fileExists(atPath: resolvedInBundle.path)
        else { return }
        do {
            try FileManager.default.createDirectory(
                at: resolvedInBundle.deletingLastPathComponent(),
                withIntermediateDirectories: true,
            )
            try FileManager.default.copyItem(at: keptResolved, to: resolvedInBundle)
        }
        catch {
            /* Without them the build resolves, or under `--no-fix` says it could not; either way the run says what happened. */
        }
    }

    /* The pins the build just used, kept for the next run's freshly generated bundle. */
    func keepResolved() {
        guard let pins = try? Data(contentsOf: resolvedInBundle) else { return }
        do {
            try pins.write(to: keptResolved, options: .atomic)
        }
        catch {
            /* Not kept, so the next run resolves again: slower, and still right. */
        }
    }

    /*
     The folders xcodebuild makes inside a committed bundle even when it resolves nothing, outermost first:
     measured, a `--no-fix` build of a project with no packages left `project.xcworkspace/xcshareddata/swiftpm/configuration`,
     empty. A check writes nothing into the developer's tree, so those it made are removed again (`removeEmpty`).
     */
    var directoriesXcodebuildMayCreate: [URL] {
        [
            "project.xcworkspace", "project.xcworkspace/xcshareddata", "project.xcworkspace/xcshareddata/swiftpm",
            "project.xcworkspace/xcshareddata/swiftpm/configuration",
        ]
        .map { project.bundle.appendingPathComponent($0, isDirectory: true) }
    }

    /* Removes each of these folders that is empty now, innermost first. A folder holding anything, which then is not ours to judge, stays. */
    func removeEmpty(_ directories: [URL]) {
        for directory in directories.reversed() {
            guard let entries = try? FileManager.default.contentsOfDirectory(atPath: directory.path), entries.isEmpty
            else { continue }
            do {
                try FileManager.default.removeItem(at: directory)
            }
            catch {
                /* An empty folder left behind holds no file; the run's answer stands. */
            }
        }
    }
}
