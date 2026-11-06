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

 Packages resolve into the scratch, `--no-fix` too (#s8p20fe), since a scratch that may hold no resolution
 yet would otherwise fail the read-only gate until someone ran a mutating check. xcodebuild keeps a project's
 pins in the workspace it builds, so where they land follows the bundle: a generated bundle is in the scratch
 already, and a committed one is built under `--no-fix` through a workspace in the scratch that names it
 (`workspace`), measured 2026-11-05 to leave the bundle byte-identical while the pins went to the workspace.
 A resolution the repository commits (`committedResolved`) is preferred to any, and under `--no-fix` the build
 is held to it as it stands (`-disableAutomaticPackageResolution`), which on a fresh scratch still clones the
 pinned revisions into `clonedPackages`. A resolve that fails is a finding at the project's definition.

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
        Self.derivedData(scratchPath: scratchPath)
    }

    /* Where a build into this scratch keeps its derived data, so the typed rules can read the build this phase made. */
    static func derivedData(scratchPath: URL) -> URL {
        scratchPath.appendingPathComponent("xcode-build", isDirectory: true)
    }

    var clonedPackages: URL {
        scratchPath.appendingPathComponent("xcode-packages", isDirectory: true)
    }

    /*
     The pins inside the bundle, where xcodebuild keeps them when it builds the bundle as a project. A generated project's bundle is made afresh on
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

    /*
     A committed bundle under `--no-fix` is never handed to xcodebuild, because building it as a project writes its
     pins, and the empty folders for them, into the bundle. This workspace in the scratch names it instead.
     */
    var buildsThroughWorkspace: Bool {
        package.projectKind == .xcodeProject && !resolutionAllowed
    }

    var workspace: URL {
        scratchPath.appendingPathComponent(
            "xcode-workspace/\(project.bundle.deletingPathExtension().lastPathComponent).xcworkspace",
            isDirectory: true,
        )
    }

    /* The pins the build reads and writes: the workspace's when it builds through one, else the bundle's. */
    var resolvedInBuild: URL {
        buildsThroughWorkspace
            ? workspace.appendingPathComponent("xcshareddata/swiftpm/Package.resolved") : resolvedInBundle
    }

    /*
     The pins the repository commits for this project, or nil. A committed bundle's are its own; an xcodegen
     project's can only be the ones a repository keeps in the bundle beside the spec, which it otherwise ignores.
     Committed means git would list the file: an ignored one is a developer's own resolution, or the output of
     their last generation, and neither is the repository's word. Outside any repository a committed bundle's pins
     are part of the project as it stands, and a bundle beside a spec is the spec's output, so it is never read.
     */
    var committedResolved: URL? {
        let candidate: URL
        switch package.projectKind {
            case .xcodeProject:
                candidate = resolvedInBundle
            case .xcodeGen:
                candidate = package.root.appendingPathComponent(
                    "\(project.bundle.lastPathComponent)/project.xcworkspace/xcshareddata/swiftpm/Package.resolved"
                )
            case .swiftPackage:
                return nil
        }
        guard FileManager.default.fileExists(atPath: candidate.path) else { return nil }
        let resolved = candidate.resolvingSymlinksInPath()
        guard let repository = IgnoreRules.Repository.containing(resolved.deletingLastPathComponent()) else {
            return package.projectKind == .xcodeProject ? candidate : nil
        }
        return IgnoreRules(repository: repository).isVisible(resolved.path) ? candidate : nil
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
        invocation(of: build, configuration: "Debug") + [build.action]
    }

    /*
     One xcodebuild of a target's scheme in one configuration, everything but what it is asked to do: the build's
     action, or `-showBuildSettings` when `XcodeBuildSettings` asks what the build would compile with, which this
     shares so the settings read goes through the same project or workspace, scratch and resolution as the build.
     */
    func invocation(of build: Build, configuration: String) -> [String] {
        var arguments = buildsThroughWorkspace ? ["-workspace", workspace.path] : ["-project", project.bundle.path]
        arguments += [
            "-scheme", build.target, "-destination", "generic/platform=\(build.platform)", "-derivedDataPath",
            derivedData.path, "-clonedSourcePackagesDirPath", clonedPackages.path, "-configuration", configuration,
        ]
        if !resolutionAllowed && committedResolved != nil {
            arguments.append("-disableAutomaticPackageResolution")
        }
        return arguments + ["CODE_SIGNING_ALLOWED=NO", "ENABLE_USER_SCRIPT_SANDBOXING=YES"]
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
        if buildsThroughWorkspace {
            try writeWorkspace()
        }
        restoreResolved()
        let snapshot =
            toolchain.hasPrefix("unknown")
            ? nil
            : BuildInputSnapshot.take(
                roots: inputRoots,
                resolved: resolvedInBuild,
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

        let buildStart = Date()
        var failures: [(build: Build, result: ProcessRunner.Result)] = []
        for build in plan.builds {
            let result = try runner.run("xcodebuild", arguments(for: build), in: package.root)
            if !result.succeeded {
                failures.append((build, result))
            }
        }
        removeFoldersMadeInLocalPackages(since: buildStart)
        if package.projectKind == .xcodeGen {
            keepResolved()
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
            let step = failedStep(of: failure.result)
            findings.append(
                FindingRecord(
                    source: .compiler,
                    file: project.definition.path,
                    line: 1,
                    column: 1,
                    severity: .error,
                    rule: "",
                    messageId: "buildFailed",
                    message: Self.isResolutionFailure(step)
                        ? "xcodebuild could not resolve the packages this project names, so nothing was built and no file's types are known: \(step)"
                        : "xcodebuild of \(failure.build.target) failed outside any file this run checks, so the project does not build: \(step)",
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
     `--no-fix` a committed resolution the project no longer matches is named, since the build was held to it.
     */
    func failedStep(of result: ProcessRunner.Result) -> String {
        Self.failedStep(of: result, heldTo: resolutionAllowed ? nil : committedResolved)
    }

    /*
     What failed, in xcodebuild's words, for any xcodebuild this engine runs: a project's (above), or a package's built
     for a device platform (`XcodePackageTypesPhase`). `heldTo` is the resolution a `--no-fix` build was held to, if any.
     */
    static func failedStep(of result: ProcessRunner.Result, heldTo committed: URL?) -> String {
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
            guard isResolutionFailure(said) else { return said }
            if let committed {
                return
                    "\(said) (under --no-fix the build is held to the resolution the repository commits, \(committed.path), and a check never moves it)"
            }
            /* Its error may name only a cache folder (measured for a host that does not resolve), so the packages it was fetching are named too. */
            var fetched: [String] = []
            for line in lines where line.hasPrefix("Fetching from ") {
                let location = String(line.dropFirst("Fetching from ".count)).trimmingCharacters(in: .whitespaces)
                if !fetched.contains(location) {
                    fetched.append(location)
                }
            }
            return fetched.isEmpty ? said : "\(said) (while fetching \(fetched.joined(separator: ", ")))"
        }
        return TypesPhase.tail(of: result)
    }

    /* xcodebuild's words when it stopped before building because the packages would not resolve. */
    static func isResolutionFailure(_ said: String) -> Bool {
        said.contains("Could not resolve package dependencies")
    }

    /*
     The pins the build starts from. The repository's own replace whatever the build holds, so the build pins what
     the repository pins. Without them a generated bundle gets the last build's copy back unless it has pins
     already, a workspace keeps what its last build wrote, and a committed bundle built as a project holds its own.
     */
    func restoreResolved() {
        let source: URL
        if let committed = committedResolved {
            source = committed
        }
        else if package.projectKind == .xcodeGen && !FileManager.default.fileExists(atPath: resolvedInBuild.path) {
            source = keptResolved
        }
        else {
            return
        }
        guard source.standardizedFileURL != resolvedInBuild.standardizedFileURL,
            let pins = try? Data(contentsOf: source)
        else { return }
        do {
            try FileManager.default.createDirectory(
                at: resolvedInBuild.deletingLastPathComponent(),
                withIntermediateDirectories: true,
            )
            try pins.write(to: resolvedInBuild, options: .atomic)
        }
        catch {
            /* Without them the build resolves, or under `--no-fix` says it could not; either way the run says what happened. */
        }
    }

    /* The workspace naming the committed bundle by its absolute path, rewritten only when that changed, so a build's reuse is not disturbed. */
    func writeWorkspace() throws {
        let location = project.bundle.path.replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "\"", with: "&quot;").replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
        let contents = Data(
            """
            <?xml version="1.0" encoding="UTF-8"?>
            <Workspace
               version = "1.0">
               <FileRef
                  location = "absolute:\(location)">
               </FileRef>
            </Workspace>

            """.utf8
        )
        let file = workspace.appendingPathComponent("contents.xcworkspacedata")
        guard (try? Data(contentsOf: file)) != contents else { return }
        try FileManager.default.createDirectory(at: workspace, withIntermediateDirectories: true)
        try contents.write(to: file, options: .atomic)
    }

    /*
     xcodebuild makes an empty `.swiftpm/xcode` inside every local package a build links, whether it resolves or not
     (measured 2026-11-05, on a path package beside an app). A check writes nothing into the developer's tree, so
     each of those folders that this build made is removed again (`removeFoldersMade`). The local packages are read
     after the build, so the ones its package state names for the first time are cleaned as well.
     */
    func removeFoldersMadeInLocalPackages(since buildStart: Date) {
        Self.removeFoldersMade(
            in: project.projectFile.localPackages + Self.localPackages(stateIn: clonedPackages),
            since: buildStart,
        )
    }

    /*
     The folders an xcodebuild makes in a package it reads, removed from each of these roots when empty and created
     since the build started, innermost first: `.swiftpm/xcode`, `.swiftpm/configuration`, then `.swiftpm`. The
     configuration folder was measured 2026-11-06, made beside `xcode` in a package xcodebuild built as the root. One
     holding anything, or older than the build, is not ours to judge and stays.
     */
    static func removeFoldersMade(in roots: [URL], since buildStart: Date) {
        for root in roots {
            let swiftpm = root.appendingPathComponent(".swiftpm", isDirectory: true)
            let made = [
                swiftpm.appendingPathComponent("xcode", isDirectory: true),
                swiftpm.appendingPathComponent("configuration", isDirectory: true), swiftpm,
            ]
            for directory in made {
                guard let entries = try? FileManager.default.contentsOfDirectory(atPath: directory.path),
                    entries.isEmpty,
                    let created = try? FileManager.default.attributesOfItem(atPath: directory.path)[.creationDate]
                        as? Date,
                    created >= buildStart
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
}
