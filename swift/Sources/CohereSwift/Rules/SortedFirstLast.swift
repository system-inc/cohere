import SwiftSyntax

/*
 No sorting a collection only to read one end of it: `items.sorted().first` is `items.min()`, and
 `items.sorted(by: areInIncreasingOrder).last` is `items.max(by: areInIncreasingOrder)`. `min` and `max` say
 the question being asked, and they walk the elements once, where `sorted` builds a whole new array and sorts
 it, n log n comparisons, to throw all of it away but one element.

 The shapes are SwiftLint's `sorted_first_last`: `first` or `last` read as a property of a `sorted` call that
 takes no argument (`sorted()`, or `sorted { }` with a trailing closure) or exactly one labelled `by`
 (`sorted(by: >)`, `sorted(by: { })`, `sorted(by: someFunction)`), anywhere in a chain
 (`items.map { }.sorted().first`) and whatever follows (`items.sorted().last?.name`). Its exemptions, each kept
 for its reason:

 - A `first` or `last` that is called, `sorted().first(where:)`, `sorted().first { }`, is not the shape:
   that is a search, not an end, and `min` cannot answer it. The names `firstIndex` and `lastIndex` never
   match. Both reasons are about meaning, so they hold whatever the types.
 - A `sorted` with any other label is left alone. SwiftLint means Realm's `sorted(byKeyPath:)`, which the types
   now exclude by themselves, being no standard library declaration; the label check still earns its place
   for Foundation's `sorted(using:)`, which sorts a standard library sequence by a `SortComparator` and has no
   `min(using:)` to move to.
 - SwiftLint's exemption for a key path is wider than Realm once types are known. A `sorted(by: \.name)`
   declared in an extension of `Sequence` in a module of ours passes the label check, and its symbol name
   starts `s:ST`, so a check that asks only whether a symbol is spelled like the standard library's says yes.
   This rule asks for the standard library's own declaration: a `sorted` whose symbol is `Sequence`'s in the
   `Swift` module (`s:STs`), and a `first` or `last` that is `Collection.first` (`s:Sls`) or
   `BidirectionalCollection.last` (`s:SKs`), read from the index the build wrote. Ours is `s:ST7Control`, and
   is not flagged: its repair would name a `min(by:)` that may not exist.

 SwiftLint judges the spelling, so it also flags a type of ours with its own `sorted` and `first`, where `min`
 may not exist. That is the difference in our favour. Where we differ in the direction of finding more of the
 same thing: a bare `sorted()` inside a sequence's own extension (`sorted().first`) and a call wrapped in one
 pair of parentheses (`(items.sorted()).last`) are found as well as a member one. A finding starts where
 SwiftLint's does, at the start of the whole chain.

 A lazy chain is flagged. `items.lazy.map { }.sorted()` resolves to the same `Sequence.sorted`, which builds
 and sorts an array whatever it was handed, so the reason holds in full, and `min` on the lazy chain walks it
 once without building anything.

 One difference the reader should know, and the reason `first` and `last` keep their own message ids,
 `sortedFirst` and `sortedLast`: the sort is stable, so among equally ordered elements `sorted().first` is the
 first of them and `sorted().last` the last. `min` also keeps the first, so `first` to `min` is exact. `max`
 keeps the first as well, not the last, so where equal elements can be told apart (records compared by one
 field) `max(by:)` may return a different one of the tied elements than `sorted(by:).last` did. The finding is
 still right about the waste, and the `sortedLast` message says so, so the reader checks whether a tie matters.

 Misses, accepted: a type of ours with its own `sorted`, and a collection type of ours that shadows `first` or
 `last`, resolve to ours and are not flagged; `sorted()[0]` and `sorted().prefix(1)` read an end too and are not
 SwiftLint's shapes, so they are left alone. No sibling rule's shape overlaps this one: `first-where` and
 `last-where` read `first` and `last` after a `filter`, not a `sorted`, and the `sorted().first(where:)` that
 sits between them is exempt here.
 */
public struct SortedFirstLast: TypedFileRule {
    public let name = "cohere-swift/sorted-first-last"

    public init() {}

    /* A `sorted` and a `first` or `last` anywhere in the file: nothing else can hold this shape. */
    public func applies(to file: ParsedFile) -> Bool {
        file.source.contains("sorted") && (file.source.contains("first") || file.source.contains("last"))
    }

