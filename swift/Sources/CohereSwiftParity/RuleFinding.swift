/* One finding from any tool, reduced to what the comparison reads: where, and under the tool's own rule id. */
struct RuleFinding: Hashable, Sendable {
    /* Relative to the package root, so the three tools' spellings of one path compare equal. */
    var file: String
    var line: Int
    var rule: String
}
