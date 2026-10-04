/*
 Generated from cohere's policy/messages/ by `go run ./policy/tools/generate`. Do not edit it here:
 edit the message files and run that again. While the two differ, a test in each engine fails.
 */
enum RuleMessages {
    /* A finding's message: the id the catalog files it under, and its text. */
    struct Message: Equatable, Sendable {
        let id: String
        let text: String
    }

    /* SHA-256 of the messages that reach Swift, resolved, which a test recomputes from policy/messages/ on disk. */
    static let sourceDigest = "9abd5925ed7a50524ebad3a534997cad0a005e94ed2285bb68fa1936e5c72ae8"

    /* Every message here, as `Rule.id`, for the test that fails on one no rule renders. */
    static let all = [
        "ConcurrencyRequireEscapeHatchReason.escapeHatchWithoutReason",
        "ConsistencyNoBareThrow.bareThrow",
        "CorrectnessNoDefaultForOwnedEnum.noDefaultForOwnedEnum",
        "CorrectnessNoDiscardedTryOptional.discardedTryOptional",
        "CorrectnessNoDiscardedTryOptional.discardedTryOptionalInClosure",
        "SecurityNoInterpolatedSqlString.interpolatedSqlString",
        "ToolchainRequireStrictMemorySafety.strictMemorySafetyMissing",
        "ToolchainRequireSwift6LanguageMode.languageModeBelowSix",
        "ToolchainRequireUpcomingFeatures.upcomingFeatureMissing",
    ]

    /* cohere-swift/always-use-lower-camel-case */
    enum AlwaysUseLowerCamelCase {
    }

    /* cohere-swift/concurrency-no-check-then-write */
    enum ConcurrencyNoCheckThenWrite {
    }

    /* cohere-swift/concurrency-no-lost-update */
    enum ConcurrencyNoLostUpdate {
    }

    /* cohere-swift/concurrency-require-escape-hatch-reason */
    enum ConcurrencyRequireEscapeHatchReason {
        static func escapeHatchWithoutReason(spelling: String) -> Message {
            Message(
                id: "escapeHatchWithoutReason",
                text:
                    #"\#(spelling) tells the compiler to trust this code instead of checking it. Say why that is safe in a comment directly above the declaration, so the next reader can check the reasoning the compiler no longer does."#,
            )
        }
    }

    /* cohere-swift/consistency-no-ambiguous-identifier */
    enum ConsistencyNoAmbiguousIdentifier {
    }

    /* cohere-swift/consistency-no-bare-throw */
    enum ConsistencyNoBareThrow {
        static func bareThrow(domain: String) -> Message {
            Message(
                id: "bareThrow",
                text:
                    #"This throws an NSError made up here, with the domain \#(domain), which names no declared failure. Declare the failure as a case of an error type of our own (an enum conforming to Error, and LocalizedError for its text) and throw that case, so a caller catches it by case. Until then a caller can match it only by repeating the domain string and the code, and nothing keeps the two in step."#,
            )
        }
    }

    /* cohere-swift/consistency-no-boolean-outcome */
    enum ConsistencyNoBooleanOutcome {
    }

    /* cohere-swift/consistency-no-hand-rolled-delay */
    enum ConsistencyNoHandRolledDelay {
    }

    /* cohere-swift/consistency-no-iso-string-date-cut */
    enum ConsistencyNoIsoStringDateCut {
    }

    /* cohere-swift/consistency-no-stuttering-name */
    enum ConsistencyNoStutteringName {
    }

    /* cohere-swift/consistency-no-utils-folder */
    enum ConsistencyNoUtilsFolder {
    }

    /* cohere-swift/correctness-no-default-for-owned-enum */
    enum CorrectnessNoDefaultForOwnedEnum {
        static func noDefaultForOwnedEnum(enumName: String) -> Message {
            Message(
                id: "noDefaultForOwnedEnum",
                text:
                    #"This default answers for every case of \#(enumName), including any added later, so the compiler can no longer say this switch does not handle a new one. List the remaining cases instead."#,
            )
        }
    }

