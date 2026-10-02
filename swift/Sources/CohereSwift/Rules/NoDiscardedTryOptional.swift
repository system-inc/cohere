import SwiftSyntax

/*
 No `try?` whose result is thrown away: `try? remove(file)` as a statement, or `_ = try? remove(file)`.

 The Swift form of "errors are not swallowed" (our TypeScript `no-floating-promises`). A `try?` whose
 optional is used is a legitimate way to say "absent if it fails". A `try?` whose result is discarded
 throws the error away without a word, and a failure nobody can see is the kind that ships. The repair is
 `do`/`catch` with a reason in the catch, even when the reason is "ignored, because …".

 Only the discarded form is matched. Of the 1,428 `try?` the design doc counted, most use their value,
 and those are not the rule's business.

 A `try?` that is the only statement of a body is often that body's value, not a discard, and syntax says
 so in most places: a function or getter with a return type, a computed property, an `if` or `switch` used
 as an expression. Those are skipped. A closure's sole `try?` is skipped too, because syntax cannot tell
 `compactMap { try? fit($0) }` (a value) from `Task { try? await Task.sleep(for: delay) }` (a discard):
 `Task.detached { try? read() }.value` shows even the callee does not decide. The Void case is a known
 miss until the typed tier can read the closure's type. Raised by @system_cohere_swift_ahraos_presence,
 measured at about 30 of Presence's 515 findings.
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

        /* `try? work()` on its own line: the expression is the whole code-block item, and not the value of its body. */
        static func isStatement(_ node: TryExprSyntax) -> Bool {
            guard let item = node.parent?.as(CodeBlockItemSyntax.self) else { return false }
            return !isImplicitValue(item)
        }

        /* Whether the item is the sole statement of a body that returns it: the implicit-return positions. */
        static func isImplicitValue(_ item: CodeBlockItemSyntax) -> Bool {
            guard let list = item.parent?.as(CodeBlockItemListSyntax.self), list.count == 1, let owner = list.parent else { return false }
            if owner.is(ClosureExprSyntax.self) || owner.is(AccessorBlockSyntax.self) {
                return true
            }
            if let switchCase = owner.as(SwitchCaseSyntax.self) {
                return switchCase.parent?.parent.map(isUsedAsValue) ?? false
            }
            guard let block = owner.as(CodeBlockSyntax.self), let body = block.parent else { return false }
            if let function = body.as(FunctionDeclSyntax.self) {
                return function.signature.returnClause.map { !isVoid($0.type) } ?? false
            }
            if let accessor = body.as(AccessorDeclSyntax.self) {
                return accessor.accessorSpecifier.tokenKind == .keyword(.get)
            }
            if body.is(IfExprSyntax.self) {
                return isUsedAsValue(body)
            }
            return false
        }

        /* An `if` or `switch` is a value unless it stands as a statement; an `else if` answers for its whole chain. */
        static func isUsedAsValue(_ expression: Syntax) -> Bool {
            var top = expression
            while let parent = top.parent, parent.is(IfExprSyntax.self) {
                top = parent
            }
            guard let parent = top.parent else { return false }
            return !(parent.is(CodeBlockItemSyntax.self) || parent.is(ExpressionStmtSyntax.self))
        }

        static func isVoid(_ type: TypeSyntax) -> Bool {
            if let tuple = type.as(TupleTypeSyntax.self) {
                return tuple.elements.isEmpty
            }
            return type.as(IdentifierTypeSyntax.self)?.name.text == "Void"
        }

        /* `_ = try? work()`, which the unfolded tree spells as the sequence `_`, `=`, `try? work()`. */
        static func isDiscardAssignment(_ node: TryExprSyntax) -> Bool {
            guard let elements = node.parent?.as(ExprListSyntax.self), elements.count == 3 else { return false }
            let parts = Array(elements)
            return parts[0].is(DiscardAssignmentExprSyntax.self) && parts[1].is(AssignmentExprSyntax.self) && parts[2].id == node.id
        }
    }
}
