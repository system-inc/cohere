import Foundation
import SwiftFormat
import SwiftOperators
import SwiftSyntax

/*
 One of swift-format's own lint rules, run inside this process over the tree the engine already parsed,
 reported under a cohere name.

 Where the incumbent's rule is exactly the house rule, running it is the strongest parity there is: the
 findings are swift-format's by construction, and nothing can drift. `require-lower-camel-case` is
 `AlwaysUseLowerCamelCase` (with its exemptions: `override`s, and underscores in XCTest and `@Test` method
 names), and `no-leading-underscores` is `NoLeadingUnderscores`. The house adds the reason to each message;
 the location and the name are swift-format's.

 The tree is folded with the standard operator table first, which `SwiftLinter` requires of a tree it is
 handed. Only the one rule is enabled and the pretty-printer is off, so this is a rule walk, not a second
 format. Neither rule reads `.swift-format`: a house rule applies whatever the repository's formatter
 configuration turns off.
 */
public struct SwiftFormatRule: FileRule {
    public let name: String
    let incumbentRule: String
    let reason: String

    public init(name: String, incumbentRule: String, reason: String) {
        self.name = name
        self.incumbentRule = incumbentRule
        self.reason = reason
    }

    public static let requireLowerCamelCase = SwiftFormatRule(
        name: "cohere-swift/require-lower-camel-case",
        incumbentRule: "AlwaysUseLowerCamelCase",
        reason: "Swift spells values in lowerCamelCase and types in UpperCamelCase, so a reader tells which is which at a glance, and an underscore inside a name is a word boundary camel case already marks."
    )

    public static let noLeadingUnderscores = SwiftFormatRule(
        name: "cohere-swift/no-leading-underscores",
        incumbentRule: "NoLeadingUnderscores",
        reason: "A leading underscore is a convention for \"private\", and access control says that in a way the compiler checks."
    )

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        var configuration = Configuration()
        for rule in configuration.rules.keys {
            configuration.rules[rule] = false
        }
        configuration.rules[incumbentRule] = true

        var reported: [Finding] = []
        let linter = SwiftLinter(configuration: configuration) { reported.append($0) }
        linter.debugOptions = [.disablePrettyPrint]
        let folded = OperatorTable.standardOperators.foldAll(file.tree) { _ in }
        guard let tree = folded.as(SourceFileSyntax.self) else { return [] }
        do {
            try linter.lint(syntax: tree, source: file.source, operatorTable: .standardOperators, assumingFileURL: file.url)
        } catch {
            /*
             Reported rather than dropped: a rule that threw checked nothing in this file, and an empty list
             would read as a clean file.
             */
            return [FindingRecord(
                source: .rule,
                file: file.url.path,
                line: 1,
                column: 1,
                severity: .error,
                rule: name,
                messageId: "incumbentFailed",
                message: "swift-format's \(incumbentRule) could not run on this file, so it was not checked: \(error)"
            )]
        }
        return reported.compactMap { finding in
            guard String(describing: finding.category) == incumbentRule, let location = finding.location else { return nil }
            let text = finding.message.text
            return FindingRecord(
                source: .rule,
                file: file.url.path,
                line: location.line,
                column: location.column,
                severity: .error,
                rule: name,
                messageId: incumbentRule,
                message: text.prefix(1).uppercased() + text.dropFirst() + ". " + reason
            )
        }
    }
}
