/*
 One incumbent rule and the cohere-swift rules that must match it finding for finding: the never-worse
 doctrine as a table. Adding a rule to the harness is adding a row here.

 The incumbent ids are the design doc's rule inventory (section 5). A row names a set of cohere rules
 because the tools cut the same ground differently: swift-format's `NeverForceUnwrap` reports `as!` as
 well as `!`, which cohere splits into no-force-unwrap and no-force-cast (measured on Presence, where the
 nine `as!` it reported looked like misses until the two cohere rules were compared together).
 */
struct RuleMapping: Sendable {
    enum Incumbent: String, Sendable {
        case swiftLint = "SwiftLint"
        case swiftFormat = "swift-format"
    }

    /*
     How two findings count as the same. Most rules point at a line. SwiftLint reports its whole-file rules
     once at line 1, so `file_name` can only be compared by which files it fires in, and
     `one_declaration_per_file` flags every declaration after the first where cohere flags every one but the
     file's own type, so the same file holds the same number of findings on different lines.
     */
    enum Comparison: Sendable {
        case line
        case fileAndCount
        case file
    }

    var incumbent: Incumbent
    var incumbentRule: String
    var rules: [String]
    var comparison: Comparison = .line
    /* When one cohere rule covers several incumbent rules, the message ids of the family this row compares; empty means every finding of the rules. */
    var messageIds: [String] = []

    static let all: [RuleMapping] = [
        RuleMapping(incumbent: .swiftLint, incumbentRule: "force_unwrapping", rules: ["cohere-swift/no-force-unwrap"]),
        RuleMapping(incumbent: .swiftFormat, incumbentRule: "NeverForceUnwrap", rules: ["cohere-swift/no-force-unwrap", "cohere-swift/no-force-cast"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "force_try", rules: ["cohere-swift/no-force-try"]),
        RuleMapping(incumbent: .swiftFormat, incumbentRule: "NeverUseForceTry", rules: ["cohere-swift/no-force-try"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "force_cast", rules: ["cohere-swift/no-force-cast"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "implicitly_unwrapped_optional", rules: ["cohere-swift/no-implicitly-unwrapped-optional"]),
        RuleMapping(incumbent: .swiftFormat, incumbentRule: "NeverUseImplicitlyUnwrappedOptionals", rules: ["cohere-swift/no-implicitly-unwrapped-optional"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "fatal_error_message", rules: ["cohere-swift/fatal-error-message"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "todo", rules: ["cohere-swift/no-todo-comment"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "one_declaration_per_file", rules: ["cohere-swift/one-type-per-file"], comparison: .fileAndCount),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "file_name", rules: ["cohere-swift/file-named-for-type"], comparison: .file),
        RuleMapping(incumbent: .swiftFormat, incumbentRule: "AlwaysUseLowerCamelCase", rules: ["cohere-swift/require-lower-camel-case"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "unowned_variable_capture", rules: ["cohere-swift/no-unowned"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "private_over_fileprivate", rules: ["cohere-swift/private-over-fileprivate"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "duplicate_imports", rules: ["cohere-swift/duplicate-imports"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "duplicated_key_in_dictionary_literal", rules: ["cohere-swift/duplicated-dictionary-key"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "identical_operands", rules: ["cohere-swift/identical-operands"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "duplicate_conditions", rules: ["cohere-swift/duplicate-conditions"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "unused_optional_binding", rules: ["cohere-swift/unused-optional-binding"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "unused_closure_parameter", rules: ["cohere-swift/unused-closure-parameter"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "legacy_constructor", rules: ["cohere-swift/legacy-constructors"], messageIds: ["legacyConstructor"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "legacy_cggeometry_functions", rules: ["cohere-swift/legacy-constructors"], messageIds: ["legacyCGGeometryFunction"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "legacy_nsgeometry_functions", rules: ["cohere-swift/legacy-constructors"], messageIds: ["legacyNSGeometryFunction"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "legacy_random", rules: ["cohere-swift/legacy-constructors"], messageIds: ["legacyRandom"]),
        RuleMapping(incumbent: .swiftFormat, incumbentRule: "NoLeadingUnderscores", rules: ["cohere-swift/no-leading-underscores"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "empty_count", rules: ["cohere-swift/empty-count"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "contains_over_filter_count", rules: ["cohere-swift/contains-over-filter"], messageIds: ["filterCount"]),
        RuleMapping(incumbent: .swiftLint, incumbentRule: "contains_over_filter_is_empty", rules: ["cohere-swift/contains-over-filter"], messageIds: ["filterIsEmpty"]),
    ]

    static func incumbentRules(of incumbent: Incumbent) -> [String] {
        all.filter { $0.incumbent == incumbent }.map(\.incumbentRule)
    }
}
