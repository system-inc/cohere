/*
 Every rule this binary implements. Compiled in rather than loaded, the same choice the Go engine makes:
 a rule costs nothing to run because nothing crosses a boundary to reach it.
 */
public enum RuleRegistry {
    public static let fileRules: [any FileRule] = [
        NoForceUnwrap(),
        NoForceTry(),
        NoForceCast(),
        NoImplicitlyUnwrappedOptional(),
    ]

    public static let packageRules: [any PackageRule] = [
        RequireSwiftSixLanguageMode(),
    ]

    /* Sorted, so two binaries' lists can be compared with `diff`. */
    public static var allNames: [String] {
        (fileRules.map(\.name) + packageRules.map(\.name)).sorted()
    }
}
