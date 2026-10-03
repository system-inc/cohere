import Foundation

/*
 One of swift-format's own lint rules, run inside this process over the tree the engine already parsed,
 reported under a cohere name.

 Where the incumbent's rule is exactly the house rule, running it is the strongest parity there is: the
 findings are swift-format's by construction, and nothing can drift. `always-use-lower-camel-case` is
 `AlwaysUseLowerCamelCase` (with its exemptions: `override`s, and underscores in XCTest and `@Test` method
 names), and `no-leading-underscores` is `NoLeadingUnderscores`. The house adds the reason to each message;
 the location and the name are swift-format's.

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
    let reason: String

    public init(name: String, origin: RuleOrigin, upstreamName: String?, incumbentRule: String, reason: String) {
        self.name = name
        self.origin = origin
        self.upstreamName = upstreamName
        self.incumbentRule = incumbentRule
        self.reason = reason
    }

    public static let requireLowerCamelCase = SwiftFormatRule(
        name: "cohere-swift/always-use-lower-camel-case",
        origin: .swiftFormat,
        upstreamName: "AlwaysUseLowerCamelCase",
        incumbentRule: "AlwaysUseLowerCamelCase",
        reason:
            "Swift spells values in lowerCamelCase and types in UpperCamelCase, so a reader tells which is which at a glance, and an underscore inside a name is a word boundary camel case already marks.",
    )

    public static let noLeadingUnderscores = SwiftFormatRule(
        name: "cohere-swift/no-leading-underscores",
        origin: .swiftFormat,
        upstreamName: "NoLeadingUnderscores",
        incumbentRule: "NoLeadingUnderscores",
        reason:
            "A leading underscore is a convention for \"private\", and access control says that in a way the compiler checks.",
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
            return [
                FindingRecord(
                    source: .rule,
                    file: file.url.path,
                    line: 1,
                    column: 1,
                    severity: .error,
                    rule: name,
                    messageId: "incumbentFailed",
                    message:
                        "swift-format's \(incumbentRule) could not run on this file, so it was not checked: \(error)",
                )
            ]
        }
        return reported.filter { $0.rule == incumbentRule }.map { finding in
            FindingRecord(
                source: .rule,
                file: file.url.path,
                line: finding.line,
                column: finding.column,
                severity: .error,
                rule: name,
                messageId: incumbentRule,
                message: finding.text.prefix(1).uppercased() + finding.text.dropFirst() + ". " + reason,
            )
        }
    }
}
