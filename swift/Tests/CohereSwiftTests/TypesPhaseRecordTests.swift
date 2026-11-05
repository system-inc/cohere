import Foundation
import Testing

@testable import CohereSwift

/*
 The types phase reports only what the compile that stands behind the code produced. A record older than the
 file it names describes text that has changed since. Presence's case (2026-11-04, #zggj9xy): a batch-mode
 compile filed another file's errors under PoseSearchTests.dia, a file that never changed, so that record was
 never rewritten and kept reporting the other file's old errors after they were fixed.
 */
struct TypesPhaseRecordTests {
    /* A package of two files, and a real compiler record for `A.swift`'s error filed under `B.swift`'s name, as batch mode can. */
    static func fixture() throws -> (phase: TypesPhase, target: URL, broken: URL, record: URL) {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-records-\(UUID().uuidString)",
            isDirectory: true,
        )
        let sources = root.appendingPathComponent("Sources/A", isDirectory: true)
        try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
        let broken = sources.appendingPathComponent("A.swift")
        try "let broken: Int = \"two\"\n".write(to: broken, atomically: true, encoding: .utf8)
        try "let fine = 1\n".write(to: sources.appendingPathComponent("B.swift"), atomically: true, encoding: .utf8)

        let target = root.appendingPathComponent("A.build", isDirectory: true)
        let objects = target.appendingPathComponent("Objects-normal/arm64", isDirectory: true)
        try FileManager.default.createDirectory(at: objects, withIntermediateDirectories: true)
        let record = objects.appendingPathComponent("B.dia")
        let runner = ProcessRunner()
        let sdk = try runner.run("xcrun", ["--show-sdk-path"], in: root)
        _ = try runner.run(
            "xcrun",
            [
                "swift-frontend", "-typecheck", broken.path, "-serialize-diagnostics-path", record.path, "-sdk",
                String(decoding: sdk.standardOutput, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines),
            ],
            in: root,
        )
        #expect(
            FileManager.default.fileExists(atPath: record.path),
            "the compiler wrote no record, so nothing below is checked",
        )

        let describe =
            #"{"name":"Example","targets":[{"name":"A","type":"library","path":"Sources/A","sources":["A.swift","B.swift"]}]}"#
        let dump = #"{"toolsVersion":{"_version":"6.2.0"},"swiftLanguageVersions":null,"targets":[]}"#
        let package = try PackageModel(root: root, describeJson: Data(describe.utf8), dumpPackageJson: Data(dump.utf8))
        let phase = TypesPhase(
            package: package,
            files: [
                FileSet.OwnedFile(url: broken, targetName: "A", targetKind: "library"),
                FileSet.OwnedFile(
                    url: sources.appendingPathComponent("B.swift"),
                    targetName: "A",
                    targetKind: "library",
                ),
            ],
            scratchPath: root.appendingPathComponent("scratch", isDirectory: true),
            resolutionAllowed: false,
            toolchain: "",
            runner: runner,
        )
        return (phase, target, broken, record)
    }

    static func setModified(_ url: URL, secondsAgo: TimeInterval) throws {
        try FileManager.default.setAttributes(
            [.modificationDate: Date().addingTimeInterval(-secondsAgo)],
            ofItemAtPath: url.path,
        )
    }

    static func findings(_ fixture: (phase: TypesPhase, target: URL, broken: URL, record: URL)) throws -> [String] {
        try fixture.phase.read(
            targetDirectories: [("A", fixture.target)],
            failedBuilds: [],
            start: Date(),
            build: "test",
        )
        .findings.map { "\(URL(fileURLWithPath: $0.file).lastPathComponent):\($0.line)" }
    }

    /* The control: a record newer than the file it names is the current compile's word on it, whichever file the record is named for. */
    @Test func aRecordNewerThanItsFileIsReported() throws {
        let fixture = try Self.fixture()
        try Self.setModified(fixture.broken, secondsAgo: 200)
        try Self.setModified(fixture.record, secondsAgo: 100)
        #expect(try Self.findings(fixture) == ["A.swift:1"])
    }

    /* The bug: the file was changed after the record was written, so the record's errors about it describe text that is gone. */
    @Test func aRecordOlderThanItsFileIsNotReported() throws {
        let fixture = try Self.fixture()
        try Self.setModified(fixture.record, secondsAgo: 200)
        try Self.setModified(fixture.broken, secondsAgo: 100)
        #expect(try Self.findings(fixture).isEmpty)
    }
}
