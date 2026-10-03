import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/* Fixture pairs for one-type-per-file (a main type plus small helpers, Kirk's ruling of 2026-10-03) and file-named-for-type. */
struct FileShapeRuleTests {
    static func file(_ name: String, _ source: String) -> ParsedFile {
        ParsedFile(url: URL(fileURLWithPath: "/fixture/\(name)"), targetName: "Fixture", targetKind: "library", source: source, tree: Parser.parse(source: source), nodeCount: 0)
    }

    static func messages(_ rule: some FileRule, _ name: String, _ source: String) -> [String] {
        let subject = file(name, source)
        guard rule.applies(to: subject) else { return [] }
        return rule.findings(in: subject).map(\.messageId)
    }

    @Test func oneTypeWithNestedHelpersPasses() {
        let source = "struct Pane {\n    struct Layout {}\n    enum Mode { case a }\n}\nextension Pane { func draw() {} }\n"
        #expect(Self.messages(OneTypePerFile(), "Pane.swift", source).isEmpty)
        #expect(Self.messages(FileNamedForType(), "Pane.swift", source).isEmpty)
    }

    /* A struct of `lines` lines, braces included, so a fixture can sit exactly at the limit or one past it. */
    static func structSource(_ name: String, lines: Int) -> String {
        "struct \(name) {\n" + (0..<(lines - 2)).map { "    var field\($0) = 0\n" }.joined() + "}\n"
    }

    static func positions(_ name: String, _ source: String) -> [String] {
        OneTypePerFile().findings(in: Self.file(name, source)).map { "\($0.line):\($0.column)" }
    }

    /* A private or fileprivate helper stays, of any kind and size; the protocol beside it is not a helper. */
    @Test func aPrivateHelperStaysAndAPublicProtocolMoves() {
        let source = "struct Pane {}\nprivate final class Helper {}\nfileprivate actor Worker {}\nprotocol Drawable {}\n"
        #expect(Self.positions("Pane.swift", source) == ["4:10"])
    }

    @Test func aLargePrivateHelperStillStays() {
        let source = "struct Pane {}\nprivate " + Self.structSource("Layout", lines: 200)
        #expect(Self.positions("Pane.swift", source).isEmpty)
    }

    /* Small means at most 30 lines: 30 stays, 31 moves. */
    @Test func aSmallValueTypeStaysAtTheLimitAndMovesPastIt() {
        #expect(Self.positions("Pane.swift", "struct Pane {}\n" + Self.structSource("Row", lines: 30)).isEmpty)
        #expect(Self.positions("Pane.swift", "struct Pane {}\n" + Self.structSource("Row", lines: 31)) == ["2:8"])
        #expect(Self.positions("Pane.swift", "struct Pane {}\nenum Mode { case idle, busy }\n").isEmpty)
    }

    /* A class, actor or protocol is never a small value type, however short. */
    @Test func aSmallReferenceTypeOrProtocolMoves() {
        let source = "struct Pane {}\nfinal class Box {}\nactor Worker {}\nprotocol Drawable {}\n"
        #expect(Self.positions("Pane.swift", source) == ["2:13", "3:7", "4:10"])
    }

    /* The comment directly above and the attributes count toward the 30; a comment a blank line away does not. */
    @Test func theAttachedCommentAndAttributesAreCounted() {
        let body = Self.structSource("Row", lines: 28)
        #expect(Self.positions("Pane.swift", "struct Pane {}\n\n/* Why. */\n@frozen\n" + body).isEmpty)
        #expect(Self.positions("Pane.swift", "struct Pane {}\n\n/*\n Why.\n */\n@frozen\n" + body) == ["7:8"])
        #expect(Self.positions("Pane.swift", "struct Pane {}\n\n/// Why.\n/// More.\n@frozen\n" + body) == ["6:8"])
        #expect(Self.positions("Pane.swift", "struct Pane {}\n\n/*\n Header.\n */\n\n@frozen\n" + body).isEmpty)
    }

    /* An extension of the small type in the same file is part of its size. */
    @Test func anExtensionOfTheSmallTypeCountsTowardIt() {
        let small = "struct Pane {}\n" + Self.structSource("Row", lines: 20)
        #expect(Self.positions("Pane.swift", small + "extension Row {\n    var total: Int { 0 }\n}\n").isEmpty)
        /* Twenty lines of struct and eleven of extension: thirty-one in all. */
        let extensionSource = "extension Row {\n" + (0..<9).map { "    var extra\($0): Int { 0 }\n" }.joined() + "}\n"
        #expect(Self.positions("Pane.swift", small + extensionSource) == ["2:8"])
    }

    /* The message names both remedies and never asks for a type to be nested only to pass. */
    @Test func theMessageNamesBothRemediesAndNeverNesting() throws {
        let finding = try #require(OneTypePerFile().findings(in: Self.file("Pane.swift", "struct Pane {}\nfinal class Box {}\n")).first)
        #expect(finding.message.contains("Move it to Box.swift"))
        #expect(finding.message.contains("mark it private"))
        #expect(finding.message.contains("small value type"))
        #expect(!finding.message.lowercased().contains("nest"))
    }

    /* The helper declared first is the one told to move; the file is not told to be renamed after it. */
    @Test func theTypeNamedLikeTheFileIsTheFilesOwn() {
        let source = "final class Helper {}\nstruct Pane {}\n"
        let findings = OneTypePerFile().findings(in: Self.file("Pane.swift", source))
        #expect(findings.map(\.message).first?.hasPrefix("Helper is a second top-level type") == true)
        #expect(Self.messages(FileNamedForType(), "Pane.swift", source).isEmpty)
    }

