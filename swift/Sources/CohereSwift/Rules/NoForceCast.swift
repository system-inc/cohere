import SwiftSyntax

/*
 No force cast: `value as! Type`. A failed cast becomes a crash. The repair is `as?` with a branch for the
 value that is not that type, which states what the author expects and what happens otherwise.

 The engine's tree is not operator-folded, so every cast parses as `UnresolvedAsExprSyntax` inside a
 sequence, never as `AsExprSyntax`. A probe that looked only for `AsExprSyntax` counted zero force casts in
 ahraos-macos, which was the tree's shape talking, not the code. Only the unfolded form is visited: a
 mutation sweep showed a branch for the folded form could not change any answer on this tree, and a branch
 nothing can reach is a branch nobody tests. If the engine ever folds its tree, this rule changes with it.

 Matches SwiftLint's `force_cast`.
 */
public struct NoForceCast: FileRule {
    public let name = "cohere-swift/no-force-cast"

    public init() {}

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let visitor = Visitor(viewMode: .sourceAccurate)
        visitor.walk(file.tree)
        return visitor.found.map { mark in
            file.finding(
                at: mark,
                rule: name,
                messageId: "forceCast",
                message: "as! crashes the process when the value is not that type. Use as? and say what happens when it is not."
            )
        }
    }

    /* Collects the `!` of every `as!`. */
    final class Visitor: SyntaxVisitor {
        private(set) var found: [TokenSyntax] = []

        override func visit(_ node: UnresolvedAsExprSyntax) -> SyntaxVisitorContinueKind {
            record(node.questionOrExclamationMark)
            return .visitChildren
        }

        private func record(_ mark: TokenSyntax?) {
            if let mark, mark.tokenKind == .exclamationMark {
                found.append(mark)
            }
        }
    }
}
