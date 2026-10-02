import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/* Fixture pairs for one-type-per-file and file-named-for-type, under Kirk's strict ruling. */
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

    /* Strict: a private helper beside the type is still a second type. */
    @Test func aPrivateHelperIsASecondType() {
        let source = "struct Pane {}\nprivate struct Helper {}\nprotocol Drawable {}\n"
        let findings = OneTypePerFile().findings(in: Self.file("Pane.swift", source))
        #expect(findings.map { "\($0.line):\($0.column)" } == ["2:16", "3:10"])
    }

    /* The helper declared first is the one told to move; the file is not told to be renamed after it. */
    @Test func theTypeNamedLikeTheFileIsTheFilesOwn() {
        let source = "struct Helper {}\nstruct Pane {}\n"
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
