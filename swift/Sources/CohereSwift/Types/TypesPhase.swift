import Foundation

/*
 The compiler's own errors and warnings for every file we own, read from what the build recorded rather
 than from what it printed.

 The build runs into the engine's cache (`--scratch-path`), never the project's `.build`. It builds test
 targets too, because their files are ours and a test that does not compile is a finding like any other.
 Then every serialized diagnostics file in each owned target's build directories is read, each diagnostic
 is placed by its own file path, and duplicates are dropped: an executable target is compiled twice (once
 normally, once testable for its tests), and both copies report the same warning.

 A file is covered only when its own `.dia` exists and is newer than the source. A missing or older
 record means the compiler did not vouch for the file as it stands, usually because the build stopped
 before reaching it. That file is listed by name and the run is incomplete, never quietly clean.

 Measured on Swift 6.4's build system: records live at
 `out/Intermediates.noindex/<Package>.build/Debug/<Target>-{p,t}[...].build/Objects-normal/<arch>/<File>.dia`.
 A layout the phase cannot find any record in is a loud failure, because the build system moved its
 output once already (the index store went to `out/v5` in 6.4) and may move this too.
 */
struct TypesPhase {
    /* What the phase found and what it could not vouch for. */
    struct Result {
        var findings: [FindingRecord]
        var record: TypesRecord
        /* True when any compiler error was found: lint is then not reached. Warnings do not bail. */
        var hasErrors: Bool
    }

    let package: PackageModel
    let files: [FileSet.OwnedFile]
    let scratchPath: URL
    let resolutionAllowed: Bool
    let runner: ProcessRunner

    /*
     The packages to build: the root, plus each local package that holds test files of ours. Building the root
     compiles a local package's libraries but never its tests, because a dependency's test targets are not part
     of the root's graph. Measured on Presence: without these builds, RigSolve's, CaptureWire's and VRMKit's 26
     test files had no compiler record at all.
     */
    private var builds: [(root: URL, scratchPath: URL)] {
        let testTargetNames = Set(files.filter { $0.targetKind == "test" }.map(\.targetName))
        let locals = package.localPackages.filter { local in
            local.targets.contains { $0.kind == "test" && testTargetNames.contains($0.name) }
        }
        return [(package.root, scratchPath)] + locals.map { ($0.root, scratchPath.appendingPathComponent("local/\($0.root.lastPathComponent)", isDirectory: true)) }
    }

    func run() throws -> Result {
        let start = Date()
        var failedBuilds: [ProcessRunner.Result] = []
        var targetDirectories: [(String, URL)] = []
        for build in builds {
            var arguments = ["build", "--build-tests", "--scratch-path", build.scratchPath.path]
            if !resolutionAllowed {
                arguments.append("--disable-automatic-resolution")
            }
            let result = try runner.run("swift", arguments, in: build.root)
            if !result.succeeded {
                failedBuilds.append(result)
            }
            let directories = try buildDirectories(under: build.scratchPath)
            guard !directories.isEmpty else {
                throw TypesFailure(description: "the build of \(build.root.path) left no target directories under \(intermediates(of: build.scratchPath).path), so this engine does not recognise the build system's layout and cannot read what the compiler said\n\(Self.tail(of: result))")
            }
            targetDirectories.append(contentsOf: directories)
        }

        let reader = try SerializedDiagnosticsReader(libraryPath: SerializedDiagnosticsReader.toolchainLibraryPath(runner: runner))

        let ours = Dictionary(files.map { ($0.url.resolvingSymlinksInPath().path, $0) }, uniquingKeysWith: { first, _ in first })
        var seen = Set<String>()
        var findings: [FindingRecord] = []
        var recordFor: [String: Date] = [:]

        for (targetName, directory) in targetDirectories {
            for record in try diaFiles(in: directory) {
                let modified = try record.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate ?? .distantPast
                /* Keyed by target as well as name: two targets may each hold a `Utilities.swift`, and one must not vouch for the other. */
                let key = "\(targetName)/\(record.deletingPathExtension().lastPathComponent)"
                recordFor[key] = max(recordFor[key] ?? .distantPast, modified)
                for diagnostic in try reader.read(record) {
                    guard let severity = diagnostic.severity else { continue }
                    let file = URL(fileURLWithPath: diagnostic.file).resolvingSymlinksInPath().path
                    guard ours[file] != nil else { continue }
                    let identity = "\(file):\(diagnostic.line):\(diagnostic.column):\(diagnostic.message)"
                    guard seen.insert(identity).inserted else { continue }
                    findings.append(FindingRecord(
                        source: .compiler,
                        file: file,
                        line: max(diagnostic.line, 1),
                        column: max(diagnostic.column, 1),
                        severity: severity,
                        rule: diagnostic.group,
                        messageId: "",
                        message: diagnostic.message
                    ))
                }
            }
        }

        /* Coverage: each file's own record, by stem, newer than the source as it stands. */
        var withoutRecord: [String] = []
        for file in files {
            let key = "\(file.targetName)/\(file.url.deletingPathExtension().lastPathComponent)"
            let sourceModified = try file.url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate ?? .distantFuture
            guard let recorded = recordFor[key], recorded >= sourceModified else {
                withoutRecord.append(file.url.path)
                continue
            }
        }

        /* A failed build with no error in any file of ours failed somewhere no file carries: linking, a dependency, the manifest. Said, never swallowed. */
        if let failed = failedBuilds.first, !findings.contains(where: { $0.severity == .error }) {
            findings.append(FindingRecord(
                source: .compiler,
                file: package.root.appendingPathComponent("Package.swift").path,
                line: 1,
                column: 1,
                severity: .error,
                rule: "",
                messageId: "buildFailed",
                message: "swift build failed outside any file this run checks, so the package does not build: \(Self.tail(of: failed))"
            ))
        }

        findings.sort { ($0.file, $0.line, $0.column) < ($1.file, $1.line, $1.column) }
        let record = TypesRecord(
            diagnostics: findings.count,
            files: files.count,
            elapsedMilliseconds: Pipeline.milliseconds(since: start),
            filesWithoutRecord: withoutRecord.sorted(),
            build: "swift build --build-tests of \(builds.count) packages, scratch \(scratchPath.path)"
        )
        return Result(findings: findings, record: record, hasErrors: findings.contains { $0.severity == .error })
    }