    public func findings(in file: ParsedFile, symbols: FileSymbols) -> [FindingRecord] {
        let visitor = Visitor(viewMode: .sourceAccurate)
        visitor.walk(file.tree)
        return visitor.found.compactMap { candidate in
            guard Self.resolves(candidate.sorted, in: file, symbols: symbols, symbolPrefix: "s:STs", names: ["sorted()", "sorted(by:)"]) else { return nil }
            if candidate.end.text == "first" {
                guard Self.resolves(candidate.end, in: file, symbols: symbols, symbolPrefix: "s:Sls", names: ["first"]) else { return nil }
                return file.finding(
                    at: candidate.node,
                    rule: name,
                    messageId: "sortedFirst",
                    message: "Sorting a collection to read its first element builds and sorts a whole new array to keep one element. Use min() (or min(by:) with the same predicate): it says what is meant, and it walks the elements once."
                )
            }
            guard Self.resolves(candidate.end, in: file, symbols: symbols, symbolPrefix: "s:SKs", names: ["last"]) else { return nil }
            return file.finding(
                at: candidate.node,
                rule: name,
                messageId: "sortedLast",
                message: "Sorting a collection to read its last element builds and sorts a whole new array to keep one element. Use max() (or max(by:) with the same predicate): it says what is meant, and it walks the elements once. Among equally ordered elements max returns the first where sorted().last returned the last, so check that a tie does not matter."
            )
        }
    }

    /*
     Whether the name at this token is a written reference to the standard library's own declaration of one of
     these names: the symbol starts with the type's substitution and `s`, the `Swift` module, so an extension of
     ours on the same protocol (`s:ST7Control...`) or Foundation's (`s:ST10Foundation...`) does not answer.
     */
    static func resolves(_ token: TokenSyntax, in file: ParsedFile, symbols: FileSymbols, symbolPrefix: String, names: Set<String>) -> Bool {
        let location = file.locations.location(for: token.positionAfterSkippingLeadingTrivia)
        guard let resolved = symbols.reference(line: location.line, column: location.column) else { return false }
        return resolved.symbol.hasPrefix(symbolPrefix) && names.contains(resolved.name)
    }

    /* One shape found: the `first` or `last` read, its name token, and the `sorted` name token. */
    struct Candidate {
        var node: MemberAccessExprSyntax
        var end: TokenSyntax
        var sorted: TokenSyntax
    }

    /* Every `first` or `last` read as a property of a `sorted` call that takes no argument or only `by:`. */
    final class Visitor: SyntaxVisitor {
        private(set) var found: [Candidate] = []

        override func visit(_ node: MemberAccessExprSyntax) -> SyntaxVisitorContinueKind {
            let end = node.declName.baseName
            guard end.text == "first" || end.text == "last", node.declName.argumentNames == nil, !Self.isCalled(node), let base = node.base, let sorted = Self.sortedToken(base) else {
                return .visitChildren
            }
            found.append(Candidate(node: node, end: end, sorted: sorted))
            return .visitChildren
        }

        /* `sorted().first(where:)` and `sorted().first { }`: the read is the called expression of a call. */
        static func isCalled(_ node: MemberAccessExprSyntax) -> Bool {
            guard let call = node.parent?.as(FunctionCallExprSyntax.self) else { return false }
            return call.calledExpression.id == node.id
        }

        /* The `sorted` name token of `items.sorted()`, `sorted(by:)`, `sorted { }`, or either inside one pair of parentheses. */
        static func sortedToken(_ expression: ExprSyntax) -> TokenSyntax? {
            var callExpression = expression
            if let tuple = expression.as(TupleExprSyntax.self), tuple.elements.count == 1, let only = tuple.elements.first, only.label == nil {
                callExpression = only.expression
            }
            guard let call = callExpression.as(FunctionCallExprSyntax.self) else { return nil }
            let labels = call.arguments.map { $0.label?.text }
            guard labels.isEmpty || labels == ["by"] else { return nil }
            if let member = call.calledExpression.as(MemberAccessExprSyntax.self), member.declName.baseName.text == "sorted", member.declName.argumentNames == nil {
                return member.declName.baseName
            }
            if let reference = call.calledExpression.as(DeclReferenceExprSyntax.self), reference.baseName.text == "sorted", reference.argumentNames == nil {
                return reference.baseName
            }
            return nil
        }
    }
}
