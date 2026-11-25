import SwiftSyntax

/*
 The comments of a file as the text rules read them (#4b337jk, `conformance/text_rules`): every comment, leading
 trivia and trailing alike, and the texts they make, where a run of `//` lines, each on the line after the last, is
 one text. So a fence or a backtick span opened on one line and closed on a later one masks what it holds, in every
 rule that reads comments. Shared by `Todo` and `ConsistencyNoShouting`, which judge different words in the same
 texts.

 A text is its bytes paired with each byte's offset in the file, so a rule that finds a word reports it where it
 sits, whatever was left out around it.
 */
enum CommentText {
    /* One comment's bytes as they sit in the file, by their offset there. */
    struct Comment {
        var offset: Int
        var bytes: [UInt8]
        var isLineComment: Bool
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

     `droppingOneSpace` leaves out the one space after a line comment's slashes too, as go/doc reads a `//` line, so a
     rule that measures a line's indentation measures the writer's and not the space every comment opens with.

     `cuttingBlockDelimiters` leaves out a block comment's opening slash and star and its closing star and slash, as
     ESLint's comment value and the TypeScript rule read one (#gzwa8xr, #xfa3q6w): a mask anchored at a line's start,
     such as a command line's, then sees a one-line block comment's first word where it would have seen the slash. Only
     the outer pair is cut, so a comment nested inside one is read as it stands. They are cut, never blanked to
     spaces: a blanked opening is two spaces of indentation, so a comment opened with two spaces after it would read
     as an indented code block. Every byte keeps its own offset, so nothing has to be added back.
     */
    static func texts(
        of comments: [Comment],
        source: [UInt8],
        droppingOneSpace: Bool = false,
        cuttingBlockDelimiters: Bool = false,
    ) -> [[(byte: UInt8, offset: Int)]] {
        var texts: [[(byte: UInt8, offset: Int)]] = []
        var previousLineCommentEnd: Int?
        for comment in comments {
            var bytes: [(byte: UInt8, offset: Int)] = []
            var skip = 0
            var bodyEnd = comment.bytes.count
            if comment.isLineComment {
                while skip < comment.bytes.count, comment.bytes[skip] == UInt8(ascii: "/") {
                    skip += 1
                }
                if droppingOneSpace, skip < comment.bytes.count, comment.bytes[skip] == UInt8(ascii: " ") {
                    skip += 1
                }
            }
            else if cuttingBlockDelimiters {
                if comment.bytes.starts(with: Array("/*".utf8)) {
                    skip = 2
                }
                /* An unterminated comment at the end of a file has no closing pair to cut. */
                if bodyEnd - skip >= 2, comment.bytes[(bodyEnd - 2)..<bodyEnd].elementsEqual(Array("*/".utf8)) {
                    bodyEnd -= 2
                }
            }
            for index in skip..<bodyEnd {
                bytes.append((comment.bytes[index], comment.offset + index))
            }
            if comment.isLineComment, let end = previousLineCommentEnd, end <= comment.offset,
                joinsARun(source[end..<comment.offset])
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

    /* A byte that continues a word: an ASCII letter, a digit or `_`. A non-ASCII byte ends one, as Go's `\\b` reads it. */
    static func isWordCharacter(_ byte: UInt8) -> Bool {
        (byte >= UInt8(ascii: "a") && byte <= UInt8(ascii: "z"))
            || (byte >= UInt8(ascii: "A") && byte <= UInt8(ascii: "Z"))
            || (byte >= UInt8(ascii: "0") && byte <= UInt8(ascii: "9")) || byte == UInt8(ascii: "_")
    }
}
