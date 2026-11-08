import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 The first typed rule, end to end on a real package, through every way a run can learn what `count` resolves
 to: the build's index, sourcekitd in process for a file the index no longer describes, and neither, which
 must read as unchecked. The negative case is the reason the rule is typed: a struct's own `count` compared
 with zero is not a collection's, and SwiftLint, judging spelling, would flag it.
 */
@Suite(.serialized)
struct EmptyCountTests {
    static let source = """
        struct Tally {
            var count: Int
        }

        func checks(items: [Int], tally: Tally) -> Bool {
            items.count == 0 || tally.count == 0 || 0 < items.count
        }

        """

    final class Package {
        let root: URL

        init(source: String) throws {
            root = FileManager.default.temporaryDirectory.appendingPathComponent(
                "cohere-swift-typed-\(UUID().uuidString)",
                isDirectory: true,
            )
            let sources = root.appendingPathComponent("Sources/Control", isDirectory: true)
            try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
            try PipelineControlTests.manifest.write(
                to: root.appendingPathComponent("Package.swift"),
                atomically: true,
                encoding: .utf8,
            )
            try write(source)
        }

        func write(_ source: String) throws {
            try source.write(
                to: root.appendingPathComponent("Sources/Control/Control.swift"),
                atomically: true,
                encoding: .utf8,
            )
        }

        /* One `--no-fix` run with the given phase flags: the lines of the rule's findings, the lint record's crashes, and whether the run was complete. */
        func run(_ flags: [String] = []) async throws -> (lines: [Int], crashes: Int, complete: Bool, build: String) {
            let options = try CommandOptions.parse(
                ["--contract", "\(EngineVersion.contract)", "--root", root.path, "--no-fix"] + flags,
                workingDirectory: root,
            )
            var stream = Data()
            let writer = ContractWriter { stream.append($0) }
            _ = try await Pipeline(options: options, writer: writer, workingDirectory: root).run()
            var lines: [Int] = []
            var crashes = 0
            var complete = false
            var build = ""
            for line in stream.split(separator: UInt8(ascii: "\n")) {
                let record = try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
                switch record["kind"] as? String {
                    case "finding" where record["rule"] as? String == EmptyCount().name:
                        lines.append(record["line"] as? Int ?? 0)
                    case "lint":
                        crashes = (record["crashes"] as? [Any])?.count ?? 0
                    case "summary":
                        complete = record["complete"] as? Bool ?? false
                    case "types":
                        build = record["build"] as? String ?? ""
                    default:
                        break
                }
            }
            return (lines, crashes, complete, build)
        }
    }

    /* After a build, from the index: both comparisons of the array's count, and not the tally's. */
    @Test func aCollectionsCountIsFlaggedAndATallysIsNot() async throws {
        let package = try Package(source: Self.source)
        let run = try await package.run()
        #expect(run.lines == [6, 6], "expected the two array comparisons on line 6 and not the tally's: \(run.lines)")
        #expect(run.crashes == 0)
        #expect(run.complete)
    }

    /*
     A `count` of our own constrained extension on `Array`, `Dictionary` and `Set`, which the compiler picks over the
     standard library's for those element types, is not flagged, and the standard library's `count` on other element
     types still is (#awb2ykk). Red under the reading of the symbol that took the constrained extension's members for
     the standard library's.
     */
    static let constrainedSource = """
        extension Array where Element == Int {
            var count: Int { 7 }
        }

        extension Dictionary where Key == String {
            var count: Int { 7 }
        }

        extension Set where Element == Int {
            var count: Int { 7 }
        }

        func checks(numbers: [Int], names: [String: Int], seen: Set<Int>, words: [String], tags: [Int: String], codes: Set<String>) -> Bool {
            numbers.count == 0 || names.count == 0 || seen.count == 0
                || words.count == 0 || tags.count == 0 || codes.count == 0
        }

        """

    @Test func aCountOfOurConstrainedExtensionIsNotFlagged() async throws {
        let package = try Package(source: Self.constrainedSource)
        let run = try await package.run()
        #expect(
            run.lines == [15, 15, 15],
            "expected only the standard library's three on line 15, none of ours on 14: \(run.lines)",
        )
        #expect(run.crashes == 0)
    }

    /* A body-only edit is checked without building, so the index no longer describes the file, and sourcekitd must. */
    @Test func aFileTheIndexNoLongerDescribesIsAskedInProcess() async throws {
        let package = try Package(source: Self.source)
        _ = try await package.run()
        try package.write(
            Self.source.replacingOccurrences(
                of: "    items.count == 0 ||",
                with: "    let none = items.count != 0\n    return none || items.count == 0 ||",
            )
        )
        let run = try await package.run()
        #expect(
            run.build.contains(TypesPhase.checkedInProcessWords),
            "the edit was built, so this test did not reach sourcekitd: \(run.build)",
        )
        #expect(run.lines == [6, 7, 7], "expected the new comparison on line 6 and the two on line 7: \(run.lines)")
        #expect(run.complete)
    }

    /*
     A body edit's run type-checks the edited file once: the types phase's check answers for its diagnostics and,
     from the same AST, its names, which lint reads instead of asking sourcekitd again (#ng5b4nx). The run after it,
     with nothing changed, replays both and checks it not at all. Counted through the session's own tally of the
     files it type-checked. Red under a pipeline that does not hand the types phase's names to lint, which checks
     the file twice, and once more on the replaying run.
     */
    @Test func aBodyEditIsTypeCheckedOnceForTypesAndLint() async throws {
        let package = try Package(source: Self.source)
        _ = try await package.run()
        try package.write(
            Self.source.replacingOccurrences(
                of: "    items.count == 0 ||",
                with: "    let none = items.count != 0\n    return none || items.count == 0 ||",
            )
        )
        let edited = package.root.appendingPathComponent("Sources/Control/Control.swift").resolvingSymlinksInPath()
        let session = try Sourcekitd.shared()
        #expect(session.typeChecks(of: edited.path) == 0, "the control: nothing asked about the fresh file yet")
        let run = try await package.run()
        #expect(
            run.build.contains(TypesPhase.checkedInProcessWords),
            "the edit was built, so this test did not reach sourcekitd: \(run.build)",
        )
        #expect(run.lines == [6, 7, 7], "\(run.lines)")
        #expect(session.typeChecks(of: edited.path) == 1, "the edited file was type-checked more than once")
        let again = try await package.run()
        #expect(again.lines == [6, 7, 7], "\(again.lines)")
        #expect(session.typeChecks(of: edited.path) == 1, "a run that replayed the answer type-checked the file")
    }

    /*
     Lint alone on a package never built, whose build fails before compiling (a dependency `--no-fix` cannot
     resolve): no index, no recorded compile, so the file is unchecked and the run says so.
     */
    @Test func aFileNoSourceDescribesIsUncheckedNotClean() async throws {
        let package = try Package(source: Self.source)
        try PipelineControlTests.unresolvableManifest.write(
            to: package.root.appendingPathComponent("Package.swift"),
            atomically: true,
            encoding: .utf8,
        )
        let run = try await package.run(["--lint"])
        #expect(run.lines.isEmpty)
        #expect(run.crashes == 1)
        #expect(!run.complete)
    }

    @Test func aFileWithNoCandidateIsNotFetched() {
        let source = "struct Plain {}\n"
        let file = ParsedFile(
            url: URL(fileURLWithPath: "/Plain.swift"),
            targetName: "Control",
            targetKind: "regular",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 0,
        )
        #expect(!EmptyCount().applies(to: file))
    }
}
