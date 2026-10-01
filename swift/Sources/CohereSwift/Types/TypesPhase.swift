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

    func run() throws -> Result {
        let start = Date()
        var arguments = ["build", "--build-tests", "--scratch-path", scratchPath.path]
        if !resolutionAllowed {
            arguments.append("--disable-automatic-resolution")
        }
        let build = try runner.run("swift", arguments, in: package.root)

        let reader = try SerializedDiagnosticsReader(libraryPath: SerializedDiagnosticsReader.toolchainLibraryPath(runner: runner))
        let targetDirectories = try buildDirectories()
        guard !targetDirectories.isEmpty || files.isEmpty else {
            throw TypesFailure(description: "the build left no target directories under \(intermediatesDirectory.path), so this engine does not recognise the build system's layout and cannot read what the compiler said\n\(Self.tail(of: build))")
        }

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
        if !build.succeeded && !findings.contains(where: { $0.severity == .error }) {
            findings.append(FindingRecord(
                source: .compiler,
                file: package.root.appendingPathComponent("Package.swift").path,
                line: 1,
                column: 1,
                severity: .error,
                rule: "",
                messageId: "buildFailed",
                message: "swift build failed outside any file this run checks, so the package does not build: \(Self.tail(of: build))"
            ))
        }

        findings.sort { ($0.file, $0.line, $0.column) < ($1.file, $1.line, $1.column) }
        let record = TypesRecord(
            diagnostics: findings.count,
            files: files.count,
            elapsedMilliseconds: Pipeline.milliseconds(since: start),
            filesWithoutRecord: withoutRecord.sorted(),
            build: "swift build --build-tests, scratch \(scratchPath.path)"
        )
        return Result(findings: findings, record: record, hasErrors: findings.contains { $0.severity == .error })
    }

    /* The build could not be read at all. */
    struct TypesFailure: Error, CustomStringConvertible {
        var description: String
    }

    private var intermediatesDirectory: URL {
        scratchPath.appendingPathComponent("out/Intermediates.noindex", isDirectory: true)
    }

    /* Every build directory belonging to a target we own files in, with that target's name: `<Target>-p.build`, `<Target>-t.build`, and the testable variants. */
    private func buildDirectories() throws -> [(String, URL)] {
        let targetNames = Set(files.map(\.targetName))
        let manager = FileManager.default
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
