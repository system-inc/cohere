import Foundation
import Testing

@testable import CohereSwift

/*
 The provenance names the parser and formatter that produced every finding, so it must name the ones
 actually resolved. This reads `Package.resolved` and fails when the constants drift from it.
 */
struct EngineVersionTests {
    static let resolvedFile = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .appendingPathComponent("Package.resolved")

    static func resolvedVersion(of identity: String) throws -> String? {
        let document = try JSONSerialization.jsonObject(with: Data(contentsOf: resolvedFile)) as? [String: Any]
        let pins = document?["pins"] as? [[String: Any]] ?? []
        let pin = pins.first { ($0["identity"] as? String) == identity }
        return (pin?["state"] as? [String: Any])?["version"] as? String
    }

    @Test func swiftSyntaxMatchesWhatWasResolved() throws {
        #expect(try Self.resolvedVersion(of: "swift-syntax") == EngineVersion.swiftSyntax)
    }

    @Test func swiftFormatMatchesWhatWasResolved() throws {
        #expect(try Self.resolvedVersion(of: "swift-format") == EngineVersion.swiftFormat)
    }

    /*
     What `swift --version` printed, handed down by the caller, names the toolchain exactly as the engine's own question
     does, so every cache keyed on the toolchain reads the same key either way.
     */
    @Test func aGivenVersionOutputNamesTheToolchainAsAskingDoes() throws {
        let asked = try ProcessRunner().run("swift", ["--version"], in: URL(fileURLWithPath: "/"))
        let printed = String(decoding: asked.standardOutput, as: UTF8.self)
        let given = EngineVersion.toolchainGiven([EngineVersion.toolchainVariable: printed])
        #expect(given != nil)
        #expect(given == EngineVersion.toolchainNamed(in: printed + asked.standardError))
        #expect(given == EngineVersion.toolchain())
    }

    /* A value naming no toolchain is not trusted: the engine asks instead. */
    @Test func aGivenValueNamingNoToolchainIsNotTaken() {
        #expect(EngineVersion.toolchainGiven([:]) == nil)
        #expect(EngineVersion.toolchainGiven([EngineVersion.toolchainVariable: ""]) == nil)
        #expect(EngineVersion.toolchainGiven([EngineVersion.toolchainVariable: "Apple Swift version 6.4.2"]) == nil)
        #expect(
            EngineVersion.toolchainGiven([
                EngineVersion.toolchainVariable:
                    "swift-driver version: 1.168.6 Apple Swift version 6.4.2 (swiftlang-6.4.2.1.9 clang-1700.6.4.2)"
            ]) == "swiftlang-6.4.2.1.9"
        )
    }
}
