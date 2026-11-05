import Foundation

/*
 What the compiler's serialized diagnostics say about the files a run owns, and which of those files they leave
 unvouched for. Shared by the two builds the types phase knows: SwiftPM's (`TypesPhase`) and xcodebuild's
 (`XcodeTypesPhase`). Each finds its own build's target directories, since the two lay them out differently, and
 both judge what is in them by the same rules, so a record means the same thing whichever build wrote it.
 */
struct CompilerRecords {
    /* The diagnostics in files we own, in record order, and the owned files no fresh record covers, by path. */
    struct Reading {
        var findings: [FindingRecord]
        var filesWithoutRecord: [String]
    }

    let files: [FileSet.OwnedFile]
    let runner: ProcessRunner

    /*
     Every owned file's diagnostics from the records under these target directories, and which files have none.

     A diagnostic counts only when its record is at least as new as the file it names. A record older than that
     file's text describes text that has changed since, so its diagnostics about that file are gone, even though
     the record is still on disk. This happens two ways. A file's own record goes stale when the build stopped
     before recompiling it. And a batch-mode compile files one file's diagnostics under another's `.dia`, so a
     record that is never rewritten keeps another file's old errors at old lines after that file is fixed. Both
     were seen on Presence (2026-11-04, PoseSearchTests.dia holding RigBodyTests' and FaceMeshReaderTests' old
     errors, #zggj9xy). A dropped diagnostic's own file is then covered only by a newer record of its own, which
     coverage below checks.
     */
    func read(
        targetDirectories: [(String, URL)],
        answeredInProcess: [String: [FindingRecord]] = [:],
    ) throws -> Reading {
        let reader = try SerializedDiagnosticsReader(
            libraryPath: SerializedDiagnosticsReader.toolchainLibraryPath(runner: runner)
        )

        let ours = Dictionary(
            files.map { ($0.url.resolvingSymlinksInPath().path, $0) },
            uniquingKeysWith: { first, _ in first },
        )
        var seen = Set<String>()
        var findings: [FindingRecord] = []
        var recordFor: [String: Date] = [:]
        /* Each named file's text as it stands, read once however many records name it. */
        var sourceModified: [String: Date] = [:]
        func modifiedNow(_ file: String) -> Date {
            if let known = sourceModified[file] {
                return known
            }
            let modified =
                (try? URL(fileURLWithPath: file).resourceValues(forKeys: [.contentModificationDateKey]))?
                .contentModificationDate ?? .distantFuture
            sourceModified[file] = modified
            return modified
        }

        for (targetName, directory) in targetDirectories {
            for record in try Self.diaFiles(in: directory) {
                let modified =
                    try record.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate
                    ?? .distantPast
                /* Keyed by target as well as name: two targets may each hold a `Utilities.swift`, and one must not vouch for the other. */
                let key = "\(targetName)/\(record.deletingPathExtension().lastPathComponent)"
                /*
                 A record libclang cannot read (seen once on Presence, a `.dia` left half-written by a build that
                 was interrupted) vouches for nothing: its file is left without a record, which marks the run
                 incomplete and makes a reused build rebuild, rather than one bad file ending the whole run. The
                 record is removed, since it sits in the engine's own scratch: the build recompiles a file whose
                 output is missing, so the build that follows writes a good one.
                 */
                let diagnostics: [SerializedDiagnosticsReader.Diagnostic]
                do {
                    diagnostics = try reader.read(record)
                }
                catch {
                    do {
                        try FileManager.default.removeItem(at: record)
                    }
                    catch {
                        /* Left in place, the file stays without a record and the run says so; only the repair is lost. */
                    }
                    continue
                }
                recordFor[key] = max(recordFor[key] ?? .distantPast, modified)
                for diagnostic in diagnostics {
                    guard let severity = diagnostic.severity else { continue }
                    let file = URL(fileURLWithPath: diagnostic.file).resolvingSymlinksInPath().path
                    /* A file sourcekitd just checked is answered by that check; its record describes text that has changed since. */
                    guard ours[file] != nil, answeredInProcess[file] == nil else { continue }
                    guard modified >= modifiedNow(file) else { continue }
                    let identity = "\(file):\(diagnostic.line):\(diagnostic.column):\(diagnostic.message)"
                    guard seen.insert(identity).inserted else { continue }
                    findings.append(
                        FindingRecord(
                            source: .compiler,
                            file: file,
                            line: max(diagnostic.line, 1),
                            column: max(diagnostic.column, 1),
                            severity: severity,
                            rule: diagnostic.group,
                            messageId: "",
                            message: diagnostic.message,
                        )
                    )
                }
            }
        }

        findings.append(contentsOf: answeredInProcess.values.joined())

        /* Coverage: each file's own record, by stem, newer than the source as it stands, or a check in this process. */
        var withoutRecord: [String] = []
        for file in files where answeredInProcess[file.url.resolvingSymlinksInPath().path] == nil {
            let key = "\(file.targetName)/\(file.url.deletingPathExtension().lastPathComponent)"
            let sourceModified =
                try file.url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate
                ?? .distantFuture
            guard let recorded = recordFor[key], recorded >= sourceModified else {
                withoutRecord.append(file.url.path)
                continue
            }
        }
        return Reading(findings: findings, filesWithoutRecord: withoutRecord)
    }

    /* Every `.dia` in a target's build directory, one folder per architecture under `Objects-normal`, the layout SwiftPM and xcodebuild share. */
    static func diaFiles(in targetDirectory: URL) throws -> [URL] {
        let objects = targetDirectory.appendingPathComponent("Objects-normal", isDirectory: true)
        guard FileManager.default.fileExists(atPath: objects.path) else { return [] }
        var records: [URL] = []
        for architecture in try FileManager.default.contentsOfDirectory(at: objects, includingPropertiesForKeys: nil) {
            records.append(
                contentsOf: try FileManager.default.contentsOfDirectory(
                    at: architecture,
                    includingPropertiesForKeys: nil,
                ).filter { $0.pathExtension == "dia" }
            )
        }
        return records
    }
}