    /* The build could not be read at all. */
    struct TypesFailure: Error, CustomStringConvertible {
        var description: String
    }

    private func intermediates(of scratchPath: URL) -> URL {
        scratchPath.appendingPathComponent("out/Intermediates.noindex", isDirectory: true)
    }

    /* Every build directory belonging to a target we own files in, with that target's name: `<Target>-p.build`, `<Target>-t.build`, and the testable variants. */
    private func buildDirectories(under scratchPath: URL) throws -> [(String, URL)] {
        let targetNames = Set(files.map(\.targetName))
        let manager = FileManager.default
        let intermediatesDirectory = intermediates(of: scratchPath)
        guard manager.fileExists(atPath: intermediatesDirectory.path) else { return [] }
        var directories: [(String, URL)] = []
        for packageDirectory in try manager.contentsOfDirectory(at: intermediatesDirectory, includingPropertiesForKeys: nil) {
            let configuration = packageDirectory.appendingPathComponent("Debug", isDirectory: true)
            guard manager.fileExists(atPath: configuration.path) else { continue }
            for targetDirectory in try manager.contentsOfDirectory(at: configuration, includingPropertiesForKeys: nil) {
                let name = targetDirectory.lastPathComponent
                guard name.hasSuffix(".build"), !name.contains("-product-") else { continue }
                /* The dash matters: `AhraOsServicesTests-p` must not count as a directory of `AhraOsServices`. */
                if let target = targetNames.first(where: { name.hasPrefix("\($0)-") }) {
                    directories.append((target, targetDirectory))
                }
            }
        }
        return directories
    }

    private func diaFiles(in targetDirectory: URL) throws -> [URL] {
        let objects = targetDirectory.appendingPathComponent("Objects-normal", isDirectory: true)
        guard FileManager.default.fileExists(atPath: objects.path) else { return [] }
        var records: [URL] = []
        for architecture in try FileManager.default.contentsOfDirectory(at: objects, includingPropertiesForKeys: nil) {
            records.append(contentsOf: try FileManager.default.contentsOfDirectory(at: architecture, includingPropertiesForKeys: nil).filter { $0.pathExtension == "dia" })
        }
        return records
    }

    /* The last lines of the build's own output, colours stripped, for a failure no file carries. */
    static func tail(of build: ProcessRunner.Result) -> String {
        let text = String(decoding: build.standardOutput, as: UTF8.self) + build.standardError
        let plain = text.replacingOccurrences(of: #"\u{1B}\[[0-9;]*m|\u{1B}\]8;;[^\u{1B}]*\u{1B}\\"#, with: "", options: .regularExpression)
        let errors = plain.split(separator: "\n").filter { $0.contains("error:") }
        return (errors.isEmpty ? plain.split(separator: "\n").suffix(5) : errors.prefix(5)).joined(separator: " / ")
    }
}
