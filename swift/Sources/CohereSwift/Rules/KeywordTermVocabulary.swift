import Foundation

/*
 The keyword terms, cohere's `policy/KeywordTerms.json`, the same file the TypeScript and Go engines read through
 `internal/naming/abbreviation`, so all three read one list of the spellings in which a language's keyword means the
 keyword rather than an abbreviation (#hvyvfr5): JavaScript's `var` in `isVarDeclaration`, which is no "variable".
 The abbreviation vocabulary reads it, so a `Var` segment that opens a listed spelling is not reported, and every
 other `Var` still is.

 The words are compiled in (`PolicyKeywordTerms`, generated from the file), so a released engine carries them with no
 checkout and nothing handed to it at run time. A test fails while the compiled-in copy and the file differ. A copy
 that would not load refuses the run with exit 2 (`Pipeline`, through the abbreviation vocabulary that reads it).
 */
public struct KeywordTermVocabulary: Sendable {
    /* One keyword: the keyword as its language spells it, and the names in which the capitalized keyword means it. */
    struct KeywordTerm: Sendable, Equatable {
        var term: String
        var spellings: [String]
    }

    struct LoadFailure: Error, CustomStringConvertible {
        var description: String
    }

    private(set) var keywordTerms: [KeywordTerm] = []

    /* An empty vocabulary lists no keyword, so every use of an abbreviation's letters is judged as the abbreviation. */
    public init() {}

    /* The compiled-in keyword terms. The reason alone, in words: the caller says what failed to load, once. */
    public static func compiledIn() throws -> KeywordTermVocabulary {
        do {
            return try load(data: Data(PolicyKeywordTerms.file.utf8))
        }
        catch let failure as LoadFailure {
            throw failure
        }
        catch {
            throw LoadFailure(description: "it is not valid JSON (\((error as NSError).localizedDescription))")
        }
    }

    /*
     Parsed by hand from JSON objects rather than decoded, because an unknown key must be refused, as Go's
     `DisallowUnknownFields` refuses it. Each check is the Go loader's (`policy/keyword_terms.go`), so a file one
     engine refuses the other refuses too: a file that does not state its bar, a term that is not lowercase letters,
     an entry with no language, no spellings, no reason or no https citation, a spelling that does not open with the
     term capitalized and then a capital, or holds anything but ASCII letters and digits, and a term or a spelling
     listed twice.
     */
    static func load(data: Data) throws -> KeywordTermVocabulary {
        guard let file = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw LoadFailure(description: "the file is not a JSON object")
        }
        try refuseUnknownKeys(in: file, allowed: ["about", "keywordTerms"], context: "the file")
        guard !(file["about"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            throw LoadFailure(description: "about is empty, and the file states the bar every entry meets")
        }
        var vocabulary = KeywordTermVocabulary()
        var seen: Set<String> = []
        for (index, entry) in (file["keywordTerms"] as? [[String: Any]] ?? []).enumerated() {
            try refuseUnknownKeys(
                in: entry,
                allowed: ["term", "language", "spellings", "reason", "citation"],
                context: "keyword term \(index)",
            )
            let term = entry["term"] as? String ?? ""
            /* A list of names or nothing, as Go's decoder reads `[]string`: a lone string is refused rather than read as no spelling. */
            let spellings: [String]
            switch entry["spellings"] {
                case nil: spellings = []
                case let names as [String]: spellings = names
                default: throw LoadFailure(description: "\"\(term)\" spellings is not a list of names")
            }
            if let refusal = refusal(
                term: term,
                language: entry["language"] as? String ?? "",
                spellings: spellings,
                reason: entry["reason"] as? String ?? "",
                citation: entry["citation"] as? String ?? "",
            ) {
                throw LoadFailure(description: "\"\(term)\" \(refusal)")
            }
            guard seen.insert(term).inserted else {
                throw LoadFailure(description: "\"\(term)\" appears twice")
            }
            vocabulary.keywordTerms.append(KeywordTerm(term: term, spellings: spellings))
        }
        return vocabulary
    }

    /* Why an entry is refused, or nil when it is not: Go's `keywordTermRefusal`, check for check. */
    static func refusal(
        term: String,
        language: String,
        spellings: [String],
        reason: String,
        citation: String,
    )
        -> String?
    {
        let termBytes = Array(term.utf8)
        guard !termBytes.isEmpty, termBytes.allSatisfy(AbbreviationVocabulary.isLowercase) else {
            return "is not lowercase letters a to z, as the abbreviation it speaks for is"
        }
        guard !language.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return "names no language, and a keyword is a keyword of one"
        }
        guard !spellings.isEmpty else {
            return "lists no spellings, so it would read nothing as the keyword"
        }
        let capitalized = AbbreviationVocabulary.capitalized(term)
        let isLetterOrDigit: (UInt8) -> Bool = { byte in
            AbbreviationVocabulary.isLetter(byte) || AbbreviationVocabulary.isDigit(byte)
        }
        var seen: Set<String> = []
        for spelling in spellings {
            let bytes = Array(spelling.utf8)
            let opening = capitalized.utf8.count
            guard bytes.starts(with: capitalized.utf8), bytes.count > opening,
                AbbreviationVocabulary.isUppercase(bytes[opening])
            else {
                return "lists \"\(spelling)\", which does not open with \"\(capitalized)\" and then a new word, "
                    + "and every spelling names the next word"
            }
            guard bytes[(opening + 1)...].allSatisfy(isLetterOrDigit) else {
                return "lists \"\(spelling)\", which holds something other than letters and digits"
            }
            guard seen.insert(spelling).inserted else {
                return "lists \"\(spelling)\" twice"
            }
        }
        guard !reason.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return "gives no reason, and every entry says why its letters are a keyword and not an abbreviation"
        }
        guard let page = URL(string: citation), page.scheme == "https", let host = page.host, !host.isEmpty else {
            return "cites no https page where its language spells the keyword, and every entry cites one"
        }
        return nil
    }

    private static func refuseUnknownKeys(in object: [String: Any], allowed: Set<String>, context: String) throws {
        if let unknown = object.keys.sorted().first(where: { !allowed.contains($0) }) {
            throw LoadFailure(description: "\(context) has an unknown field \"\(unknown)\"")
        }
    }
}
