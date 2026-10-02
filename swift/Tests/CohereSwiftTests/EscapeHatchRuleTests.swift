import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/* Fixture pairs for print by target kind, escape hatches with reasons, discarded try?, failure messages and TODO comments. */
struct EscapeHatchRuleTests {
    static func file(_ source: String, kind: String = "library") -> ParsedFile {
        ParsedFile(url: URL(fileURLWithPath: "/fixture/Subject.swift"), targetName: "Fixture", targetKind: kind, source: source, tree: Parser.parse(source: source), nodeCount: 0)
    }

    static func lines(_ rule: some FileRule, _ source: String, kind: String = "library") -> [Int] {
        let subject = file(source, kind: kind)
        guard rule.applies(to: subject) else { return [] }
        return rule.findings(in: subject).map(\.line)
    }

    @Test func printIsDecidedByTargetKind() {
        let tool = "import Foundation\nprint(\"usage: tool <file>\")\n"
        let app = "import SwiftUI\nprint(\"tapped\")\n"
        #expect(Self.lines(NoPrint(), tool, kind: "library") == [2])
        #expect(Self.lines(NoPrint(), tool, kind: "executable").isEmpty, "a command-line tool's stdout is its interface")
        #expect(Self.lines(NoPrint(), tool, kind: "application") == [2], "an app's model file imports only Foundation and is still app code")
        #expect(Self.lines(NoPrint(), app, kind: "test").isEmpty)
    }

    @Test func aMethodNamedPrintIsNotPrint() {
        #expect(Self.lines(NoPrint(), "printer.print(page)\nlet print = 3\n").isEmpty)
        #expect(Self.lines(NoPrint(), "debugPrint(value)\n") == [1])
    }

    @Test func escapeHatchesNeedACommentDirectlyAbove() {
        let source = """
            final class Bare: @unchecked Sendable {}

            /* The buffer is only touched on the render queue. */
            final class Explained: @unchecked Sendable {}

            /* A comment two paragraphs up is about something else. */

            nonisolated(unsafe) var orphaned = 0
            // The callback is registered once, before any thread starts.
            nonisolated(unsafe) var explained = 0
            @preconcurrency import Darwin
            """
        #expect(Self.lines(RequireEscapeHatchReason(), source) == [1, 8, 11])
    }

    @Test func plainSendableAndNonisolatedAreNotHatches() {
        #expect(Self.lines(RequireEscapeHatchReason(), "struct Value: Sendable {}\nnonisolated func work() {}\n").isEmpty)
    }

    @Test func discardedTryOptionalsAreFound() {
        let source = """
            func cleanUp() {
                try? FileManager.default.removeItem(at: url)
                _ = try? load()
                let kept = try? load()
                if let value = try? load() { use(value) }
            }
            """
        #expect(Self.lines(NoDiscardedTryOptional(), source) == [2, 3])
    }

    /* A sole `try?` is its body's value wherever the body returns one (the shapes @system_cohere_swift_ahraos_presence found on Presence), and a discard wherever it does not. */
    @Test func aSoleTryOptionalThatIsItsBodysValueIsNotADiscard() {
        let values = """
            let looks = data.flatMap { try? decoder.decode([Look].self, from: $0) } ?? []
            let ledges = candidates.compactMap { try? self.fit(ledge: $0) }
            func latency() -> Double? { try? OutputLatency.read() }
            var duration: Double? { try? asset.duration() }
            var cached: Data? {
                get { try? Data(contentsOf: url) }
            }
            let chosen = if fast { try? quick() } else { try? slow() }
            let picked = switch mode {
            case .fast: try? quick()
            default: nil
            }
            """
        #expect(Self.lines(NoDiscardedTryOptional(), values).isEmpty)
        let discards = """
            func cleanUp() { try? FileManager.default.removeItem(at: url) }
            func reset() -> Void { try? store.clear() }
            if stale { try? cache.purge() }
            switch mode {
            case .fast: try? quick()
            default: break
            }
            let runs = items.map { item in
                try? item.write()
                return item
            }
            """
        #expect(Self.lines(NoDiscardedTryOptional(), discards) == [1, 2, 3, 5, 9])
    }

    @Test func failuresWithoutMessagesAreFound() {
        let source = """
            fatalError()
            fatalError("")
            fatalError("the cache was built before the store opened")
            preconditionFailure()
            assertionFailure("unreachable: every case returns above")
            """
        #expect(Self.lines(FatalErrorMessage(), source) == [1, 2, 4])
    }

    @Test func todoCommentsAreFound() {
        let source = """
            // TODO: handle resize
            /* FIXME the offset is off by one */
            // keep a todo list in the task tree instead
            // TODOS are words, but TODOLIST is not a marker
            let value = 1
            """
        #expect(Self.lines(NoTodoComment(), source) == [1, 2])
    }
}
