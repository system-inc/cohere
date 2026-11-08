import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 The kept answers both ways: a key the answer was given under replays it, and a change to any one input the
 answer read asks again. The second half is the one that matters, because a key missing an input would pass
 every replay test and hand back a verdict about text, arguments or a build that are gone.

 The sibling input is proven against the real sourcekitd as well: a file whose body uses another file's
 declaration must be asked again when that declaration changes, or the compiler's new error is never seen.
 Serialized, as `SourcekitdTests` is, so a failure reads as this suite's own.
 */
@Suite(.serialized)
struct BodiesOnlyAnswersTests {
    static let contents = Data("func use() -> Int { make() }\n".utf8)
    static let arguments = ["-module-name", "Subject", "/Subject/Use.swift"]
    static let build = "build fingerprint"

    static func finding(_ message: String) -> FindingRecord {
        FindingRecord(
            source: .compiler,
            file: "/Subject/Use.swift",
            line: 1,
            column: 1,
            severity: .error,
            rule: "",
            messageId: "",
            message: message,
        )
    }

    static func key(
        contents: Data = contents,
        arguments: [String] = arguments,
        build: String = build,
        siblingInterfaces: [String: String] = [:],
        engine: String = "engine",
    ) throws -> String {
        try BodiesOnlyAnswers.key(
            contents: contents,
            arguments: arguments,
            build: build,
            siblingInterfaces: siblingInterfaces,
            engine: engine,
        )
    }

    /* What a run under `key` answers after an earlier run kept "kept" under the base key: replayed, or asked and "asked". */
    static func answer(under key: String) throws -> (findings: [FindingRecord], replayed: Bool) {
        var earlier = BodiesOnlyAnswers()
        let base = try Self.key()
        _ = earlier.answer(path: "/Subject/Use.swift", key: base, replaying: BodiesOnlyAnswers()) {
            ([finding("kept")], nil)
        }
        var next = BodiesOnlyAnswers()
        let answered = next.answer(path: "/Subject/Use.swift", key: key, replaying: earlier) {
            ([finding("asked")], nil)
        }
        return (answered.answer.findings, answered.replayed)
    }

    @Test func theSameInputsReplayTheKeptAnswer() throws {
        let replayed = try Self.answer(under: try Self.key())
        #expect(replayed.replayed)
        #expect(replayed.findings == [Self.finding("kept")])
    }

    @Test func eachInputTheAnswerReadAsksAgain() throws {
        let changed: [(String, String)] = [
            ("contents", try Self.key(contents: Data("func use() -> Int { make() + 1 }\n".utf8))),
            ("arguments", try Self.key(arguments: Self.arguments + ["-DDEBUG"])),
            ("build", try Self.key(build: "another build")),
            ("sibling interfaces", try Self.key(siblingInterfaces: ["/Subject/Make.swift": "changed"])),
            ("engine", try Self.key(engine: "another engine")),
        ]
        for (input, key) in changed {
            let answer = try Self.answer(under: key)
            #expect(!answer.replayed, "a changed \(input) replayed the kept answer")
            #expect(answer.findings == [Self.finding("asked")], "a changed \(input) was not asked again")
        }
    }

    /* Only a sibling whose interface is not the build's is keyed, so editing one more file inside its bodies asks about no other. */
    @Test func onlySiblingsWhoseInterfaceMovedAreKeyed() {
        let built = ["/A.swift": "a", "/B.swift": "b", "/C.swift": "c"]
        #expect(
            BodiesOnlyAnswers.siblingInterfaces(
                of: "/A.swift",
                current: ["/A.swift": "a2", "/B.swift": "b"],
                built: built,
            )
            .isEmpty
        )
        #expect(
            BodiesOnlyAnswers.siblingInterfaces(
                of: "/A.swift",
                current: ["/A.swift": "a", "/B.swift": "b2"],
                built: built,
            )
                == ["/B.swift": "b2"]
        )
    }

    /*
     `Use.swift`'s body calls `make()`, declared in `Make.swift`, and both files are edited. Once `make()` returns
     a String, the compiler has a new error in `Use.swift` whose own bytes never changed, so its kept answer
     must not be replayed.
     */
    @Test func aSiblingsChangedDeclarationAsksAgain() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-answers-\(UUID().uuidString)",
            isDirectory: true,
        ).resolvingSymlinksInPath()
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let make = directory.appendingPathComponent("Make.swift")
        let use = directory.appendingPathComponent("Use.swift")
        let built = "func make() -> Int {\n    1\n}\n"
        try built.write(to: make, atomically: true, encoding: .utf8)
        let useText = "func use() -> Int {\n    make()\n}\n"
        try useText.write(to: use, atomically: true, encoding: .utf8)
        let sdk = try ProcessRunner().run("xcrun", ["--show-sdk-path"], in: directory)
        let sdkPath = String(decoding: sdk.standardOutput, as: UTF8.self).trimmingCharacters(
            in: .whitespacesAndNewlines
        )
        let arguments = [
            "-module-name", "Subject", "-parse-as-library", "-swift-version", "6", "-sdk", sdkPath, make.path, use.path,
        ]
        func interface(_ source: String) -> String { InterfaceFingerprint.of(Parser.parse(source: source)) }
        let builtInterfaces = [make.path: interface(built), use.path: interface(useText)]

        /* One run's answer for `Use.swift`, with `Make.swift` as it is on disk and both files edited. */
        func check(
            makeText: String,
            replaying earlier: BodiesOnlyAnswers,
        ) throws -> (
            answers: BodiesOnlyAnswers, findings: [FindingRecord], replayed: Bool
        ) {
            try makeText.write(to: make, atomically: true, encoding: .utf8)
            let key = try BodiesOnlyAnswers.key(
                contents: Data(useText.utf8),
                arguments: arguments,
                build: Self.build,
                siblingInterfaces: BodiesOnlyAnswers.siblingInterfaces(
                    of: use.path,
                    current: [make.path: interface(makeText), use.path: interface(useText)],
                    built: builtInterfaces,
                ),
            )
            var answers = BodiesOnlyAnswers()
            let answered = try answers.answer(path: use.path, key: key, replaying: earlier) {
                (
                    TypesPhase.findings(
                        in: try Sourcekitd.shared().diagnostics(file: use.path, arguments: arguments, text: useText),
                        of: use.path,
                    ),
                    nil,
                )
            }
            return (answers, answered.answer.findings, answered.replayed)
        }

        let first = try check(makeText: "func make() -> Int {\n    2\n}\n", replaying: BodiesOnlyAnswers())
        #expect(first.findings.isEmpty, "\(first.findings)")
        let bodyOnly = try check(makeText: "func make() -> Int {\n    3\n}\n", replaying: first.answers)
        #expect(bodyOnly.replayed, "a sibling's body edit asked again")
        let changed = try check(makeText: "func make() -> String {\n    \"three\"\n}\n", replaying: bodyOnly.answers)
        #expect(!changed.replayed, "a sibling's changed declaration replayed the answer from before it")
        #expect(
            changed.findings.contains { $0.severity == .error && $0.line == 2 },
            "the error the changed declaration causes was not found: \(changed.findings)",
        )
    }
}
