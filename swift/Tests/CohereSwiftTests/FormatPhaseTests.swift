import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 The format phase must agree with `xcrun swift-format` byte for byte, because `Format.sh` and cohere read
 the same `.swift-format` and two formatters that disagree turn every run into a tug of war over the same
 lines. The fixture is deliberately misformatted: if both outputs equalled the input, the comparison would
 pass for a formatter that does nothing.
 */
struct FormatPhaseTests {
    static let configuration = #"{"version":1,"lineLength":120,"indentation":{"spaces":4},"rules":{"NoBlockComments":false}}"#

    static let dirtySource = """
        import Foundation
        struct  Example{
          let value:Int
            func doubled()->Int { value*2 }
        /* A prose block above a declaration, kept as written. */
        func helper( _ a:Int,_ b:Int)->Int{return a+b}
        }

        """

    static func workspace(source: String, withConfiguration: Bool) throws -> URL {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("cohere-swift-format-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        if withConfiguration {
            try configuration.write(to: directory.appendingPathComponent(".swift-format"), atomically: true, encoding: .utf8)
        }
        try source.write(to: directory.appendingPathComponent("Example.swift"), atomically: true, encoding: .utf8)
        return directory
    }

    static func parsed(_ url: URL, source: String) -> ParsedFile {
        ParsedFile(url: url, targetName: "Example", targetKind: "library", source: source, tree: Parser.parse(source: source), nodeCount: 0)
    }

    @Test func theLibraryMatchesTheToolchainFormatterAndActuallyChangesTheFile() throws {
        let directory = try Self.workspace(source: Self.dirtySource, withConfiguration: true)
        let file = directory.appendingPathComponent("Example.swift")
        let outcome = FormatPhase.format(Self.parsed(file, source: Self.dirtySource), boundary: directory)
        guard case let .changed(formatted) = outcome else {
            Issue.record("the dirty fixture was not changed, so the comparison below would prove nothing: \(outcome)")
            return
        }
        let toolchain = try ProcessRunner().run(
            "xcrun",
            ["swift-format", "format", "--configuration", directory.appendingPathComponent(".swift-format").path, file.path],
            in: directory
        )
        #expect(toolchain.succeeded, "xcrun swift-format failed: \(toolchain.standardError)")
        #expect(formatted == String(decoding: toolchain.standardOutput, as: UTF8.self))
        #expect(formatted.contains("/* A prose block above a declaration, kept as written. */"))
    }

    @Test func noConfigurationMeansDeclinedWithAReason() throws {
        let directory = try Self.workspace(source: Self.dirtySource, withConfiguration: false)
        let file = directory.appendingPathComponent("Example.swift")
        guard case let .declined(reason) = FormatPhase.format(Self.parsed(file, source: Self.dirtySource), boundary: directory) else {
            Issue.record("a file with no .swift-format above it was formatted with something")
            return
        }
        #expect(reason.contains(".swift-format"))
    }

    @Test func aConfigurationAboveTheBoundaryIsNotUsed() throws {
        let outer = try Self.workspace(source: "", withConfiguration: true)
        let inner = outer.appendingPathComponent("Repository", isDirectory: true)
        try FileManager.default.createDirectory(at: inner, withIntermediateDirectories: true)
        #expect(FormatPhase.configurationFile(for: inner.appendingPathComponent("A.swift"), boundary: inner) == nil)
        #expect(FormatPhase.configurationFile(for: inner.appendingPathComponent("A.swift"), boundary: outer) != nil)
    }

    @Test func theFirstDifferingLineIsReported() {
        #expect(FormatPhase.firstDifferingLine("a\nb\nc", "a\nB\nc") == 2)
        #expect(FormatPhase.firstDifferingLine("a\nb", "a\nb\n") == 3)
    }
}
