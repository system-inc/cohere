import SwiftSyntax

/*
 Cyclomatic complexity: a function, initializer, accessor or closure with more than 20 paths through it.

 Every decision point is another path a reader has to hold open at once and another path a test has to
 cover. Past a point the body cannot be understood in one sitting, and the repair is to split decisions out
 into named functions, or to replace the branching with a lookup, so each piece reads on its own. No fix is
 offered: only the author knows where the seams are.

 The Swift port of our TypeScript `complexity` rule (cohere's `internal/lint/rules/core/complexity.go`),
 which wins over SwiftLint's `cyclomatic_complexity` on both the threshold and what counts.

 The threshold is 20, the Go rule's default. No house configuration sets one: none of the
 `CohereSettings.json` files under ahra, system, phi or connected mention `complexity`, and Structure's
 lint configuration does not either. The score is the Go rule's: 1 for the function, plus 1 per decision
 point, reported when it is greater than the threshold.

 What counts, each JavaScript construct the Go rule counts mapped to its Swift spelling:
   - `if` and every `else if` (each is its own `IfExprSyntax`), `guard`, `while`, `repeat`, `for`.
   - every `case` item, unless the switch is a lookup table (see the exemptions): `case .a, .b:` counts 2,
     because it is the Swift spelling of JavaScript's stacked `case A: case B:`, which counts 2. `default`
     and `@unknown default` never count, as upstream's `SwitchCase[test]` never counts `default`.
     `fallthrough` takes nothing away, as JavaScript fall-through takes nothing away.
   - every `catch` clause.
   - `&&`, `||` and `??`, one per operator.
   - every condition after the first in an `if`, `guard` or `while` condition list: `if let a, let b`
     is `if (a != null && b != null)`, and the comma is Swift's spelling of that `&&`.
   - a `where` on a `for`, a `case` item or a `catch` item: it is an `if` (or an `&&`) written in place.
   - the ternary `?:`.
   - optional chaining, one per `?` in an access: `a?.b`, `a?()`, `a?[0]`, as the Go rule counts each
     optional member access and call. Only a `?` that is the base of a member access, call or subscript
     counts, so the `x?` of a pattern (`case let x?`) is not mistaken for one.
   - a parameter default value, which the Go rule counts as a branch on whether the argument was given.

 Where SwiftLint differs (0.65.1, measured): it starts from 0 rather than 1, so its score is one lower; it
 does not count `&&`, `||`, `??`, the ternary, optional chaining, condition commas, `where` or parameter
 defaults; it counts one per `case` clause however many items it has, counts `default`, and subtracts one
 per `fallthrough`; it folds a closure's branches into the enclosing function, where we score the closure
 as its own body as the Go rule scores an arrow function; it counts every `#if` clause; it has no
 lookup-table exemption, so its own documented triggering example, whose switch is four `case n: break`
 arms, scores 11 there and 8 here; and it reports only functions and initializers.

 Each frame is scored on its own and its nested frames are not added to it, as the Go rule does with a
 nested function. The frames: a function, initializer, deinitializer, subscript, explicit accessor
 (`get`, `set`, `didSet`, ...), a computed property's implicit getter, a closure, and a stored property's
 initializer inside a type (the Go rule's class field initializer). Top-level code is never a frame, as the
 Go rule never reports the program, so a top-level `if` and a global's initializer are not scored.

 Exemptions, each with its reason:
   - A switch that is a lookup table counts 1 for the whole switch, which is the Go rule's `modified`
     variant applied only where it is true. A table: every `case` item is an enum case (`.light`,
     `Weight.light`) or a literal, with no binding and no `where`, and every arm is one plain statement
     (a value, a `return`, a call, a `break` or a `throw`). The message's repair is "replace the branching
     with a lookup", and in Swift the exhaustive switch IS the lookup: rewriting it as a dictionary would
     throw away the compiler's exhaustiveness check and make the code worse, so flagging it would be a
     finding nobody can act on. In TypeScript the same table is a `Record`, which the Go rule never counts.
     A table's arms are still read, so a ternary or `??` inside one counts as usual. Anything else, such
     as `case .move(let distance):`, a `where`, a range, two statements in an arm, a nested `if`, a
     `fallthrough` or a `#if` among the cases, is ordinary logic and counts item by item.
   - `#if` branches: only one of them is ever compiled, so the clause with the most decisions is counted
     and the others are not. Adding them all, as SwiftLint does, would flag a function whose compiled body
     on every platform is under the threshold. The `#if` condition itself (`#if os(macOS) && DEBUG`) is not
     a runtime branch and is never counted.
   - `try?`, `as?`, key path `?` and `if #available` are not counted beyond the statement that holds them:
     the Go rule has no JavaScript construct for them to map from.
 */
