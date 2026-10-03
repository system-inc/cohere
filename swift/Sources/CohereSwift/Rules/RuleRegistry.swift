/*
 Every rule this binary implements. Compiled in rather than loaded, the same choice the Go engine makes:
 a rule costs nothing to run because nothing crosses a boundary to reach it.
 */
public enum RuleRegistry {
    /*
     Every file rule, built from what a run loads. Today that is only the abbreviation vocabulary, which the
     pipeline reads before anything is checked, so a vocabulary that is missing refuses the run instead of
     leaving the naming rule judging nothing.
     */
    public static func fileRules(vocabulary: AbbreviationVocabulary) -> [any FileRule] { [
        NoForceUnwrap(),
        NoForceTry(),
        PerformanceNoIndependentAwaitInLoop(),
        NoForceCast(),
        NoImplicitlyUnwrappedOptional(),
        FileNamedForType(),
        MaxFileLines(),
        NoPrint(),
        RequireEscapeHatchReason(),
        FatalErrorMessage(),
        NoTodoComment(),
        NoAbbreviatedIdentifier(vocabulary: vocabulary),
        NoUnowned(),
        PrivateOverFileprivate(),
        DuplicateImports(),
        DuplicatedDictionaryKey(),
        IdenticalOperands(),
        DuplicateConditions(),
        UnusedOptionalBinding(),
        UnusedClosureParameter(),
        LegacyConstructors(),
        NoAmbiguousIdentifier(),
        NoStutteringName(),
        NoUtilsFolder(),
        NoBareThrow(),
        ConcurrencyNoLostUpdate(),
        SwiftFormatRule.requireLowerCamelCase,
        SwiftFormatRule.noLeadingUnderscores,
    ] }

    /* The rules that read what names resolve to; see `TypedFileRule` for what a run fetches for them. */
    public static let typedRules: [any TypedFileRule] = [
        EmptyCount(),
        ContainsOverFilter(),
        ContainsOverFirstNotNil(),
        SortedFirstLast(),
        FirstWhere(),
        LastWhere(),
        IsDisjoint(),
        ReduceInto(),
        NoDefaultForOwnedEnum(),
        NoUnhandledThrowingTask(),
        RedundantTypeAnnotation(),
        NoDiscardedTryOptional(),
        SecurityNoInterpolatedShellCommand(),
        SecurityNoInterpolatedSqlString(),
        ConcurrencyNoCheckThenWrite(),
        ConsistencyNoIsoStringDateCut(),
        CorrectnessRequireResponseStatusCheck(),
    ]

    public static let packageRules: [any PackageRule] = [
        RequireSwiftSixLanguageMode(),
        RequireUpcomingFeatures(),
        RequireStrictMemorySafety(),
    ]

    /* Sorted, so two binaries' lists can be compared with `diff`. */
    public static var allNames: [String] {
        /* Names only, so no vocabulary is read: `--rules` answers without one. */
        (fileRules(vocabulary: AbbreviationVocabulary()).map(\.name) + typedRules.map(\.name) + packageRules.map(\.name)).sorted()
    }
}