    @Test func aTypeInBothBranchesOfAnIfIsOneType() {
        let source = "#if os(macOS)\nstruct Pane {}\n#else\nstruct Pane {}\n#endif\n"
        #expect(Self.messages(OneTypePerFile(), "Pane.swift", source).isEmpty)
    }

    @Test func aTypeAliasIsNotAType() {
        let source = "struct Pane {}\ntypealias Panes = [Pane]\n"
        #expect(Self.messages(OneTypePerFile(), "Pane.swift", source).isEmpty)
    }

    @Test func aFileNotNamedForItsTypeIsReported() {
        #expect(Self.messages(FileNamedForType(), "Utilities.swift", "final class Pane {}\n") == ["fileNotNamedForType"])
    }

    @Test func anExtensionOfAnotherTypeBelongsInItsOwnFile() {
        let source = "struct Pane {}\nextension String { var trimmed: String { self } }\n"
        #expect(Self.messages(FileNamedForType(), "Pane.swift", source) == ["extensionOutsideItsFile"])
    }

    @Test func aPrivateExtensionStaysBesideItsType() {
        let source = "struct App {}\nprivate extension View { var windowEnvironment: Int { 0 } }\n"
        #expect(Self.messages(FileNamedForType(), "App.swift", source).isEmpty)
    }

    @Test func aPrivateHelperExtensionStaysInAPurposeFile() {
        let source = "extension Terminal.SeededVtState { func wireCheckpoint() {} }\nfileprivate extension VtColor { init(value: Int) {} }\n"
        #expect(Self.messages(FileNamedForType(), "Terminal+AuthoritativeCheckpoint.swift", source).isEmpty)
    }

    /* The control: with no visible face, a private extension is just a misnamed file. */
    @Test func aFileOfOnlyPrivateExtensionsIsStillNamedForWhatItExtends() {
        let source = "private extension VtColor { init(value: Int) {} }\n"
        #expect(Self.messages(FileNamedForType(), "Helpers.swift", source) == ["extensionOutsideItsFile"])
    }

    /* The SwiftUI modifier idiom: the extension is the modifier's public face. */
    @Test func anExtensionExposingTheFilesTypeStaysBesideIt() {
        let source = "struct DelayWidthUntilIdle: ViewModifier {}\nextension View {\n    func delayWidthUntilIdle() -> some View { modifier(DelayWidthUntilIdle()) }\n}\n"
        #expect(Self.messages(FileNamedForType(), "DelayWidthUntilIdle.swift", source).isEmpty)
    }

    /* One member that does not name the type is enough to send the extension to its own file. */
    @Test func anExtensionWithUnrelatedMembersStillMoves() {
        let source = "struct Pane {}\nextension View {\n    func pane() -> Pane { Pane() }\n    func unrelated() {}\n}\n"
        #expect(Self.messages(FileNamedForType(), "Pane.swift", source) == ["extensionOutsideItsFile"])
    }

    @Test func purposeFilesPass() {
        #expect(Self.messages(FileNamedForType(), "String+Trimming.swift", "extension String {}\n").isEmpty)
        #expect(Self.messages(FileNamedForType(), "Array+Chunks.swift", "extension Array where Element == Int {}\n").isEmpty)
        #expect(Self.messages(FileNamedForType(), "Outer+Inner.swift", "extension Outer.Inner {}\n").isEmpty)
        #expect(Self.messages(FileNamedForType(), "Outer.Inner+Codable.swift", "extension Outer.Inner: Codable {}\n").isEmpty)
    }

    /* A purpose file's helpers stay beside the extension it is named for, under both rules; a bigger internal type, or a file whose only face is the helper, still answers. */
    @Test func aPurposeFileMayHoldHelperTypes() {
        let privateHelper = "extension Prune {\n    func run() {}\n}\nprivate struct BinarySlice {\n    var low: Int\n}\nextension BinarySlice { var isEmpty: Bool { low == 0 } }\n"
        #expect(Self.messages(FileNamedForType(), "Prune+Search.swift", privateHelper).isEmpty)
        #expect(Self.messages(OneTypePerFile(), "Prune+Search.swift", privateHelper).isEmpty)
        let smallValues = "extension StagePane {\n    func probe() {}\n}\nstruct ProbeResult {\n    var score: Double\n}\nenum ProbeStage { case warm, run }\n"
        #expect(Self.messages(FileNamedForType(), "StagePane+Probe.swift", smallValues).isEmpty)
        #expect(Self.messages(OneTypePerFile(), "StagePane+Probe.swift", smallValues).isEmpty)
        let internalClass = "extension StagePane {\n    func probe() {}\n}\nfinal class ProbeRunner {}\n"
        #expect(Self.messages(FileNamedForType(), "StagePane+Probe.swift", internalClass).count == 1)
        let helperAsTheOnlyFace = "private extension StagePane {\n    func probe() {}\n}\nprivate struct ProbeResult {}\n"
        #expect(Self.messages(FileNamedForType(), "StagePane+Probe.swift", helperAsTheOnlyFace).count == 1)
    }

    @Test func anExtensionOnlyFileNamedForSomethingElseIsReported() {
        #expect(Self.messages(FileNamedForType(), "Helpers.swift", "extension String {}\n") == ["extensionOutsideItsFile"])
    }

    @Test func mainKeepsItsName() {
        #expect(Self.messages(FileNamedForType(), "main.swift", "struct Tool {}\nTool().run()\n").isEmpty)
    }

    @Test func freeFunctionsHaveNoNameToMatch() {
        #expect(Self.messages(FileNamedForType(), "Math.swift", "func clamp(_ value: Int) -> Int { value }\n").isEmpty)
    }
}
