import CryptoKit
import Foundation
import Testing

@testable import CohereSwift

/*
 The compiled-in house messages (`RuleMessages`, generated from cohere's policy/messages/ by
 `go run ./policy/tools/generate`) against the files they were made from, and against the rules that render them.
 */
struct RuleMessagesTests {
    /* `swift/Tests/CohereSwiftTests/` up to cohere's root. */
    static let repositoryRoot = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent()

    /* The digest the generator records, recomputed the same way: each file's name, a zero byte, its bytes and a zero byte, in name order. */
    static func digestOnDisk() throws -> String {
        let directory = repositoryRoot.appendingPathComponent("policy/messages")
        let names = try FileManager.default.contentsOfDirectory(atPath: directory.path)
            .filter { $0.hasSuffix(".json") }
            .sorted { $0.utf8.lexicographicallyPrecedes($1.utf8) }
        var hash = SHA256()
        for name in names {
            hash.update(data: Data(name.utf8) + Data([0]))
            hash.update(data: try Data(contentsOf: directory.appendingPathComponent(name)) + Data([0]))
        }
        return hash.finalize()
            .map { byte in
                let digits = String(byte, radix: 16)
                return digits.count == 1 ? "0" + digits : digits
            }
            .joined()
    }

    /* A message file edited and not regenerated fails here, so the Swift rules cannot print words the catalog no longer holds. */
    @Test func theMessagesWereGeneratedFromTheFilesOnDisk() throws {
        #expect(RuleMessages.sourceDigest == (try Self.digestOnDisk()), "run go run ./policy/tools/generate")
    }

    /* Every Swift source except the generated catalog, as one text, for the test below to search. */
    static func engineSources() throws -> String {
        let sources = repositoryRoot.appendingPathComponent("swift/Sources")
        guard let walker = FileManager.default.enumerator(at: sources, includingPropertiesForKeys: nil) else {
            return ""
        }
        var text = ""
        for case let file as URL in walker
        where file.pathExtension == "swift" && !file.lastPathComponent.hasSuffix(".generated.swift") {
            text += try String(contentsOf: file, encoding: .utf8)
        }
        return text
    }

    /* `RuleMessages.Rule.id(`, the call a rule makes. */
    static func isCalled(_ entry: String, in sources: String) -> Bool {
        sources.contains("RuleMessages.\(entry)(")
    }

    /*
     An entry no rule renders fails, as the Go registry test fails on one. The positive control comes first: the
     search finds bare-throw's call, which a rule makes, and does not find a name nothing calls, so a search that
     matches nothing, or everything, cannot pass the check below.
     */
    @Test func everyMessageIsRenderedByARule() throws {
        let sources = try Self.engineSources()
        #expect(Self.isCalled("ConsistencyNoBareThrow.bareThrow", in: sources))
        #expect(!Self.isCalled("ConsistencyNoBareThrow.noSuchMessage", in: sources))
        #expect(!RuleMessages.all.isEmpty)
        for entry in RuleMessages.all {
            #expect(Self.isCalled(entry, in: sources), "\(entry) is in the catalog and no rule renders it")
        }
    }
}
