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

    /* SHA-256 of every .json file in policy/messages/, each one's name and bytes in name order, which a test recomputes from the files on disk. */
    static let sourceDigest = "445a85674d16ba891fd27721b6b686d81ad1973c85a1759227da8e872efdf907"

    /* Every message here, as `Rule.id`, for the test that fails on one no rule renders. */
    static let all = [
        "ConsistencyNoBareThrow.bareThrow",
        "SecurityNoInterpolatedSqlString.interpolatedSqlString",
    ]

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
}