public struct CyclomaticComplexity: FileRule {
    public let name = "cohere-swift/cyclomatic-complexity"

    /* The Go rule's default, which the house has not overridden; see the comment on the type. */
    public static let defaultMaximum = 20

    let maximum: Int

    public init() {
        maximum = Self.defaultMaximum
    }

    /* For fixtures, which read better against a small threshold, as the Go rule's own do. */
    init(maximum: Int) {
        self.maximum = maximum
    }

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let visitor = Visitor(viewMode: .sourceAccurate)
        visitor.walk(file.tree)
        return visitor.scored.filter { $0.complexity > maximum }.map { scored in
            file.finding(
                at: scored.anchor,
                rule: name,
                messageId: "complex",
                message: "\(scored.label) has a complexity of \(scored.complexity). Maximum allowed is \(maximum). Every branch here is another path a reader has to hold open at once, and another path a test has to cover. Split the decision out into named functions, or replace the branching with a lookup, so each piece can be understood on its own."
            )
        }
    }

    /* One scored body: where to report it, how to name it, and its complexity. */
    struct Scored {
        let anchor: TokenSyntax
        let label: String
        let complexity: Int
    }

    /* Keeps a stack of open frames and counts each decision point into the innermost one. */
    final class Visitor: SyntaxVisitor {
        struct Frame {
            let node: SyntaxIdentifier
            let anchor: TokenSyntax
            let label: String
            var decisions = 0
        }

        private var frames: [Frame] = []
        private var tableSwitches: Set<SyntaxIdentifier> = []
        private(set) var scored: [Scored] = []

        private func open(_ node: some SyntaxProtocol, anchor: TokenSyntax, label: String) {
            frames.append(Frame(node: node.id, anchor: anchor, label: label))
        }

        /* Closes the innermost frame if this node opened it. A node that opened none, such as a local stored property, leaves the stack alone. */
        private func close(_ node: some SyntaxProtocol) {
            guard let frame = frames.last, frame.node == node.id else { return }
            frames.removeLast()
            scored.append(Scored(anchor: frame.anchor, label: frame.label, complexity: 1 + frame.decisions))
        }

        private func count(_ amount: Int) {
            guard amount > 0, !frames.isEmpty else { return }
            frames[frames.count - 1].decisions += amount
        }

        override func visit(_ node: FunctionDeclSyntax) -> SyntaxVisitorContinueKind {
            open(node, anchor: node.name, label: Self.label(of: node))
            return .visitChildren
        }

        override func visitPost(_ node: FunctionDeclSyntax) {
            close(node)
        }

        override func visit(_ node: InitializerDeclSyntax) -> SyntaxVisitorContinueKind {
            open(node, anchor: node.initKeyword, label: "Initializer")
            return .visitChildren
        }

        override func visitPost(_ node: InitializerDeclSyntax) {
            close(node)
        }

        override func visit(_ node: DeinitializerDeclSyntax) -> SyntaxVisitorContinueKind {
            open(node, anchor: node.deinitKeyword, label: "Deinitializer")
            return .visitChildren
        }

        override func visitPost(_ node: DeinitializerDeclSyntax) {
            close(node)
        }

        /* A subscript is a frame so its parameter defaults and its implicit getter land in it; explicit accessors are frames of their own. */
        override func visit(_ node: SubscriptDeclSyntax) -> SyntaxVisitorContinueKind {
            open(node, anchor: node.subscriptKeyword, label: "Subscript")
            return .visitChildren
        }

        override func visitPost(_ node: SubscriptDeclSyntax) {
            close(node)
        }

        override func visit(_ node: AccessorDeclSyntax) -> SyntaxVisitorContinueKind {
            open(node, anchor: node.accessorSpecifier, label: Self.label(of: node))
            return .visitChildren
        }

        override func visitPost(_ node: AccessorDeclSyntax) {
            close(node)
        }

        override func visit(_ node: ClosureExprSyntax) -> SyntaxVisitorContinueKind {
            open(node, anchor: node.leftBrace, label: "Closure")
            return .visitChildren
        }

        override func visitPost(_ node: ClosureExprSyntax) {
            close(node)
        }

        /*
         A binding is a frame in two cases: a computed property's implicit getter, wherever it is, and a
         stored property's initializer inside a type, which is the Go rule's class field initializer. A
         local or global stored property is not, so its initializer counts toward the enclosing function,
         or toward nothing at the top level.
         */
        override func visit(_ node: PatternBindingSyntax) -> SyntaxVisitorContinueKind {
            guard let anchor = node.pattern.firstToken(viewMode: .sourceAccurate) else { return .visitChildren }
            let propertyName = node.pattern.trimmedDescription
            if case .getter = node.accessorBlock?.accessors {
                open(node, anchor: anchor, label: "Getter '\(propertyName)'")
            } else if node.initializer != nil, Self.isTypeMember(node) {
                open(node, anchor: anchor, label: "Property initializer '\(propertyName)'")
            }
            return .visitChildren
        }

        override func visitPost(_ node: PatternBindingSyntax) {
            close(node)
        }

        override func visit(_ node: IfExprSyntax) -> SyntaxVisitorContinueKind {
            count(node.conditions.count)
            return .visitChildren
        }

        override func visit(_ node: GuardStmtSyntax) -> SyntaxVisitorContinueKind {
            count(node.conditions.count)
            return .visitChildren
        }

        override func visit(_ node: WhileStmtSyntax) -> SyntaxVisitorContinueKind {
            count(node.conditions.count)
            return .visitChildren
        }

        override func visit(_ node: RepeatStmtSyntax) -> SyntaxVisitorContinueKind {
            count(1)
            return .visitChildren
        }

        override func visit(_ node: ForStmtSyntax) -> SyntaxVisitorContinueKind {
            count(node.whereClause == nil ? 1 : 2)
            return .visitChildren
        }

        override func visit(_ node: CatchClauseSyntax) -> SyntaxVisitorContinueKind {
            count(1)
            return .visitChildren
        }

        override func visit(_ node: CatchItemSyntax) -> SyntaxVisitorContinueKind {
            count(node.whereClause == nil ? 0 : 1)
            return .visitChildren
        }

        /* A lookup-table switch counts once for the whole switch; any other switch is counted item by item below. */
        override func visit(_ node: SwitchExprSyntax) -> SyntaxVisitorContinueKind {
            if Self.isLookupTable(node) {
                tableSwitches.insert(node.id)
                count(1)
            }
            return .visitChildren
        }

        /* One per item of a `case` label; `default` has no items to count. */
        override func visit(_ node: SwitchCaseItemSyntax) -> SyntaxVisitorContinueKind {
            if let owner = node.parent?.parent?.parent?.parent?.parent, tableSwitches.contains(owner.id) {
                return .visitChildren
            }
            count(node.whereClause == nil ? 1 : 2)
            return .visitChildren
        }

        override func visit(_ node: BinaryOperatorExprSyntax) -> SyntaxVisitorContinueKind {
            if ["&&", "||", "??"].contains(node.operator.text) {
                count(1)
            }
            return .visitChildren
        }

        override func visit(_ node: UnresolvedTernaryExprSyntax) -> SyntaxVisitorContinueKind {
            count(1)
            return .visitChildren
        }

        override func visit(_ node: TernaryExprSyntax) -> SyntaxVisitorContinueKind {
            count(1)
            return .visitChildren
        }

        override func visit(_ node: OptionalChainingExprSyntax) -> SyntaxVisitorContinueKind {
            if Self.isAccessBase(node) {
                count(1)
            }
            return .visitChildren
        }

        override func visit(_ node: FunctionParameterSyntax) -> SyntaxVisitorContinueKind {
            count(node.defaultValue == nil ? 0 : 1)
            return .visitChildren
        }

        /*
         Only one `#if` clause is compiled, so the clause with the most decisions is the one counted. Each
         clause's body is walked here by hand, which also keeps the clause's condition out of the count.
         With no frame open there is nothing to count into, and the clauses are walked normally so the
         frames inside them still open.
         */
        override func visit(_ node: IfConfigDeclSyntax) -> SyntaxVisitorContinueKind {
            guard !frames.isEmpty else { return .visitChildren }
            let owner = frames.count - 1
            let before = frames[owner].decisions
            var mostInOneClause = 0
            for clause in node.clauses {
                frames[owner].decisions = 0
                if let elements = clause.elements {
                    walk(elements)
                }
                mostInOneClause = Swift.max(mostInOneClause, frames[owner].decisions)
            }
            frames[owner].decisions = before + mostInOneClause
            return .skipChildren
        }

        /* `func` at the top level or inside another body is a function; inside a type it is a method. */
        static func label(of node: FunctionDeclSyntax) -> String {
            let isStatic = node.modifiers.contains { modifier in
                modifier.name.tokenKind == .keyword(.static) || modifier.name.tokenKind == .keyword(.class)
            }
            let isMember = node.parent?.is(MemberBlockItemSyntax.self) ?? false
            let kind = isStatic ? "Static method" : (isMember ? "Method" : "Function")
            return "\(kind) '\(node.name.text)'"
        }

        /* Named for what it reads or writes: `Getter 'title'`, `Setter 'title'`, `Accessor didSet of 'title'`. */
        static func label(of node: AccessorDeclSyntax) -> String {
            let block = node.parent?.parent?.as(AccessorBlockSyntax.self)
            let owner: String
            if let binding = block?.parent?.as(PatternBindingSyntax.self) {
                owner = "'\(binding.pattern.trimmedDescription)'"
            } else {
                owner = "the subscript"
            }
            switch node.accessorSpecifier.tokenKind {
            case .keyword(.get):
                return "Getter \(owner)"
            case .keyword(.set):
                return "Setter \(owner)"
            default:
                return "Accessor \(node.accessorSpecifier.text) of \(owner)"
            }
        }

        /*
         A switch that is a table: every `case` matches only enum cases or literals, with no bindings and no
         `where`, and every arm is one plain statement (a value, a `return`, a call, a `break`, a `throw`).
         A `#if` among the cases, a `fallthrough`, or an arm with more than one statement or its own `if`
         or `switch` makes it ordinary logic, counted item by item.
         */
        static func isLookupTable(_ node: SwitchExprSyntax) -> Bool {
            var keys = 0
            for element in node.cases {
                guard let switchCase = element.as(SwitchCaseSyntax.self), isPlainArm(switchCase.statements) else { return false }
                if case .case(let label) = switchCase.label {
                    for item in label.caseItems {
                        guard item.whereClause == nil, let pattern = item.pattern.as(ExpressionPatternSyntax.self), isTableKey(pattern.expression) else {
                            return false
                        }
                        keys += 1
                    }
                }
            }
            return keys > 0
        }

        /* `.light`, `Weight.light`, `"Legacy"`, `7`, `2.5`, `true`, `nil`: a key, not a pattern that binds or tests. */
        static func isTableKey(_ expression: ExprSyntax) -> Bool {
            if let access = expression.as(MemberAccessExprSyntax.self) {
                return access.declName.argumentNames == nil && (access.base == nil || access.base?.is(DeclReferenceExprSyntax.self) == true)
            }
            if let string = expression.as(StringLiteralExprSyntax.self) {
                return string.segments.allSatisfy { $0.is(StringSegmentSyntax.self) }
            }
            return expression.is(IntegerLiteralExprSyntax.self) || expression.is(FloatLiteralExprSyntax.self)
                || expression.is(BooleanLiteralExprSyntax.self) || expression.is(NilLiteralExprSyntax.self)
        }

        static func isPlainArm(_ statements: CodeBlockItemListSyntax) -> Bool {
            guard statements.count == 1, let only = statements.first else { return false }
            switch only.item {
            case .expr(let expression):
                return !isBranching(expression)
            case .stmt(let statement):
                if let returned = statement.as(ReturnStmtSyntax.self) {
                    return returned.expression.map { !isBranching($0) } ?? true
                }
                if let thrown = statement.as(ThrowStmtSyntax.self) {
                    return !isBranching(thrown.expression)
                }
                return statement.is(BreakStmtSyntax.self)
            case .decl:
                return false
            }
        }

        static func isBranching(_ expression: ExprSyntax) -> Bool {
            expression.is(IfExprSyntax.self) || expression.is(SwitchExprSyntax.self)
        }

        static func isTypeMember(_ binding: PatternBindingSyntax) -> Bool {
            binding.parent?.parent?.parent?.is(MemberBlockItemSyntax.self) ?? false
        }

        /* `a?.b`, `a?()` and `a?[0]`: the `?` is the base of the access it guards. Anything else, such as a pattern's `x?`, is not a chain. */
        static func isAccessBase(_ node: OptionalChainingExprSyntax) -> Bool {
            guard let parent = node.parent else { return false }
            if let access = parent.as(MemberAccessExprSyntax.self) {
                return access.base?.id == node.id
            }
            if let call = parent.as(FunctionCallExprSyntax.self) {
                return call.calledExpression.id == node.id
            }
            if let subscriptCall = parent.as(SubscriptCallExprSyntax.self) {
                return subscriptCall.calledExpression.id == node.id
            }
            return false
        }
    }
}