    /* cohere-swift/correctness-no-discarded-try-optional */
    enum CorrectnessNoDiscardedTryOptional {
        static func discardedTryOptional() -> Message {
            Message(
                id: "discardedTryOptional",
                text:
                    #"This try? throws the error away and keeps nothing, so a failure here is invisible. Use do/catch, and say in the catch why the failure can be ignored if it can."#,
            )
        }

        static func discardedTryOptionalInClosure() -> Message {
            Message(
                id: "discardedTryOptionalInClosure",
                text:
                    #"This try? is the whole body of a closure whose result goes nowhere (it returns nothing, or it is the value of a task nobody keeps), so the error is thrown away unseen. Use do/catch inside the closure, and say in the catch why the failure can be ignored if it can."#,
            )
        }
    }

    /* cohere-swift/correctness-no-identical-branches */
    enum CorrectnessNoIdenticalBranches {
    }

    /* cohere-swift/correctness-no-uncleared-race-timeout */
    enum CorrectnessNoUnclearedRaceTimeout {
    }

    /* cohere-swift/correctness-no-write-only-collection */
    enum CorrectnessNoWriteOnlyCollection {
    }

    /* cohere-swift/correctness-require-response-status-check */
    enum CorrectnessRequireResponseStatusCheck {
    }

    /* cohere-swift/force-cast */
    enum ForceCast {
    }

    /* cohere-swift/performance-no-independent-await-in-loop */
    enum PerformanceNoIndependentAwaitInLoop {
    }

    /* cohere-swift/security-no-interpolated-shell-command */
    enum SecurityNoInterpolatedShellCommand {
    }

    /* cohere-swift/security-no-interpolated-sql-string */
    enum SecurityNoInterpolatedSqlString {
        static func interpolatedSqlString() -> Message {
            Message(
                id: "interpolatedSqlString",
                text:
                    #"This value is written between the single quotes of a SQL string, so a quote in it ends the string early: the query breaks on ordinary input like O'Brien, and a crafted value runs as SQL. Bind it as a parameter instead (WHERE name = ? with the value in the parameters beside the statement). Where the query cannot take parameters, give the value a type that cannot hold a quote (an integer, a closed enum's case), or escape it in place by doubling backslashes and quotes: .replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "'", with: "''")."#,
            )
        }
    }

    /* cohere-swift/toolchain-require-strict-memory-safety */
    enum ToolchainRequireStrictMemorySafety {
        static func strictMemorySafetyMissing(target: String) -> Message {
            Message(
                id: "strictMemorySafetyMissing",
                text:
                    #"\#(target) does not compile under strict memory safety. Add .strictMemorySafety() to its swiftSettings, so every unsafe construct is visible where it is used; then replace each flagged site with a safe API where one exists, and mark unsafe only what must be."#,
            )
        }
    }

    /* cohere-swift/toolchain-require-swift-6-language-mode */
    enum ToolchainRequireSwift6LanguageMode {
        static func languageModeBelowSix(target: String, languageMode: String) -> Message {
            Message(
                id: "languageModeBelowSix",
                text:
                    #"\#(target) compiles in Swift \#(languageMode) language mode. Swift 6 is required: it makes data-race safety a compile error instead of a warning nobody reads. Set .swiftLanguageMode(.v6) on the target, or remove the setting that lowers it."#,
            )
        }
    }

    /* cohere-swift/toolchain-require-upcoming-features */
    enum ToolchainRequireUpcomingFeatures {
        static func upcomingFeatureMissing(
            target: String,
            features: String,
            settings: String,
            reasons: String,
        ) -> Message {
            Message(
                id: "upcomingFeatureMissing",
                text:
                    #"\#(target) does not enable \#(features). Add \#(settings) to its swiftSettings. \#(reasons)."#,
            )
        }
    }
}
