import Foundation

/*
 The shouting vocabulary, cohere's `policy/Shouting.json`, the same file the TypeScript rule
 `nexus/consistency-no-shouting` embeds, so Swift and TypeScript call the same words acronyms (#y8whqbe): the
 words that are never shouting, in groups that each carry their reason, and the two-letter words that are.

 The words are compiled in (`PolicyShouting`, generated from the file), so a released engine carries them with no
 checkout and nothing handed to it at run time. A test fails while the compiled-in copy and the file differ. A copy
 that would not load refuses the run with exit 2 (`Pipeline`), never a rule that quietly reads every acronym with a
 vowel as shouting.
 */
public struct ShoutingVocabulary: Sendable {
    struct LoadFailure: Error, CustomStringConvertible {
        var description: String
    }

    /* Every allowed group's words, which are never shouting whatever their shape. */
    private(set) var allowed: Set<String> = []
    /* The two-letter words the vowel test cannot judge, which are shouting in capitals. */
    private(set) var shoutedTwoLetterWords: Set<String> = []

    /* The letters whose presence makes a word of three or more pronounceable, which an acronym usually is not. */
    static let vowels = Set("AEIOUY".utf8)

    /* An empty vocabulary judges nothing, which is right only where nothing is judged: listing rule names, or a run with the rule off. */
    public init() {}

    /* The compiled-in words. The reason alone, in words: the caller says what failed to load, once, in the sentence a person reads. */
    public static func compiledIn() throws -> ShoutingVocabulary {
        do {
            return try load(data: Data(PolicyShouting.file.utf8))
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
     `DisallowUnknownFields` refuses it: a misspelled group would otherwise be a list that silently allows nothing.
     Each check is the Go loader's (`shouting_vocabulary.go`), so a file one engine refuses the other refuses too.
     */
    static func load(data: Data) throws -> ShoutingVocabulary {
        guard let file = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw LoadFailure(description: "the file is not a JSON object")
        }
        try refuseUnknownKeys(in: file, allowed: ["about", "allowed", "shoutedTwoLetterWords"], context: "the file")

        var vocabulary = ShoutingVocabulary()
        let groups = file["allowed"] as? [[String: Any]] ?? []
        guard !groups.isEmpty else {
            throw LoadFailure(description: "no allowed group, so every acronym with a vowel would read as shouting")
        }
        for (index, group) in groups.enumerated() {
            try refuseUnknownKeys(in: group, allowed: ["reason", "tokens"], context: "allowed group \(index)")
            let tokens = group["tokens"] as? [String] ?? []
            guard !(group["reason"] as? String ?? "").isEmpty, !tokens.isEmpty else {
                throw LoadFailure(description: "allowed group \(index) needs a reason and at least one token")
            }
            for token in tokens {
                guard isUppercaseWord(token) else {
                    throw LoadFailure(
                        description: "allowed group \(index) lists \"\(token)\", which is not an uppercase word"
                    )
                }
                vocabulary.allowed.insert(token)
            }
        }

        let twoLetterWords = file["shoutedTwoLetterWords"] as? [String: Any] ?? [:]
        try refuseUnknownKeys(in: twoLetterWords, allowed: ["reason", "words"], context: "shoutedTwoLetterWords")
        let words = twoLetterWords["words"] as? [String] ?? []
        guard !(twoLetterWords["reason"] as? String ?? "").isEmpty, !words.isEmpty else {
            throw LoadFailure(description: "shoutedTwoLetterWords needs a reason and at least one word")
        }
        for word in words {
            guard word.utf8.count == 2, isUppercaseWord(word) else {
                throw LoadFailure(
                    description: "shoutedTwoLetterWords lists \"\(word)\", which is not a two-letter uppercase word"
                )
            }
            vocabulary.shoutedTwoLetterWords.insert(word)
        }
        return vocabulary
    }

    private static func refuseUnknownKeys(in object: [String: Any], allowed: Set<String>, context: String) throws {
        if let unknown = object.keys.sorted().first(where: { !allowed.contains($0) }) {
            throw LoadFailure(description: "\(context) has an unknown field \"\(unknown)\"")
        }
    }

    /* The shape the rule's words have, `[A-Z][A-Z0-9_]*`, so a word the rule could never see is refused instead of listed. */
    static func isUppercaseWord(_ word: String) -> Bool {
        let bytes = Array(word.utf8)
        guard let first = bytes.first, first >= UInt8(ascii: "A"), first <= UInt8(ascii: "Z") else { return false }
        return bytes.allSatisfy { ConsistencyNoShouting.isUppercaseWordCharacter($0) }
    }

    /*
     Whether an uppercase word is shouting rather than an acronym, the judgment of the TypeScript rule's
     `isShoutedToken`: an allowed word never is; a digit or an underscore makes it an identifier (`C1`, `DATABASE_URL`);
     a one-letter word is not judged; a two-letter word is shouting only when named, since `IS` and `IP` have the same
     shape; and past two letters a vowel is the tell of a pronounceable word, where a consonant cluster is an acronym.
     */
    func isShouted(_ word: String) -> Bool {
        if allowed.contains(word) {
            return false
        }
        let bytes = Array(word.utf8)
        if bytes.contains(where: { ($0 >= UInt8(ascii: "0") && $0 <= UInt8(ascii: "9")) || $0 == UInt8(ascii: "_") }) {
            return false
        }
        if bytes.count < 2 {
            return false
        }
        if bytes.count == 2 {
            return shoutedTwoLetterWords.contains(word)
        }
        return bytes.contains { Self.vowels.contains($0) }
    }
}
