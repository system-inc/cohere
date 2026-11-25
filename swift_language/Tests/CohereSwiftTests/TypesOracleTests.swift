import Foundation
import Testing

@testable import CohereSwift

/*
 The oracle's verdict on a real two-file package, checked here. A build stopped at an error at file scope: the
 module's emit-module job failed, and the build compiled neither file one by one, so neither had a record, and on
 2026-11-05 the oracle read the matching counts as agreement. The check never stops there (#11fs5xp): a batch that
 fails is checked again one file at a time, so both files have records and both sides report the one error. A
 file without a record still never reads as agreement, which the second test pins on the report itself.
 */
@Suite(.serialized)
struct TypesOracleTests {
    static let manifest =
        "// swift-tools-version:6.0\nimport PackageDescription\nlet package = Package(name: \"Gauge\", targets: [.target(name: \"Gauge\")])\n"

    @Test func aModuleWithAnErrorAtFileScopeHasEveryFileRecorded() throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-oracle-\(UUID().uuidString)",
            isDirectory: true,
        ).resolvingSymlinksInPath()
        let sources = root.appendingPathComponent("Sources/Gauge", isDirectory: true)
        try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
        try Self.manifest.write(to: root.appendingPathComponent("Package.swift"), atomically: true, encoding: .utf8)
        let broken = sources.appendingPathComponent("Broken.swift")
        let reading = sources.appendingPathComponent("Reading.swift")
        try "let broken: Int = \"not a number\"\n".write(to: broken, atomically: true, encoding: .utf8)
        try "func reading() -> Int {\n    return 1\n}\n".write(to: reading, atomically: true, encoding: .utf8)

        let checked = try TypesOracle(root: root).run()
        #expect(checked.filesWithoutRecord.isEmpty, "\(checked.filesWithoutRecord)")
        #expect(checked.build.count == 1, "\(checked.build)")
        #expect(checked.agrees)

        try "func broken() -> Int {\n    var value = 1\n    return value\n}\n".write(
            to: broken,
            atomically: true,
            encoding: .utf8,
        )
        let warned = try TypesOracle(root: root).run()
        #expect(warned.filesWithoutRecord.isEmpty, "\(warned.filesWithoutRecord) \(warned.build)")
        #expect(warned.build.count == 1)
        #expect(warned.agrees)
    }

    /* A file the check left without a record is one nobody vouched for, so the two sides matching is no agreement. */
    @Test func aFileWithoutARecordIsNeverAgreement() {
        let report = TypesOracle.Report(
            filesOwned: 2,
            filesAsked: 2,
            filesWithoutCommand: [],
            filesUnanswered: [:],
            filesWithoutRecord: ["/package/Sources/Gauge/Reading.swift"],
            build: [],
            sourcekitd: [],
            groupDifferences: [],
            controlFile: "/package/Sources/Gauge/Broken.swift",
            controlFound: true,
            buildMilliseconds: 0,
            sourcekitdMilliseconds: 0,
            slowestFiles: [],
        )
        #expect(!report.agrees)
    }
}
