/*
 What the fix and format phase did. The front door fills an `edit.Summary` from it and prints that
 summary's own line, so the Swift and TypeScript fix lines cannot drift apart.

 Formatting keeps its own counts, separate from fixes, because a formatter that declined every file and
 a formatter that found every file already correct otherwise print the same zero.
 */
public struct FixRecord: Codable, Equatable, Sendable {
    public var kind = "fix"
    public var filesConsidered: Int
    public var filesRewritten: Int
    public var fixesApplied: Int
    public var fixesRefused: Int
    public var refusalsByReason: [String: Int]
    public var filesReformatted: Int
    public var filesNotFormatted: Int
    public var notFormattedReasons: [String: Int]
    public var formatScope: String

    public init(
        filesConsidered: Int,
        filesRewritten: Int,
        fixesApplied: Int,
        fixesRefused: Int,
        refusalsByReason: [String: Int],
        filesReformatted: Int,
        filesNotFormatted: Int,
        notFormattedReasons: [String: Int],
        formatScope: String,
    ) {
        self.filesConsidered = filesConsidered
        self.filesRewritten = filesRewritten
        self.fixesApplied = fixesApplied
        self.fixesRefused = fixesRefused
        self.refusalsByReason = refusalsByReason
        self.filesReformatted = filesReformatted
        self.filesNotFormatted = filesNotFormatted
        self.notFormattedReasons = notFormattedReasons
        self.formatScope = formatScope
    }
}
