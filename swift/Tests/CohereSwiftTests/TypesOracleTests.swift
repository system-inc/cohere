import Foundation
import Testing

@testable import CohereSwift

/*
 The oracle's verdict on a real two-file package, built here. An error at file scope fails the module's
 emit-module job, so the build compiles neither file one by one and leaves no record for either, while it and
 sourcekitd both report that one error. Measured 2026-11-05: the counts matched, and the oracle read that as
 agreement. A warning in its place lets the build compile every file, so the two sides agree for real, which
 shows the first verdict is about the records and not a sourcekitd that cannot answer. A warning, not an error
 inside a body: an error still fails the build, which then stops scheduling the jobs left, so under load the
 other file was measured never compiling.
 */
@Suite(.serialized)
struct TypesOracleTests {
    static let manifest =
        "// swift-tools-version:6.0\nimport PackageDescription\nlet package = Package(name: \"Gauge\", targets: [.target(name: \"Gauge\")])\n"

    @Test func aBuildThatStopsBeforeCompilingAFileDoesNotAgree() throws {
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

        let stopped = try TypesOracle(root: root).run()
        #expect(stopped.filesWithoutRecord.contains(reading.path))
        #expect(stopped.onlyBuild.isEmpty && stopped.onlySourcekitd.isEmpty)
        #expect(!stopped.agrees)

        try "func broken() -> Int {\n    var value = 1\n    return value\n}\n".write(
            to: broken,
            atomically: true,
            encoding: .utf8,
        )
        let compiled = try TypesOracle(root: root).run()
        #expect(compiled.filesWithoutRecord.isEmpty, "\(compiled.filesWithoutRecord) \(compiled.build)")
        #expect(compiled.build.count == 1)
        #expect(compiled.agrees)
    }
}
