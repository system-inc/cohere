import Foundation
import SwiftSyntax

/*
 A run of five or more `//` lines becomes one block comment, with a fix that changes only the delimiters.

 Kirk's ruling: why-comments are `/* */` prose blocks, matching our TypeScript, enforced by the fixer. This
 is a port of `nexus/consistency-no-long-line-comment`, and it decides the same cases the same way. Four or
 fewer lines read fine as a stack. Past four, a stack of slashes stops reading as one thought and starts
 reading as a wall, with no cue where the passage ends.

 Swift's additions, each a decision rather than a config switch:
 - A run of `///` documentation lines becomes `/** */`, not `/* */`, so it stays a documentation comment
   for Xcode and the compiler.
 - Directives never join a run, the same way `eslint-` and `@ts-` lines do in TypeScript: `MARK:`,
   `swift-format-ignore`, `swiftlint:` and `sourcery:` only work as their own line comment, and folding
   one into a block silently turns it off.

 Comments are read from the syntax tree's trivia, never from a text scan, so a `//` inside a string literal
 is never mistaken for one. Only a comment alone on its line can join a run: a trailing comment shares its
 line with code, and replacing it would swallow the code before it.
 */
public struct NoLongLineComment: FileRule {
    public let name = "cohere-swift/no-long-line-comment"

    /* Four or fewer lines read fine as a stack; the block delimiters would be noise. The TypeScript rule's default, kept. */
    static let maximumLineCount = 4

    static let directivePrefixes = ["MARK:", "swift-format-ignore", "swiftlint:", "sourcery:", "TODO:", "FIXME:"]

    /* One comment alone on its line. Offsets are UTF-8 bytes into the file; the column is 0-based. */
    struct LineComment: Equatable {
        var start: Int
        var end: Int
        var line: Int
        var column: Int
        var text: String
        var isDocumentation: Bool
    }

    public init() {}

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        var findings: [FindingRecord] = []
        var run: [LineComment] = []

        func flush() {
            if let finding = report(run, in: file) {
                findings.append(finding)
            }
            run = []
        }

        for comment in Self.lineComments(in: file) {
            if let previous = run.last,
                previous.line + 1 != comment.line || previous.column != comment.column || previous.isDocumentation != comment.isDocumentation
            {
                flush()
            }
            run.append(comment)
        }
        flush()
        return findings
    }

    private func report(_ run: [LineComment], in file: ParsedFile) -> FindingRecord? {
        guard run.count > Self.maximumLineCount, let first = run.first, let last = run.last else {
            return nil
        }
        let start = file.locations.location(for: AbsolutePosition(utf8Offset: first.start))
        let end = file.locations.location(for: AbsolutePosition(utf8Offset: last.end))
        /*
         Either delimiter in the text makes the fold unsafe, so the run is reported without a fix and a person
         decides how to phrase it. A star-slash would close the block early, as in TypeScript. A slash-star is
         Swift's own hazard: Swift block comments nest, so a path ending in a slash and a star, written in a
         comment, opens a second block that never closes and the file stops parsing. Measured on ahraos-macos,
         where a comment naming the provider-proxy routes did exactly that. (This comment cannot quote the path:
         it is itself a block comment, and quoting it broke this file's build.)
         */
        let foldable = !run.contains { $0.text.contains("*/") || $0.text.dropFirst(2).contains("/*") }
        return FindingRecord(
            source: .rule,
            file: file.url.path,
            line: start.line,
            column: start.column,
            endLine: end.line,
            endColumn: end.column,
            severity: .error,
            rule: name,
            messageId: "longLineComment",
            message: "A run of five or more // lines should be a block comment. Past four lines a stack of slashes stops reading as one thought and starts reading as a wall, with no cue where the passage ends. This run is \(run.count) lines.",
            fixes: foldable ? [FindingRecord.Edit(start: first.start, end: last.end, text: Self.block(from: run))] : []
        )
    }

    /* The run rewritten as a block, keeping each line's text and the run's indentation; only the delimiters and the one space after them change. */
    static func block(from run: [LineComment]) -> String {
        let indent = String(repeating: " ", count: run.first?.column ?? 0)
        let opening = run.first?.isDocumentation == true ? "/**" : "/*"
        let prefix = run.first?.isDocumentation == true ? "///" : "//"
        var lines = [opening]
        for comment in run {
            var text = Substring(comment.text.dropFirst(prefix.count))
            if text.first == " " {
                text = text.dropFirst()
            }
            lines.append(text.trimmingCharacters(in: .whitespaces).isEmpty ? indent + " *" : indent + " * " + text)
        }
        lines.append(indent + " */")
        return lines.joined(separator: "\n")
    }

    /* Every `//` or `///` comment that sits alone on its line and is not a directive, in file order. */
    static func lineComments(in file: ParsedFile) -> [LineComment] {
        var comments: [LineComment] = []
        for token in file.tree.tokens(viewMode: .sourceAccurate) {
            /*
             A run sitting between two members of a chain (`.padding()`, comments, `.frame()`) is left as it is.
             swift-format 604 moves the member after a block comment back to the chain's starting indentation,
             which makes the code read as though the member belonged to a different expression, while `//` lines
             in the same place keep it. Found by @system_cohere_swift_ahraos_macos on 9 SwiftUI chains, and
             reproduced with swift-format alone. A fold the formatter then undoes would be a finding nobody can
             clear, so the rule does not raise it.
             */
            if token.tokenKind == .period, token.parent?.as(MemberAccessExprSyntax.self)?.base != nil {
                continue
            }
            var offset = token.position.utf8Offset
            /* Only leading trivia: a comment in trailing trivia shares its line with the code before it. */
            var aloneOnLine = token.position.utf8Offset == 0 || token.previousToken(viewMode: .sourceAccurate) == nil
            for piece in token.leadingTrivia {
                let length = piece.sourceLength.utf8Length
                switch piece {
                case .newlines, .carriageReturns, .carriageReturnLineFeeds:
                    aloneOnLine = true
                case .spaces, .tabs:
                    break
                case let .lineComment(text), let .docLineComment(text):
                    let isDocumentation: Bool
                    if case .docLineComment = piece { isDocumentation = true } else { isDocumentation = false }
                    let body = text.drop { $0 == "/" }.trimmingCharacters(in: .whitespaces)
                    let isDirective = directivePrefixes.contains { body.hasPrefix($0) }
                    if aloneOnLine && !isDirective {
                        let location = file.locations.location(for: AbsolutePosition(utf8Offset: offset))
                        comments.append(LineComment(
                            start: offset,
                            end: offset + length,
                            line: location.line,
                            column: location.column - 1,
                            text: text,
                            isDocumentation: isDocumentation
                        ))
                    }
                    aloneOnLine = false
                default:
                    aloneOnLine = false
                }
                offset += length
            }
        }
        return comments
    }
}
