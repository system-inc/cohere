import Foundation
import SwiftSyntax

/*
 No all-caps words in comments: "don't shout" is a house standard. A file that already shouts makes the next writer
 come in braced and a little louder, until emphasis means nothing because everything has it, and "NEVER cache this"
 is usually a sentence hoping volume will carry a claim its reason should. The Swift form of
 `nexus/consistency-no-shouting`, held by the text rules corpus to one verdict in every engine (#4b337jk,
 `conformance/text_rules`, family `shouting`; #y8whqbe):
   - which words are acronyms is `policy/Shouting.json`, the one list every engine reads (`ShoutingVocabulary`);
   - what counts as code is the TypeScript and Go engines' masks, ported here byte for byte: fences, indented code
     blocks, backticks, double-quoted text and a quote that wraps a line, single-quoted tokens and capital phrases,
     `@example` blocks, and command lines. Code wears backticks or quotes, and inside them nothing is read;
   - a run of `//` lines, each on the line after the last, is one text (`CommentText`), so a fence or a backtick span
     across lines masks what it holds, and each line is read past its slashes and the one space after them, as go/doc
     reads a `//` line, so an indented code block is measured by the writer's indentation;
   - a block comment is read without its delimiters, as the TypeScript rule reads one (#gzwa8xr), so a command line
     in a one-line block comment opens its line and is masked as one behind a gutter is;
   - Swift's own notations are not prose: the generated-code marker, `// Code generated ... DO NOT EDIT.`, whose
     text an external convention fixes (only before the first token, where tools look for it), and Xcode's
     `// MARK:`, whose word is the editor's;
   - each finding covers one word, with its end.

 Comments only, never string literals, though the house standard covers strings too: the TypeScript and Go rules
 read comments alone, and the corpus rules one verdict, with no case for a string. A string is also often a value
 whose capitals belong to someone else (an HTTP method, a SQL keyword, an environment key), which the masks above
 cannot tell from a raised voice without the backticks a comment gives them.

 No fix, deliberately, as in TypeScript: lowercasing the word leaves the sentence that needed shouting still weak,
 and the repair is usually to state the reason instead, which a rule cannot write.
 */
public struct ConsistencyNoShouting: FileRule {
    public static let ruleName = "cohere-swift/consistency-no-shouting"
    public let name = ruleName
    public let origin = RuleOrigin.house
    public let upstreamName: String? = nil

    let vocabulary: ShoutingVocabulary

    /* The vocabulary is handed in, loaded by the pipeline before anything is checked, never looked up here. */
    public init(vocabulary: ShoutingVocabulary) {
        self.vocabulary = vocabulary
    }

