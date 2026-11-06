import Foundation
import SwiftSyntax

/*
 A file holds at most 2,000 lines.

 Kirk's ruling (2026-10-03, #r3hfpe8). The size of a file is what a reader pays for, not how many types it
 holds: a file with one type and a fifteen-line helper beside it is easy to hold, and a 2,426-line file with
 one type is not. So size is checked on its own. A file past the limit has seams in it, and the repair is to
 split along them: an extension per concern (`Type+Purpose.swift`), or a type of its own (the orbit camera
 out of a 2,241-line viewport, for one).

 The threshold is Kirk's: 2,000. The line counts of every owned file in the two Swift repos on 2026-10-03
 (#r3hfpe8) put it in context. Of 1,469 files the median was about 100 lines; 141 were over 400 (SwiftLint's
 default), 20 over 1,000, 6 over 1,500 and 3 over 2,000. The limit reports only the files no reader can hold,
 rather than every substantial view or service, which a lower limit would turn into splits made to satisfy
 a number.

 Lines are counted the way SwiftLint's `file_length` counts them in its default configuration: every line,
 comments and blank lines included, with a final line break ending the last line rather than starting another.
 A line ends where Swift's grammar ends one, at a line feed, a carriage return and line feed, or a lone carriage
 return. A comment is part of what a reader has to hold, and a count that skipped comments would reward moving
 explanation out of the file, the opposite of what the house wants.

 The finding covers the excess, from the first line past the limit to the end of the file, the span every engine
 gives in the text rules corpus (#4b337jk, family `long-file`; #7aa7d3y). SwiftLint reports at the last line.
 */
public struct FileLength: FileRule {
    public let name = "cohere-swift/file-length"
    public let origin = RuleOrigin.swiftLint
    public let upstreamName: String? = "file_length"

    /* The house limit, and the most a project's settings may set: a `max` above it would be an allowance (`RuleConfiguration`). */
    public static let maximumLines = 2_000

    /* The limit this run judges by: the house's, or a stricter `max` from the project's settings. */
    public let maximum: Int

    public init(maximum: Int = FileLength.maximumLines) {
        self.maximum = maximum
    }

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        /* Kirk's convention (2026-10-03), shared with TypeScript: a generated file is named `*.generated.swift`, gets every other rule and the formatter, and is exempt from this one alone, because its length is the generator's, not a reader's. */
        guard !file.url.lastPathComponent.hasSuffix(".generated.swift") else { return [] }
        let starts = Self.lineStarts(of: file.source)
        let lines = Self.lineCount(of: file.source)
        guard lines > maximum else { return [] }
        let start = file.locations.location(for: AbsolutePosition(utf8Offset: starts[maximum]))
        let end = file.locations.location(for: AbsolutePosition(utf8Offset: file.source.utf8.count))
        return [
            FindingRecord(
                source: .rule,
                file: file.url.path,
                line: start.line,
                column: start.column,
                endLine: end.line,
                endColumn: end.column,
                severity: .error,
                rule: name,
                messageId: "tooManyLines",
                message:
                    "This file is \(lines) lines, over the \(maximum) a file may hold. Split it along its seams, an extension per concern in Type+Purpose.swift or a helper type in its own file, so each part is one a reader can hold whole.",
            )
        ]
    }

    /* The lines of the source: one per line break, plus one for a last line that does not end in one. */
    static func lineCount(of source: String) -> Int {
        let starts = lineStarts(of: source)
        return starts.last == source.utf8.count ? starts.count - 1 : starts.count
    }

    /*
     The byte offset each line starts at, the first at 0. A line feed, a carriage return and line feed, and a lone
     carriage return each end a line once, as Swift's grammar reads them, so the offset after the last break is
     listed even when the source ends there.
     */
    static func lineStarts(of source: String) -> [Int] {
        let bytes = Array(source.utf8)
        var starts = [0]
        var index = 0
        while index < bytes.count {
            if bytes[index] == UInt8(ascii: "\r"), index + 1 < bytes.count, bytes[index + 1] == UInt8(ascii: "\n") {
                index += 1
            }
            if bytes[index] == UInt8(ascii: "\n") || bytes[index] == UInt8(ascii: "\r") {
                starts.append(index + 1)
            }
            index += 1
        }
        return starts
    }
}
