import Foundation
import SwiftSyntax

/*
 No `TODO`, `FIXME` or `XXX` comments: unfinished work belongs in the task tree, where it has an owner, a
 priority and a place in a plan, not in a comment that is read only when someone happens to open the file.

 The Swift form of the house warning-comment rule, held by the text rules corpus to one verdict in every
 engine (#4b337jk, `conformance/textrules`, family `warning-comments`; #47s5j9h):
   - the words in capitals only, since a lowercase "todo" is ordinary English (an app's to-do list);
   - each a whole word, where an ASCII letter, a digit or `_` continues it, so `TODO_LIST` and `TODO2` are
     names rather than markers;
   - anywhere in any comment, leading or trailing, on any line of a block comment;
   - never inside backticks or a fenced block, where the word is quoted rather than written;
   - the finding covers the word.
 A run of `//` lines, each on the line after the last, is one text, so a fence or a backtick span opened on one
 line and closed on a later one masks what it holds.
 */
public struct Todo: FileRule {
    public let name = "cohere-swift/todo"
    public let origin = RuleOrigin.swiftLint
    public let upstreamName: String? = "todo"

    static let markers = ["TODO", "FIXME", "XXX"]

    public init() {}

    /* One comment's bytes as they sit in the file, by their offset there. */
    struct Comment {
        var offset: Int
        var bytes: [UInt8]
        var isLineComment: Bool
    }

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let source = Array(file.source.utf8)
        var findings: [FindingRecord] = []
        for text in Self.texts(of: Self.comments(in: file), source: source) {
            for (offset, marker) in Self.markers(in: text) {
                let start = file.locations.location(for: AbsolutePosition(utf8Offset: offset))
                let end = file.locations.location(for: AbsolutePosition(utf8Offset: offset + marker.utf8.count))
                findings.append(
                    FindingRecord(
                        source: .rule,
                        file: file.url.path,
                        line: start.line,
                        column: start.column,
                        endLine: end.line,
                        endColumn: end.column,
                        severity: .error,
                        rule: name,
                        messageId: "todoComment",
                        message:
                            "A \(marker) comment is unfinished work that only someone opening this file will see. Put it in the task tree, where it has an owner and a priority, and delete the comment.",
                    )
                )
            }
        }
        return findings
    }

    /* Every comment in the file, leading trivia and trailing alike, in the order they appear. */
    static func comments(in file: ParsedFile) -> [Comment] {
        var comments: [Comment] = []
        for token in file.tree.tokens(viewMode: .sourceAccurate) {
            var offset = token.position.utf8Offset
            for piece in token.leadingTrivia {
                if let comment = comment(piece, at: offset) {
                    comments.append(comment)
                }
                offset += piece.sourceLength.utf8Length
            }
            offset = token.endPositionBeforeTrailingTrivia.utf8Offset
            for piece in token.trailingTrivia {
                if let comment = comment(piece, at: offset) {
                    comments.append(comment)
                }
                offset += piece.sourceLength.utf8Length
            }
        }
        return comments
    }

    static func comment(_ piece: TriviaPiece, at offset: Int) -> Comment? {
        switch piece {
            case let .lineComment(text), let .docLineComment(text):
                return Comment(offset: offset, bytes: Array(text.utf8), isLineComment: true)
            case let .blockComment(text), let .docBlockComment(text):
                return Comment(offset: offset, bytes: Array(text.utf8), isLineComment: false)
            default:
                return nil
        }
    }

    /*
     The texts the words are looked for in, each as its bytes paired with their offsets in the file: a block comment
     alone, and a run of line comments together when only spaces, tabs and one line break stand between each and the
     next. The comment markers of a line comment are left out; a line break joins the lines of a run.
     */
    static func texts(of comments: [Comment], source: [UInt8]) -> [[(byte: UInt8, offset: Int)]] {
        var texts: [[(byte: UInt8, offset: Int)]] = []
        var previousLineCommentEnd: Int?
        for comment in comments {
            var bytes: [(byte: UInt8, offset: Int)] = []
            var skip = 0
            if comment.isLineComment {
                while skip < comment.bytes.count, comment.bytes[skip] == UInt8(ascii: "/") {
                    skip += 1
                }
            }
            for index in skip..<comment.bytes.count {
                bytes.append((comment.bytes[index], comment.offset + index))
            }
            if comment.isLineComment, let end = previousLineCommentEnd, end <= comment.offset,
                Self.joinsARun(source[end..<comment.offset])
            {
                texts[texts.count - 1].append((UInt8(ascii: "\n"), end))
                texts[texts.count - 1].append(contentsOf: bytes)
            }
            else {
                texts.append(bytes)
            }
            previousLineCommentEnd = comment.isLineComment ? comment.offset + comment.bytes.count : nil
        }
        return texts
    }

    /* Whether the bytes between two line comments put the second on the line straight after the first. */
    static func joinsARun(_ between: ArraySlice<UInt8>) -> Bool {
        var lineBreaks = 0
        var index = between.startIndex
        while index < between.endIndex {
            switch between[index] {
                case UInt8(ascii: " "), UInt8(ascii: "\t"):
                    break
                case UInt8(ascii: "\r"):
                    lineBreaks += 1
                    if index + 1 < between.endIndex, between[index + 1] == UInt8(ascii: "\n") {
                        index += 1
                    }
                case UInt8(ascii: "\n"):
                    lineBreaks += 1
                default:
                    return false
            }
            index += 1
        }
        return lineBreaks == 1
    }

    /* Each warning word written in a text, with its offset in the file, after quoted text is masked. */
    static func markers(in text: [(byte: UInt8, offset: Int)]) -> [(offset: Int, marker: String)] {
        var masked = [Bool](repeating: false, count: text.count)
        maskFences(in: text, masked: &masked)
        maskBacktickSpans(in: text, masked: &masked)
        var found: [(offset: Int, marker: String)] = []
        let bytes = text.map(\.byte)
        for marker in markers {
            let word = Array(marker.utf8)
            guard word.count <= bytes.count else { continue }
            for start in 0...(bytes.count - word.count) where Array(bytes[start..<(start + word.count)]) == word {
                let end = start + word.count
                guard !masked[start..<end].contains(true),
                    start == 0 || !isWordCharacter(bytes[start - 1]),
                    end == bytes.count || !isWordCharacter(bytes[end])
                else { continue }
                found.append((text[start].offset, marker))
            }
        }
        return found.sorted { $0.offset < $1.offset }
    }

    /*
     A line whose text, past spaces, tabs and a block comment's `*` decoration, opens with three backticks opens or
     closes a fence, and it and every line inside are masked. A fence left open runs to the text's end.
     */
    static func maskFences(in text: [(byte: UInt8, offset: Int)], masked: inout [Bool]) {
        var inside = false
        var lineStart = 0
        while lineStart < text.count {
            var lineEnd = lineStart
            while lineEnd < text.count, text[lineEnd].byte != UInt8(ascii: "\n") {
                lineEnd += 1
            }
            var first = lineStart
            while first < lineEnd,
                [UInt8(ascii: " "), UInt8(ascii: "\t"), UInt8(ascii: "*")].contains(text[first].byte)
            {
                first += 1
            }
            let opensFence =
                lineEnd - first >= 3 && text[first..<(first + 3)].allSatisfy { $0.byte == UInt8(ascii: "`") }
            if opensFence || inside {
                for index in lineStart..<lineEnd {
                    masked[index] = true
                }
            }
            if opensFence {
                inside.toggle()
            }
            lineStart = lineEnd + 1
        }
    }

    /* A backtick and the next one not already masked, and all between, are masked. A backtick with no partner is literal. */
    static func maskBacktickSpans(in text: [(byte: UInt8, offset: Int)], masked: inout [Bool]) {
        var index = 0
        while index < text.count {
            guard !masked[index], text[index].byte == UInt8(ascii: "`") else {
                index += 1
                continue
            }
            var close = index + 1
            while close < text.count, masked[close] || text[close].byte != UInt8(ascii: "`") {
                close += 1
            }
            guard close < text.count else { return }
            for masking in index...close {
                masked[masking] = true
            }
            index = close + 1
        }
    }

    /* A byte that continues a word: an ASCII letter, a digit or `_`. A non-ASCII byte ends one, as Go's `\\b` reads it. */
    static func isWordCharacter(_ byte: UInt8) -> Bool {
        (byte >= UInt8(ascii: "a") && byte <= UInt8(ascii: "z"))
            || (byte >= UInt8(ascii: "A") && byte <= UInt8(ascii: "Z"))
            || (byte >= UInt8(ascii: "0") && byte <= UInt8(ascii: "9")) || byte == UInt8(ascii: "_")
    }
}
