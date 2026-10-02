import SwiftSyntax

/*
 No `try?` whose result is thrown away: `try? remove(file)` as a statement, or `_ = try? remove(file)`.

 The Swift form of "errors are not swallowed" (our TypeScript `no-floating-promises`). A `try?` whose
 optional is used is a legitimate way to say "absent if it fails". A `try?` whose result is discarded
 throws the error away without a word, and a failure nobody can see is the kind that ships. The repair is
 `do`/`catch` with a reason in the catch, even when the reason is "ignored, because …".

 Only the discarded form is matched. Of the 1,428 `try?` the design doc counted, most use their value,
 and those are not the rule's business.
 */
public struct NoDiscardedTryOptional: FileRule {
    public let name = "cohere-swift/no-discarded-try-optional"

    public init() {}

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let visitor = Visitor(viewMode: .sourceAccurate)
        visitor.walk(file.tree)
        return visitor.found.map { mark in
            file.finding(
                at: mark,
                rule: name,
                messageId: "discardedTryOptional",
                message: "This try? throws the error away and keeps nothing, so a failure here is invisible. Use do/catch, and say in the catch why the failure can be ignored if it can."
            )
        }
    }

    /* Collects the `?` of every `try?` that is a whole statement, or the right side of `_ =`. */
    final class Visitor: SyntaxVisitor {
        private(set) var found: [TokenSyntax] = []

        override func visit(_ node: TryExprSyntax) -> SyntaxVisitorContinueKind {
            guard let mark = node.questionOrExclamationMark, mark.tokenKind == .postfixQuestionMark else {
                return .visitChildren
            }
            if Self.isStatement(node) || Self.isDiscardAssignment(node) {
                found.append(mark)
            }
            return .visitChildren
        }

        /* `try? work()` on its own line: the expression is the whole code-block item. */
        static func isStatement(_ node: TryExprSyntax) -> Bool {
            node.parent?.is(CodeBlockItemSyntax.self) == true
        }

        /* `_ = try? work()`, which the unfolded tree spells as the sequence `_`, `=`, `try? work()`. */
        static func isDiscardAssignment(_ node: TryExprSyntax) -> Bool {
            guard let elements = node.parent?.as(ExprListSyntax.self), elements.count == 3 else { return false }
            let parts = Array(elements)
            return parts[0].is(DiscardAssignmentExprSyntax.self) && parts[1].is(AssignmentExprSyntax.self) && parts[2].id == node.id
        }
    }
}
