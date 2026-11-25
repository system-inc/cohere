import SwiftSyntax

/*
 No statement labels: `outer: for row in grid { ... continue outer }`. A jump to a label lands where a reader is
 not looking, so they have to find the label to know where execution goes next. The repair moves the nested loops
 into a function of their own and returns from it, or lets a named boolean the outer loop tests say when to stop.

 It is the house's one verdict on labels, held by the text rules corpus (#4b337jk, family `labels`), where
 `cohere-go/consistency-no-label` judges Go the same way. One finding per label, at the label's name: the label is
 the decision, and every `break` or `continue` naming it follows from that one decision, so a jump is never a
 finding of its own (lb01). A label no jump names is still a label, and reported the same (lb00). Swift lets a
 label sit on a loop, an `if`, a `switch` or a `do`, and each is one labeled statement here.
 */
public struct ConsistencyNoLabel: FileRule {
    public let name = "cohere-swift/consistency-no-label"
    public let origin = RuleOrigin.house
    public let upstreamName: String? = nil

    public init() {}

    /* A label is a name and a colon before a statement, so a file without a colon has none. */
    public func applies(to file: ParsedFile) -> Bool {
        file.source.contains(":")
    }

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let visitor = Visitor(viewMode: .sourceAccurate)
        visitor.walk(file.tree)
        return visitor.labels.map { label in
            file.finding(at: label, rule: name, message: RuleMessages.ConsistencyNoLabel.label(label: label.text))
        }
    }

    /* Collects the name of every labeled statement, nested ones included, in source order. */
    final class Visitor: SyntaxVisitor {
        private(set) var labels: [TokenSyntax] = []

        override func visit(_ node: LabeledStmtSyntax) -> SyntaxVisitorContinueKind {
            labels.append(node.label)
            return .visitChildren
        }
    }
}
