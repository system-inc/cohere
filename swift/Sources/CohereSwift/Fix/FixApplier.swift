/*
 Applies rule fixes to a file's text.

 Edits are UTF-8 byte ranges into the text the rules read, applied back to front so an earlier edit never
 moves a later one. Two edits that overlap cannot both be right about the same bytes, so the first one wins
 and the other is refused and counted. It is not dropped silently: the fix line reports refusals by reason,
 so a fixer that keeps losing to another is visible rather than inert.
 */
enum FixApplier {
    /* The rewritten text and what happened to every edit offered. */
    struct Result: Equatable {
        var text: String
        var applied: Int
        var refusedOverlapping: Int
        var refusedInvalidRange: Int
    }

    static func apply(_ edits: [FindingRecord.Edit], to text: String) -> Result {
        var bytes = Array(text.utf8)
        var applied = 0
        var refusedOverlapping = 0
        var refusedInvalidRange = 0

        /* Accepted in source order, so "first one wins" means the earliest in the file, the same every run. */
        var accepted: [FindingRecord.Edit] = []
        for edit in edits.sorted(by: { ($0.start, $0.end) < ($1.start, $1.end) }) {
            guard edit.start >= 0, edit.start <= edit.end, edit.end <= bytes.count else {
                refusedInvalidRange += 1
                continue
            }
            if let previous = accepted.last, edit.start < previous.end {
                refusedOverlapping += 1
                continue
            }
            accepted.append(edit)
        }
        for edit in accepted.reversed() {
            bytes.replaceSubrange(edit.start..<edit.end, with: Array(edit.text.utf8))
            applied += 1
        }
        return Result(
            text: String(decoding: bytes, as: UTF8.self),
            applied: applied,
            refusedOverlapping: refusedOverlapping,
            refusedInvalidRange: refusedInvalidRange,
        )
    }
}