    static let space = UInt8(ascii: " ")
    static let tab = UInt8(ascii: "\t")
    static let newline = UInt8(ascii: "\n")
    static let backtick = UInt8(ascii: "`")
    static let doubleQuote = UInt8(ascii: "\"")
    static let singleQuote = UInt8(ascii: "'")

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let source = Array(file.source.utf8)
        let comments = CommentText.comments(in: file)
        let notation = Self.notationOffsets(of: comments, in: file)
        var findings: [FindingRecord] = []
        for text in CommentText.texts(
            of: comments,
            source: source,
            droppingOneSpace: true,
            cuttingBlockDelimiters: true,
        ) {
            var masked = text.map(\.byte)
            /* Nothing is reported without two capitals in a row, so most texts skip the masking. */
            guard Self.hasAdjacentUppercaseLetters(masked) else { continue }
            for (index, entry) in text.enumerated() where notation.marker.contains(entry.offset) {
                masked[index] = Self.space
            }
            Self.maskCodeAndCommands(&masked)
            /*
             A notation word is blanked after the code masks, since blanking it first would leave its line opening with
             four spaces, which the indented code block mask reads as code, hiding the prose after it.
             */
            for (index, entry) in text.enumerated() where notation.words.contains(entry.offset) {
                masked[index] = Self.space
            }
            for word in Self.words(in: masked) {
                let spelled = String(decoding: masked[word], as: UTF8.self)
                guard vocabulary.isShouted(spelled) else { continue }
                let start = file.locations.location(for: AbsolutePosition(utf8Offset: text[word.lowerBound].offset))
                let end = file.locations.location(
                    for: AbsolutePosition(utf8Offset: text[word.upperBound - 1].offset + 1)
                )
                let message = RuleMessages.ConsistencyNoShouting.shoutingInComment(tokens: "\"\(spelled)\"")
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
                        messageId: message.id,
                        message: message.text,
                    )
                )
            }
        }
        return findings
    }

    /*
     The file offsets of Swift's own notations in its comments, which are never read: a generated-code marker before
     the first token, all of it, set aside before the code masks run as go/doc sets it aside; and the `MARK` of a line
     comment that opens `MARK:`, the word alone, since what follows it is prose.
     */
    static func notationOffsets(
        of comments: [CommentText.Comment],
        in file: ParsedFile,
    ) -> (marker: Set<Int>, words: Set<Int>) {
        let firstToken =
            file.tree.firstToken(viewMode: .sourceAccurate)?.positionAfterSkippingLeadingTrivia.utf8Offset
            ?? file.source.utf8.count
        var marker = Set<Int>()
        var words = Set<Int>()
        for comment in comments where comment.isLineComment {
            if comment.offset < firstToken, isGeneratedMarker(comment.bytes) {
                marker.formUnion(comment.offset..<(comment.offset + comment.bytes.count))
                continue
            }
            var index = 0
            while index < comment.bytes.count, comment.bytes[index] == UInt8(ascii: "/") {
                index += 1
            }
            while index < comment.bytes.count, comment.bytes[index] == space || comment.bytes[index] == tab {
                index += 1
            }
            if comment.bytes[index...].starts(with: Array("MARK:".utf8)) {
                words.formUnion((comment.offset + index)..<(comment.offset + index + 4))
            }
        }
        return (marker, words)
    }

    /* Go's `^// Code generated .* DO NOT EDIT\.$`, the line go.dev/s/generatedcode says marks a file as generated. */
    static func isGeneratedMarker(_ bytes: [UInt8]) -> Bool {
        let opening = Array("// Code generated ".utf8)
        let closing = Array(" DO NOT EDIT.".utf8)
        return bytes.count >= opening.count + closing.count && bytes.starts(with: opening)
            && bytes.suffix(closing.count).elementsEqual(closing) && !bytes.contains(newline)
    }

    /* Whether two uppercase letters appear in a row: the shortest thing that can be reported, so a superset of it. */
    static func hasAdjacentUppercaseLetters(_ bytes: [UInt8]) -> Bool {
        var previousWasUppercase = false
        for byte in bytes {
            let isUppercase = byte >= UInt8(ascii: "A") && byte <= UInt8(ascii: "Z")
            if isUppercase && previousWasUppercase {
                return true
            }
            previousWasUppercase = isUppercase
        }
        return false
    }

    /* A byte an uppercase word is made of past its first letter: `[A-Z0-9_]`. */
    static func isUppercaseWordCharacter(_ byte: UInt8) -> Bool {
        (byte >= UInt8(ascii: "A") && byte <= UInt8(ascii: "Z"))
            || (byte >= UInt8(ascii: "0") && byte <= UInt8(ascii: "9")) || byte == UInt8(ascii: "_")
    }

    /*
     Each uppercase word in masked text, as the range of its bytes: Go's `\b[A-Z][A-Z0-9_]*\b`, which matches only a
     whole run of ASCII word characters, so a run is a word when it opens with a capital and holds no lowercase.
     */
    static func words(in masked: [UInt8]) -> [Range<Int>] {
        var words: [Range<Int>] = []
        var index = 0
        while index < masked.count {
            guard CommentText.isWordCharacter(masked[index]) else {
                index += 1
                continue
            }
            var end = index
            while end < masked.count, CommentText.isWordCharacter(masked[end]) {
                end += 1
            }
            if masked[index] >= UInt8(ascii: "A"), masked[index] <= UInt8(ascii: "Z"),
                masked[index..<end].allSatisfy(isUppercaseWordCharacter)
            {
                words.append(index..<end)
            }
            index = end
        }
        return words
    }

    /*
     Blanks everything that is code, in the order and the shapes of the shared Go masks (`MaskCodeAndCommands`), so
     nothing inside it is read. Every masked byte becomes a space, one per byte, keeping each offset; a mask that can
     span lines keeps its line breaks, so the masks after it still see the lines.
     */
    static func maskCodeAndCommands(_ text: inout [UInt8]) {
        maskFences(&text)
        maskIndentedCodeBlocks(&text)
        maskBacktickSpans(&text)
        maskDoubleQuoted(&text)
        maskWrappedDoubleQuotes(&text)
        maskSingleQuotedTokens(&text)
        maskSingleQuotedCapitalPhrases(&text)
        maskExamples(&text)
        maskCommandLines(&text)
    }

    static func blank(_ text: inout [UInt8], _ range: Range<Int>) {
        for index in range where text[index] != newline {
            text[index] = space
        }
    }

    /* The offset of the next `needle` at or after `start`, if there is one. */
    static func find(_ needle: [UInt8], in text: [UInt8], from start: Int) -> Int? {
        guard needle.count <= text.count, start <= text.count - needle.count else { return nil }
        return (start...(text.count - needle.count)).first { text[$0..<($0 + needle.count)].elementsEqual(needle) }
    }

    /* The lines of a text as ranges of it, line breaks left out. */
    static func lines(of text: [UInt8]) -> [Range<Int>] {
        var lines: [Range<Int>] = []
        var start = 0
        for (index, byte) in text.enumerated() where byte == newline {
            lines.append(start..<index)
            start = index + 1
        }
        lines.append(start..<text.count)
        return lines
    }

    /* "(?s)```.*?```": three backticks and the next three after them, anywhere on any line. An unclosed fence is not one. */
    static func maskFences(_ text: inout [UInt8]) {
        let fence = Array("```".utf8)
        var start = 0
        while let opening = find(fence, in: text, from: start) {
            guard let closing = find(fence, in: text, from: opening + 3) else { return }
            blank(&text, opening..<(closing + 3))
            start = closing + 3
        }
    }

    /*
     An indented code block, the markdown form of a fence: lines indented four spaces or a tab, opened after a blank
     line or at the start, running until a line that is not indented. A block comment's gutter, `*` and one space or
     tab, is set aside before the indentation is measured. An indented line straight after prose continues the
     paragraph and is read.
     */
    static func maskIndentedCodeBlocks(_ text: inout [UInt8]) {
        var previousIsBlank = true
        var insideBlock = false
        for line in lines(of: text) {
            var content = line.lowerBound
            while content < line.upperBound, text[content] == space || text[content] == tab {
                content += 1
            }
            if content < line.upperBound, text[content] == UInt8(ascii: "*"),
                content + 1 == line.upperBound || text[content + 1] == space || text[content + 1] == tab
            {
                content = min(content + 2, line.upperBound)
            }
            else {
                content = line.lowerBound
            }
            if text[content..<line.upperBound].allSatisfy(isWhitespace) {
                previousIsBlank = true
                continue
            }
            let isIndented =
                text[content..<line.upperBound].starts(with: [space, space, space, space]) || text[content] == tab
            insideBlock = isIndented && (insideBlock || previousIsBlank)
            previousIsBlank = false
            if insideBlock {
                blank(&text, line)
            }
        }
    }

    /* Whitespace as a line's blankness is judged, the ASCII part of JavaScript's set. */
    static func isWhitespace(_ byte: UInt8) -> Bool {
        byte == space || byte == tab || byte == UInt8(ascii: "\r") || byte == 0x0B || byte == 0x0C
    }

    /* "`[^`]*`": a backtick and the next one, across lines. A backtick with no partner is literal. */
    static func maskBacktickSpans(_ text: inout [UInt8]) {
        var start = 0
        while let opening = text[start...].firstIndex(of: backtick) {
            guard let closing = text[(opening + 1)...].firstIndex(of: backtick) else { return }
            blank(&text, opening..<(closing + 1))
            start = closing + 1
        }
    }

    /* `"[^"\n]*"`: a double-quoted phrase on one line, whose words are a value quoted rather than the writer's voice. */
    static func maskDoubleQuoted(_ text: inout [UInt8]) {
        var start = 0
        while let opening = text[start...].firstIndex(of: doubleQuote) {
            var closing = opening + 1
            while closing < text.count, text[closing] != doubleQuote, text[closing] != newline {
                closing += 1
            }
            guard closing < text.count, text[closing] == doubleQuote else {
                start = opening + 1
                continue
            }
            blank(&text, opening..<(closing + 1))
            start = closing + 1
        }
    }

    /*
     A double-quoted phrase a comment wrapped onto the next line. After the one-line pass each line has at most one
     quote left, and only those leftovers pair, a line's with the very next line's, so a stray inch mark can only blank
     text and never unblank what the one-line pass masked.
     */
    static func maskWrappedDoubleQuotes(_ text: inout [UInt8]) {
        guard text.contains(newline), text.count(where: { $0 == doubleQuote }) >= 2 else { return }
        let lines = lines(of: text)
        for index in 0..<(lines.count - 1) {
            guard let opening = text[lines[index]].firstIndex(of: doubleQuote),
                let closing = text[lines[index + 1]].firstIndex(of: doubleQuote)
            else { continue }
            blank(&text, opening..<lines[index].upperBound)
            /* The closing quote is blanked with the rest, so it cannot also open a pair with the line after it. */
            blank(&text, lines[index + 1].lowerBound..<(closing + 1))
        }
    }

    /* A byte of a single-quoted token: `[A-Za-z0-9_.\-/]`. */
    static func isQuotedTokenCharacter(_ byte: UInt8) -> Bool {
        CommentText.isWordCharacter(byte) || byte == UInt8(ascii: ".") || byte == UInt8(ascii: "-")
            || byte == UInt8(ascii: "/")
    }

    /* `'[A-Za-z0-9_.\-/]+'`: a single-quoted token with no space inside, so an apostrophe in prose never opens one. */
    static func maskSingleQuotedTokens(_ text: inout [UInt8]) {
        var start = 0
        while let opening = text[start...].firstIndex(of: singleQuote) {
            var closing = opening + 1
            while closing < text.count, isQuotedTokenCharacter(text[closing]) {
                closing += 1
            }
            guard closing > opening + 1, closing < text.count, text[closing] == singleQuote else {
                start = opening + 1
                continue
            }
            blank(&text, opening..<(closing + 1))
            start = closing + 1
        }
    }

    /* A byte a single-quoted capital phrase opens and closes with, `[A-Z0-9_]`, and one it may hold inside. */
    static func isCapitalPhraseCharacter(_ byte: UInt8, inside: Bool) -> Bool {
        isUppercaseWordCharacter(byte) || (inside && Array(" .,:=-/".utf8).contains(byte))
    }

    /*
     `'[A-Z0-9_][A-Z0-9_ .,:=\-/]*[A-Z0-9_]'`, a capital phrase set in single quotes such as 'ON CONFLICT DO NOTHING',
     where neither quote is glued to a letter or digit outside it, so a possessive cannot open one.
     */
    static func maskSingleQuotedCapitalPhrases(_ text: inout [UInt8]) {
        guard text.count(where: { $0 == singleQuote }) >= 2 else { return }
        var matches: [Range<Int>] = []
        var start = 0
        while let opening = text[start...].firstIndex(of: singleQuote) {
            var closing = opening + 1
            while closing < text.count, isCapitalPhraseCharacter(text[closing], inside: true) {
                closing += 1
            }
            guard closing < text.count, text[closing] == singleQuote, closing - opening - 1 >= 2,
                isCapitalPhraseCharacter(text[opening + 1], inside: false),
                isCapitalPhraseCharacter(text[closing - 1], inside: false)
            else {
                start = opening + 1
                continue
            }
            matches.append(opening..<(closing + 1))
            start = closing + 1
        }
        let original = text
        for match in matches {
            if match.lowerBound > 0, isAsciiLetterOrDigit(original[match.lowerBound - 1]) {
                continue
            }
            if match.upperBound < original.count, isAsciiLetterOrDigit(original[match.upperBound]) {
                continue
            }
            blank(&text, match)
        }
    }

    static func isAsciiLetterOrDigit(_ byte: UInt8) -> Bool {
        CommentText.isWordCharacter(byte) && byte != UInt8(ascii: "_")
    }

    /* Go's `\s`: space, tab, line feed, form feed and carriage return. */
    static func isRegexSpace(_ byte: UInt8) -> Bool {
        byte == space || byte == tab || byte == newline || byte == 0x0C || byte == UInt8(ascii: "\r")
    }

    /* Where a line's text starts past a doc comment's gutter, `^\s*\*?\s?`. */
    static func pastGutter(_ text: [UInt8], _ line: Range<Int>) -> Int {
        var index = line.lowerBound
        while index < line.upperBound, isRegexSpace(text[index]) {
            index += 1
        }
        if index < line.upperBound, text[index] == UInt8(ascii: "*") {
            index += 1
        }
        if index < line.upperBound, isRegexSpace(text[index]) {
            index += 1
        }
        return index
    }

    /* The lines under an `@example` tag, which are code whatever they look like, up to the next tag. */
    static func maskExamples(_ text: inout [UInt8]) {
        guard text.contains(UInt8(ascii: "@")) else { return }
        var insideExample = false
        for line in lines(of: text) {
            let content = text[pastGutter(text, line)..<line.upperBound]
            let tag = Array("@example".utf8)
            if content.starts(with: tag),
                content.count == tag.count || !CommentText.isWordCharacter(content[content.startIndex + tag.count])
            {
                insideExample = true
                blank(&text, line)
                continue
            }
            if insideExample, content.first == UInt8(ascii: "@"), content.count > 1,
                CommentText.isWordCharacter(content[content.startIndex + 1])
            {
                insideExample = false
            }
            if insideExample {
                blank(&text, line)
            }
        }
    }

    /* The commands a line can open with: `$`, or one of these and a space. */
    static let commandStarters = ["ahra", "git", "pnpm", "npm", "sqlite3", "curl"].map { Array($0.utf8) }

    /* A line that is plainly a command to run, `^\s*(\$|ahra\s|git\s|pnpm\s|npm\s|sqlite3\s|curl\s|s\s+c\b)` past the gutter. */
    static func maskCommandLines(_ text: inout [UInt8]) {
        for line in lines(of: text) {
            var index = pastGutter(text, line)
            while index < line.upperBound, isRegexSpace(text[index]) {
                index += 1
            }
            let rest = text[index..<line.upperBound]
            let isCommand =
                rest.first == UInt8(ascii: "$")
                || commandStarters.contains { starter in
                    rest.starts(with: starter) && rest.count > starter.count
                        && isRegexSpace(rest[rest.startIndex + starter.count])
                } || isStructureCommand(rest)
            if isCommand {
                blank(&text, line)
            }
        }
    }

    /* `s\s+c\b`: the structure command, `s c`, with any run of spaces between. */
    static func isStructureCommand(_ rest: ArraySlice<UInt8>) -> Bool {
        guard rest.first == UInt8(ascii: "s") else { return false }
        var index = rest.startIndex + 1
        guard index < rest.endIndex, isRegexSpace(rest[index]) else { return false }
        while index < rest.endIndex, isRegexSpace(rest[index]) {
            index += 1
        }
        guard index < rest.endIndex, rest[index] == UInt8(ascii: "c") else { return false }
        return index + 1 == rest.endIndex || !CommentText.isWordCharacter(rest[index + 1])
    }
}
