import Foundation
import SwiftSyntax

/*
 One of swift-format's own lint rules, run inside this process over the tree the engine already parsed,
 reported under a cohere name.

 Where the incumbent's rule is exactly the house rule, running it is the strongest parity there is: the
 findings are swift-format's by construction, and nothing can drift. `always-use-lower-camel-case` is
 `AlwaysUseLowerCamelCase` (with its exemptions: `override`s, and underscores in XCTest and `@Test` method
 names), and `no-leading-underscores` is `NoLeadingUnderscores`. The house adds the reason to each message;
 the name is swift-format's, and so is where a finding starts. It ends where the name does, found as the token
 swift-format pointed at, so a finding covers the name and not a point before it. Each wrapped rule renders its
 own message and its own failure to run: `always-use-lower-camel-case` from policy/messages/, and
 `no-leading-underscores`, which has no message file yet, from its text here.

 The casing rule is the house's in two places where swift-format's stops short of it, both held by the text rules
 corpus (#4b337jk, family `casing`), where every engine gives one verdict:
 - Types are UpperCamelCase with no `_` (us03). swift-format judges values only, so the house walks the type
   declarations itself (`TypeNames`) and reports a type with its own message, which keeps its findings apart
   from swift-format's in the parity harness.
 - A value that mirrors an external name spelled exactly as that name's owner spells it is not ours to rename,
   so swift-format's finding on it is dropped (us07, `mirrorsTheNameItReads`). Decided by the declaration's
   structure, never by a list of names.

 The swift-format pass itself lives in `SwiftFormatPass`, run once per file for every wrapped rule, with
 only those rules enabled and the pretty-printer off, so it is a rule walk, not a second format. Neither
 rule follows the house format's rule table: a house rule applies whatever the formatter's table turns
 off.
 */
public struct SwiftFormatRule: FileRule {
    public let name: String
    /*
     Declared per wrapped rule rather than implied by the wrapper: running swift-format's rule is how the
     house checks a thing, and a house rule could be checked this way too.
     */
    public let origin: RuleOrigin
    public let upstreamName: String?
    let incumbentRule: String
    /* The finding's message, from swift-format's own text for it, capitalized as a sentence. */
    let message: @Sendable (_ swiftFormatFinding: String) -> RuleMessages.Message
    /* The message when swift-format's rule could not run on a file. */
    let failure: @Sendable (_ incumbentRule: String, _ error: String) -> RuleMessages.Message
    /* Whether a name swift-format reports was spelled by someone else, so the house drops the finding. */
    let spelledElsewhere: @Sendable (_ name: TokenSyntax) -> Bool
    /* What the house judges beside swift-format's rule, in the same file and under the same name. */
    let houseFindings: @Sendable (_ file: ParsedFile, _ rule: String) -> [FindingRecord]

    init(
        name: String,
        origin: RuleOrigin,
        upstreamName: String?,
        incumbentRule: String,
        message: @escaping @Sendable (_ swiftFormatFinding: String) -> RuleMessages.Message,
        failure: @escaping @Sendable (_ incumbentRule: String, _ error: String) -> RuleMessages.Message,
        spelledElsewhere: @escaping @Sendable (_ name: TokenSyntax) -> Bool = { _ in false },
        houseFindings: @escaping @Sendable (_ file: ParsedFile, _ rule: String) -> [FindingRecord] = { _, _ in [] },
    ) {
        self.name = name
        self.origin = origin
        self.upstreamName = upstreamName
        self.incumbentRule = incumbentRule
        self.message = message
        self.failure = failure
        self.spelledElsewhere = spelledElsewhere
        self.houseFindings = houseFindings
    }

    public static let requireLowerCamelCase = SwiftFormatRule(
        name: "cohere-swift/always-use-lower-camel-case",
        origin: .swiftFormat,
        upstreamName: "AlwaysUseLowerCamelCase",
        incumbentRule: "AlwaysUseLowerCamelCase",
        message: { swiftFormatFinding in
            RuleMessages.AlwaysUseLowerCamelCase.alwaysUseLowerCamelCase(swiftFormatFinding: swiftFormatFinding)
        },
        failure: { incumbentRule, error in
            RuleMessages.AlwaysUseLowerCamelCase.incumbentFailed(incumbentRule: incumbentRule, error: error)
        },
        spelledElsewhere: { name in SwiftFormatRule.mirrorsTheNameItReads(name) },
        houseFindings: { file, rule in SwiftFormatRule.TypeNames.findings(in: file, rule: rule) },
    )

    public static let noLeadingUnderscores = SwiftFormatRule(
        name: "cohere-swift/no-leading-underscores",
        origin: .swiftFormat,
        upstreamName: "NoLeadingUnderscores",
        incumbentRule: "NoLeadingUnderscores",
        message: { swiftFormatFinding in
            RuleMessages.Message(
                id: "NoLeadingUnderscores",
                text:
                    "\(swiftFormatFinding). A leading underscore is a convention for \"private\", and access control says that in a way the compiler checks.",
            )
        },
        failure: { incumbentRule, error in
            RuleMessages.Message(
                id: "incumbentFailed",
                text: "swift-format's \(incumbentRule) could not run on this file, so it was not checked: \(error)",
            )
        },
    )

    /* Every incumbent rule this wrapper runs, so one swift-format pass answers for all of them. */
    static let incumbentRules = [requireLowerCamelCase.incumbentRule, noLeadingUnderscores.incumbentRule]

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        let reported: SwiftFormatPass.Outcome
        do {
            reported = try SwiftFormatPass.shared.findings(in: file)
        }
        catch {
            /*
             Reported rather than dropped: a rule that threw checked nothing in this file, and an empty list
             would read as a clean file.
             */
            let rendered = failure(incumbentRule, "\(error)")
            return [
                FindingRecord(
                    source: .rule,
                    file: file.url.path,
                    line: 1,
                    column: 1,
                    severity: .error,
                    rule: name,
                    messageId: rendered.id,
                    message: rendered.text,
                )
            ]
        }
        let incumbentFindings: [FindingRecord] = reported.filter { $0.rule == incumbentRule }.compactMap { finding in
            let rendered = message(finding.text.prefix(1).uppercased() + finding.text.dropFirst())
            /* swift-format reports at the name's first byte, so the token there is the name it judged. */
            let position = file.locations.position(ofLine: finding.line, column: finding.column)
            guard let token = file.tree.token(at: position), token.positionAfterSkippingLeadingTrivia == position
            else {
                /*
                 Kept with no end rather than dropped: swift-format found something here, and a finding that only
                 starts is still the defect, where a dropped one would read as a clean name.
                 */
                return FindingRecord(
                    source: .rule,
                    file: file.url.path,
                    line: finding.line,
                    column: finding.column,
                    severity: .error,
                    rule: name,
                    messageId: rendered.id,
                    message: rendered.text,
                )
            }
            if spelledElsewhere(token) {
                return nil
            }
            return file.finding(at: token, rule: name, message: rendered)
        }
        return incumbentFindings + houseFindings(file, name)
    }

    /*
     Whether a value's name mirrors the external name its value is read from, spelled exactly as that name's owner
     spells it, so the spelling is the owner's and not ours (CLAUDE.md: "matching the upstream spelling keeps it
     greppable across that boundary"). The structure decides, at the binding the name declares:

     - its value reads a key spelled as the name: a subscript or call anywhere in the initializer takes, as an
       argument, a plain string literal whose text is the name, `let DATABASE_URL =
       ProcessInfo.processInfo.environment["DATABASE_URL"] ?? ""` or `let HOME = getenv("HOME")`;
     - or its value is a reference to a declaration of exactly the name, `let O_RDONLY = Darwin.O_RDONLY`, or
       re-binds it, `if let MAX_SIZE`, `guard let MAX_SIZE = MAX_SIZE`: the name was spelled where it was
       declared, and is judged there.

     Spelled exactly means byte for byte, so `let O_RDONLY_MODE = O_RDONLY` mirrors nothing and is reported (us08):
     the house's own name for an upstream value is ours. A literal held as the value, `let DATABASE_URL =
     "DATABASE_URL"`, reads nothing, so it is ours too, and so is a key in a dictionary literal, which a value
     builds rather than reads. Only a binding with a value can mirror one, so a parameter, a closure parameter, a
     function and an enum case never do.
     */
    static func mirrorsTheNameItReads(_ name: TokenSyntax) -> Bool {
        guard let pattern = name.parent?.as(IdentifierPatternSyntax.self) else { return false }
        let value: ExprSyntax
        if let binding = pattern.parent?.as(PatternBindingSyntax.self) {
            guard let initializer = binding.initializer else { return false }
            value = initializer.value
        }
        else if let condition = pattern.parent?.as(OptionalBindingConditionSyntax.self) {
            /* `if let MAX_SIZE` with no value re-binds the declaration of that name. */
            guard let initializer = condition.initializer else { return true }
            value = initializer.value
        }
        else {
            return false
        }
        if referencedName(of: value) == name.text {
            return true
        }
        return value.tokens(viewMode: .sourceAccurate).contains { token in
            guard case .stringSegment(let text) = token.tokenKind, text == name.text,
                let literal = token.parent?.parent?.parent?.as(StringLiteralExprSyntax.self),
                literal.segments.count == 1,
                let argument = literal.parent?.as(LabeledExprSyntax.self),
                let arguments = argument.parent?.as(LabeledExprListSyntax.self)
            else { return false }
            return arguments.parent?.is(SubscriptCallExprSyntax.self) == true
                || arguments.parent?.is(FunctionCallExprSyntax.self) == true
        }
    }

    /* The declaration a value names when the value is only that reference, through `try`, `await` and parentheses. */
    static func referencedName(of value: ExprSyntax) -> String? {
        if let reference = value.as(DeclReferenceExprSyntax.self) {
            return reference.baseName.text
        }
        if let member = value.as(MemberAccessExprSyntax.self) {
            return member.declName.baseName.text
        }
        if let attempt = value.as(TryExprSyntax.self) {
            return referencedName(of: attempt.expression)
        }
        if let awaited = value.as(AwaitExprSyntax.self) {
            return referencedName(of: awaited.expression)
        }
        if let parenthesized = value.as(TupleExprSyntax.self), parenthesized.elements.count == 1,
            let only = parenthesized.elements.first, only.label == nil
        {
            return referencedName(of: only.expression)
        }
        return nil
    }

    /*
     The house's type casing, beside swift-format's value casing: every declared type name is UpperCamelCase with
     no `_` inside it, the same two tests swift-format puts to a value turned around. A struct, class, enum, actor,
     protocol, type alias, associated type and generic parameter each declare one. A leading `_` is
     `no-leading-underscores`'s finding, as it is for a value, and the first letter is judged in ASCII, as
     swift-format judges a value's. A macro is not a type, and an extension declares no name.
     */
    final class TypeNames: SyntaxVisitor {
        private(set) var declared: [(declaration: String, name: TokenSyntax)] = []

        static func findings(in file: ParsedFile, rule: String) -> [FindingRecord] {
            let visitor = TypeNames(viewMode: .sourceAccurate)
            visitor.walk(file.tree)
            return visitor.declared.compactMap { declared in
                guard case .identifier(let text) = declared.name.tokenKind, isNotUpperCamelCase(text) else {
                    return nil
                }
                return file.finding(
                    at: declared.name,
                    rule: rule,
                    message: RuleMessages.AlwaysUseLowerCamelCase.alwaysUseUpperCamelCaseForType(
                        declaration: declared.declaration,
                        name: text,
                    ),
                )
            }
        }

        /* An `_` after the first character, or a lowercase first letter. */
        static func isNotUpperCamelCase(_ text: String) -> Bool {
            guard let first = text.unicodeScalars.first else { return false }
            return text.dropFirst().contains("_") || ("a"..."z").contains(first)
        }

        private func declare(_ declaration: String, _ name: TokenSyntax) {
            declared.append((declaration, name))
        }

        override func visit(_ node: StructDeclSyntax) -> SyntaxVisitorContinueKind {
            declare("struct", node.name)
            return .visitChildren
        }
        override func visit(_ node: ClassDeclSyntax) -> SyntaxVisitorContinueKind {
            declare("class", node.name)
            return .visitChildren
        }
        override func visit(_ node: EnumDeclSyntax) -> SyntaxVisitorContinueKind {
            declare("enum", node.name)
            return .visitChildren
        }
        override func visit(_ node: ActorDeclSyntax) -> SyntaxVisitorContinueKind {
            declare("actor", node.name)
            return .visitChildren
        }
        override func visit(_ node: ProtocolDeclSyntax) -> SyntaxVisitorContinueKind {
            declare("protocol", node.name)
            return .visitChildren
        }
        override func visit(_ node: TypeAliasDeclSyntax) -> SyntaxVisitorContinueKind {
            declare("type alias", node.name)
            return .visitChildren
        }
        override func visit(_ node: AssociatedTypeDeclSyntax) -> SyntaxVisitorContinueKind {
            declare("associated type", node.name)
            return .visitChildren
        }
        override func visit(_ node: GenericParameterSyntax) -> SyntaxVisitorContinueKind {
            declare("generic parameter", node.name)
            return .visitChildren
        }
    }
}
