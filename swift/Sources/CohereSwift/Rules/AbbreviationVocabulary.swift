import Foundation

/*
 The abbreviation vocabulary, cohere's `policy/Abbreviations.json`, the same file the Go rule
 `nexus/consistency-no-abbreviated-identifier` embeds, so Swift and TypeScript judge the same words.

 The evaluation is a port of `abbreviation_vocabulary.go`, and its order is observable, so it is kept
 exactly: allowed name, whole word, early prefixes, suffixes, late prefixes, then stop if an allowed
 segment is present, then segments. The first match wins, and within a form the file's order is the
 order (`timeoutMsRef` reports the millisecond form because `ms` precedes `ref`). Messages are built
 byte for byte as Go builds them, and a differential over the same names checks that.

 Names are scanned as ASCII bytes, the way Go's regular expressions read them, rather than through
 Swift's Unicode-aware `Character`: `[a-z]` means those 26 bytes in both engines.

 The words are compiled in (`PolicyAbbreviations`, generated from the file), so a released engine carries
 them with no checkout and nothing handed to it at run time (#2cqemcd). A test fails while the compiled-in
 copy and the file differ. A copy that would not load refuses the run with exit 2 (`Pipeline`), never a
 rule that quietly judges nothing.

 The keyword terms (`KeywordTermVocabulary`, cohere's `policy/KeywordTerms.json`) are read with the words, as Go's
 `abbreviation.Load` reads them (#hvyvfr5): a segment that opens one of a keyword's listed spellings at a word
 boundary is the keyword, not the abbreviation, so `isVarDeclaration` is clean and `NoVar` is still reported.
 */
public struct AbbreviationVocabulary: Sendable {
    /* One abbreviated word and the forms it is judged in. */
    struct Entry: Sendable {
        var abbreviation: String
        /* The words it stands in for, the most common meaning first. A word with more than one meaning lists every one, and every message names them all. */
        var expansions: [String]
        var advice: String
        /* The whole form's style, when the word is judged as a whole name. */
        var whole: String?
        var prefix: (phase: String, style: String)?
        var suffix: (advice: String, matcher: String, replacement: String)?
        /* The segment form's style, when the word is judged as a camelCase word anywhere in a name. */
        var segment: String?
        /* The names `policy/KeywordTerms.json` lists for the word as a language's keyword, or none where it is no keyword. */
        var keywordTermSpellings: [[UInt8]] = []
    }

    /* What the vocabulary says about one name: the form and word that matched, and the message. */
    public struct Finding: Equatable, Sendable {
        public var form: String
        public var abbreviation: String
        public var messageId: String
        public var message: String

        /* A finding whose id and text come from the house catalog: the four shapes every word reports in, shared with TypeScript. */
        init(form: String, abbreviation: String, message: RuleMessages.Message) {
            self.form = form
            self.abbreviation = abbreviation
            self.messageId = message.id
            self.message = message.text
        }
    }

    struct LoadFailure: Error, CustomStringConvertible {
        var description: String
    }

    private(set) var wholeByName: [String: Entry] = [:]
    private(set) var earlyPrefixes: [Entry] = []
    private(set) var latePrefixes: [Entry] = []
    private(set) var suffixes: [Entry] = []
    private(set) var segments: [Entry] = []
    private(set) var allowedNames: Set<String> = []
    private(set) var allowedSegments: [String] = []

    /* An empty vocabulary judges nothing, which is right only where nothing is judged: listing rule names, or a run with the rule off. */
    public init() {}

    /* The compiled-in words. The reason alone, in words: the caller says what failed to load, once, in the sentence a person reads. */
    public static func compiledIn() throws -> AbbreviationVocabulary {
        let keywordTerms: KeywordTermVocabulary
        do {
            keywordTerms = try KeywordTermVocabulary.compiledIn()
        }
        catch {
            throw LoadFailure(description: "policy/KeywordTerms.json: \(error)")
        }
        do {
            return try load(data: Data(PolicyAbbreviations.file.utf8), keywordTerms: keywordTerms)
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
     `DisallowUnknownFields` refuses it: a misspelled form would otherwise be an entry that silently never
     matches. A keyword term is refused where no entry's segment form gives advice for its word, as Go refuses
     it: a rename rewrites the word's first segment, which may be the keyword's own.
     */
    static func load(
        data: Data,
        keywordTerms: KeywordTermVocabulary = KeywordTermVocabulary(),
    ) throws
        -> AbbreviationVocabulary
    {
        guard let file = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw LoadFailure(description: "the file is not a JSON object")
        }
        try refuseUnknownKeys(
            in: file,
            allowed: ["about", "abbreviations", "allowedNames", "allowedSegments"],
            context: "the file",
        )

        var vocabulary = AbbreviationVocabulary()
        var seen = Set<String>()
        for (index, object) in (file["abbreviations"] as? [[String: Any]] ?? []).enumerated() {
            let entry = try parseEntry(object, index: index)
            guard seen.insert(entry.abbreviation).inserted else {
                throw LoadFailure(description: "entry \"\(entry.abbreviation)\" appears twice")
            }
            if entry.whole != nil {
                vocabulary.wholeByName[entry.abbreviation] = entry
            }
            if let prefix = entry.prefix {
                if prefix.phase == "early" {
                    vocabulary.earlyPrefixes.append(entry)
                }
                else {
                    vocabulary.latePrefixes.append(entry)
                }
            }
            if entry.suffix != nil {
                vocabulary.suffixes.append(entry)
            }
            if entry.segment != nil {
                vocabulary.segments.append(entry)
            }
        }
        for keywordTerm in keywordTerms.keywordTerms {
            guard let index = vocabulary.segments.firstIndex(where: { $0.abbreviation == keywordTerm.term }),
                vocabulary.segments[index].segment == "advice"
            else {
                throw LoadFailure(
                    description:
                        "keyword term \"\(keywordTerm.term)\" names no entry whose segment form gives advice, the one form that can carry it"
                )
            }
            vocabulary.segments[index].keywordTermSpellings += keywordTerm.spellings.map { Array($0.utf8) }
        }
        for allowed in file["allowedNames"] as? [[String: Any]] ?? [] {
            try refuseUnknownKeys(in: allowed, allowed: ["name", "reason"], context: "an allowed name")
            vocabulary.allowedNames.insert(allowed["name"] as? String ?? "")
        }
        for allowed in file["allowedSegments"] as? [[String: Any]] ?? [] {
            try refuseUnknownKeys(in: allowed, allowed: ["segment", "reason"], context: "an allowed segment")
            vocabulary.allowedSegments.append(allowed["segment"] as? String ?? "")
        }
        return vocabulary
    }

    private static func parseEntry(_ object: [String: Any], index: Int) throws -> Entry {
        let abbreviation = object["abbreviation"] as? String ?? ""
        let context = "entry \(index) (\"\(abbreviation)\")"
        try refuseUnknownKeys(
            in: object,
            allowed: ["abbreviation", "expansions", "advice", "reason", "whole", "prefix", "suffix", "segment"],
            context: context,
        )
        guard !abbreviation.isEmpty, abbreviation.lowercased() == abbreviation else {
            throw LoadFailure(description: "\(context): the abbreviation must be lowercase and present")
        }
        /* A list of words or nothing, as Go's decoder reads `[]string`: a lone string is refused rather than read as no expansion. */
        let expansions: [String]
        switch object["expansions"] {
            case nil: expansions = []
            case let words as [String]: expansions = words
            default: throw LoadFailure(description: "\(context): expansions is not a list of words")
        }
        var entry = Entry(
            abbreviation: abbreviation,
            expansions: expansions,
            advice: object["advice"] as? String ?? "",
        )
        var needsExpansion = false
        if let whole = object["whole"] as? [String: Any] {
            try refuseUnknownKeys(in: whole, allowed: ["style"], context: "\(context) whole")
            let style = whole["style"] as? String ?? ""
            switch style {
                case "orDescriptive", "plain": needsExpansion = true
                case "advice" where !entry.advice.isEmpty: break
                case "advice":
                    throw LoadFailure(description: "\(context): whole style is advice and the entry has none")
                default: throw LoadFailure(description: "\(context): unknown whole style \"\(style)\"")
            }
            entry.whole = style
        }
        if let prefix = object["prefix"] as? [String: Any] {
            try refuseUnknownKeys(in: prefix, allowed: ["phase", "style"], context: "\(context) prefix")
            let phase = prefix["phase"] as? String ?? ""
            guard phase == "early" || phase == "late" else {
                throw LoadFailure(description: "\(context): unknown prefix phase \"\(phase)\"")
            }
            let style = prefix["style"] as? String ?? ""
            switch style {
                case "": needsExpansion = true
                case "advice" where !entry.advice.isEmpty: break
                case "advice":
                    throw LoadFailure(description: "\(context): prefix style is advice and the entry has none")
                default: throw LoadFailure(description: "\(context): unknown prefix style \"\(style)\"")
            }
            entry.prefix = (phase, style)
        }
        if let suffix = object["suffix"] as? [String: Any] {
            try refuseUnknownKeys(
                in: suffix,
                allowed: ["advice", "matcher", "replacement"],
                context: "\(context) suffix",
            )
            let matcher = suffix["matcher"] as? String ?? ""
            guard matcher.isEmpty || matcher == "millisecondWord" else {
                throw LoadFailure(description: "\(context): unknown suffix matcher \"\(matcher)\"")
            }
            let advice = suffix["advice"] as? String ?? ""
            let replacement = suffix["replacement"] as? String ?? ""
            if advice.isEmpty && replacement.isEmpty {
                needsExpansion = true
            }
            entry.suffix = (advice, matcher, replacement)
        }
        if let segment = object["segment"] {
            guard let fields = segment as? [String: Any] else {
                throw LoadFailure(description: "\(context): the segment form is not an object")
            }
            try refuseUnknownKeys(in: fields, allowed: ["style"], context: "\(context) segment")
            let style = fields["style"] as? String ?? ""
            switch style {
                case "": needsExpansion = true
                case "advice" where !entry.advice.isEmpty: break
                case "advice":
                    throw LoadFailure(description: "\(context): segment style is advice and the entry has none")
                default: throw LoadFailure(description: "\(context): unknown segment style \"\(style)\"")
            }
            entry.segment = style
        }
        guard entry.whole != nil || entry.prefix != nil || entry.suffix != nil || entry.segment != nil else {
            throw LoadFailure(description: "\(context): no form, so the entry judges nothing")
        }
        if needsExpansion && entry.expansions.isEmpty {
            throw LoadFailure(description: "\(context): a form suggests the expansion and the entry has none")
        }
        /* An empty expansion would print `Use ""`, or fold a name's own tail into the suggestion as if it were the word. */
        if entry.expansions.contains("") {
            throw LoadFailure(description: "\(context): an expansion is empty")
        }
        return entry
    }

    private static func refuseUnknownKeys(in object: [String: Any], allowed: Set<String>, context: String) throws {
        if let unknown = object.keys.sorted().first(where: { !allowed.contains($0) }) {
            throw LoadFailure(description: "\(context) has an unknown field \"\(unknown)\"")
        }
    }

    /* `prop` becomes `Prop`, the spelling a suffix or segment form matches. */
    static func capitalized(_ word: String) -> String {
        word.prefix(1).uppercased() + word.dropFirst()
    }

    /* A list of names as a message reads it: `"a"`, `"a" or "b"`, or `"a", "b" or "c"`, as Go's `quoted` writes it. */
    static func quoted(_ names: [String]) -> String {
        let quotedNames = names.map { "\"\($0)\"" }
        guard let last = quotedNames.last, quotedNames.count > 1 else {
            return quotedNames.joined()
        }
        return quotedNames.dropLast().joined(separator: ", ") + " or " + last
    }

    /* `Use "a".` for one candidate, byte for byte what every message said before a word could mean two things; more than one names them all. */
    static func useAdvice(_ candidates: [String]) -> String {
        if candidates.count == 1 {
            return "Use \"\(candidates[0])\"."
        }
        return "Use \(quoted(candidates)), whichever it means."
    }

    /* The entry for an abbreviation a finding names, whichever form it matched in: the file-name rule reads its expansions and advice, as Go's reads the finding's entry. */
    func entry(for abbreviation: String) -> Entry? {
        wholeByName[abbreviation]
            ?? (earlyPrefixes + suffixes + latePrefixes + segments).first { $0.abbreviation == abbreviation }
    }

    /* What the vocabulary reports for a name, if anything, in the order the Go rule has always used. */
    public func find(_ name: String) -> Finding? {
        if allowedNames.contains(name) {
            return nil
        }
        if let entry = wholeByName[name], let whole = entry.whole {
            let advice: String
            switch whole {
                case "advice": advice = entry.advice
                case "plain": advice = Self.useAdvice(entry.expansions)
                case _ where entry.expansions.count > 1:
                    advice = "Use \(Self.quoted(entry.expansions)), whichever it means, or a more descriptive name."
                default: advice = "Use \(Self.quoted(entry.expansions)) or a more descriptive name."
            }
            return Finding(
                form: "whole",
                abbreviation: entry.abbreviation,
                message: RuleMessages.ConsistencyNoAbbreviatedIdentifier.abbreviatedIdentifier(
                    name: name,
                    advice: advice,
                ),
            )
        }
        let bytes = Array(name.utf8)
        for entry in earlyPrefixes where !allowedSegmentHolds(name, entry.abbreviation) {
            if let finding = prefixFinding(entry, name: name, bytes: bytes) {
                return finding
            }
        }
        for entry in suffixes {
            guard let suffix = entry.suffix else { continue }
            if suffix.matcher == "millisecondWord" {
                if let range = Self.millisecondWord(in: bytes) {
                    let suggestion = String(
                        decoding: bytes[..<(range.lowerBound + 1)] + Array(suffix.replacement.utf8)
                            + bytes[range.upperBound...],
                        as: UTF8.self,
                    )
                    return Finding(
                        form: "suffix",
                        abbreviation: entry.abbreviation,
                        message: RuleMessages.ConsistencyNoAbbreviatedIdentifier.millisecondSuffix(
                            name: name,
                            suggestion: suggestion,
                        ),
                    )
                }
                continue
            }
            let word = Self.capitalized(entry.abbreviation)
            guard name.hasSuffix(word) else { continue }
            var advice = suffix.advice
            if advice.isEmpty {
                let head = String(name.dropLast(word.count))
                /* A replacement is the one spelling the file chose for every meaning, so it stands alone. */
                let replacements =
                    suffix.replacement.isEmpty ? entry.expansions.map(Self.capitalized) : [suffix.replacement]
                advice = Self.useAdvice(replacements.map { head + $0 })
            }
            return Finding(
                form: "suffix",
                abbreviation: entry.abbreviation,
                message: RuleMessages.ConsistencyNoAbbreviatedIdentifier.abbreviatedSuffix(
                    name: name,
                    suffix: word,
                    advice: advice,
                ),
            )
        }
        for entry in latePrefixes {
            if let finding = prefixFinding(entry, name: name, bytes: bytes) {
                return finding
            }
        }
        if allowedSegments.contains(where: { name.contains($0) }) {
            return nil
        }
        for entry in segments {
            let word = Array(Self.capitalized(entry.abbreviation).utf8)
            guard Self.segmentMatches(word, in: bytes), let at = Self.firstSegmentStart(word, in: bytes) else {
                continue
            }
            if !entry.keywordTermSpellings.isEmpty
                && Self.everySegmentIsKeywordTerm(word, spellings: entry.keywordTermSpellings, in: bytes)
            {
                continue
            }
            var advice = entry.advice
            if entry.segment != "advice" {
                let suggestions = entry.expansions.map { expansion in
                    String(
                        decoding: bytes[..<at] + Array(Self.capitalized(expansion).utf8) + bytes[(at + word.count)...],
                        as: UTF8.self,
                    )
                }
                /* Every expansion rewrites the same occurrence, so a rewrite that leaves the name as it was leaves it so for all of them. */
                if suggestions.contains(name) {
                    continue
                }
                advice = Self.useAdvice(suggestions)
            }
            let shown = Self.capitalized(entry.abbreviation)
            return Finding(
                form: "segment",
                abbreviation: entry.abbreviation,
                message: RuleMessages.ConsistencyNoAbbreviatedIdentifier.abbreviatedWordSegment(
                    name: name,
                    word: shown,
                    advice: advice,
                ),
            )
        }
        return nil
    }

    /* `^<abbreviation>[A-Z]`. An advice-style prefix names the abbreviation, not the name, as Go's does. */
    private func prefixFinding(_ entry: Entry, name: String, bytes: [UInt8]) -> Finding? {
        guard let prefix = entry.prefix else { return nil }
        let word = Array(entry.abbreviation.utf8)
        guard bytes.count > word.count, bytes.starts(with: word), Self.isUppercase(bytes[word.count]) else {
            return nil
        }
        if prefix.style == "advice" {
            return Finding(
                form: "prefix",
                abbreviation: entry.abbreviation,
                message: RuleMessages.ConsistencyNoAbbreviatedIdentifier.abbreviatedIdentifier(
                    name: entry.abbreviation,
                    advice: entry.advice,
                ),
            )
        }
        let rest = name.dropFirst(entry.abbreviation.count)
        return Finding(
            form: "prefix",
            abbreviation: entry.abbreviation,
            message: RuleMessages.ConsistencyNoAbbreviatedIdentifier.abbreviatedIdentifier(
                name: name,
                advice: Self.useAdvice(entry.expansions.map { $0 + rest }),
            ),
        )
    }

    /* Whether an allowed segment in the name holds this abbreviation's letters, which exempts `db` from an `InnoDb` name's early prefix. */
    private func allowedSegmentHolds(_ name: String, _ abbreviation: String) -> Bool {
        allowedSegments.contains { name.contains($0) && $0.lowercased().contains(abbreviation) }
    }

    /* The first `[a-z]Ms($|[A-Z])`, as the range from the lowercase letter to the end of `Ms`. */
    static func millisecondWord(in bytes: [UInt8]) -> Range<Int>? {
        var index = 1
        while index + 2 <= bytes.count {
            if bytes[index] == UInt8(ascii: "M"), bytes[index + 1] == UInt8(ascii: "s"), isLowercase(bytes[index - 1]),
                index + 2 == bytes.count || isUppercase(bytes[index + 2])
            {
                return (index - 1)..<(index + 2)
            }
            index += 1
        }
        return nil
    }

    /* `(^|[^a-zA-Z])Word($|[A-Z0-9])` or `[a-z0-9]Word($|[A-Z0-9])`: the word standing as a camelCase word of its own. */
    static func segmentMatches(_ word: [UInt8], in bytes: [UInt8]) -> Bool {
        var start = 0
        while start + word.count <= bytes.count {
            if bytes[start..<(start + word.count)].elementsEqual(word), endsWord(bytes, at: start + word.count) {
                if start == 0 {
                    return true
                }
                let before = bytes[start - 1]
                if !isLetter(before) || isLowercase(before) || isDigit(before) {
                    return true
                }
            }
            start += 1
        }
        return false
    }

    /*
     Whether each place the word stands as a segment, as `segmentMatches` reads it, opens one of its keyword term's
     spellings at a word boundary: Go's `everySegmentIsKeywordTerm`. Asked only once a segment has matched.
     */
    static func everySegmentIsKeywordTerm(_ word: [UInt8], spellings: [[UInt8]], in bytes: [UInt8]) -> Bool {
        var start = 0
        while start + word.count <= bytes.count {
            if bytes[start..<(start + word.count)].elementsEqual(word), endsWord(bytes, at: start + word.count),
                start == 0 || !isUppercase(bytes[start - 1]),
                !spellings.contains(where: { opensKeywordTerm($0, in: bytes, at: start) })
            {
                return false
            }
            start += 1
        }
        return true
    }

    /* Whether the spelling starts at `start` and ends at the name's end or anything but a lowercase letter, so `VarScoped` opens `VarScopedDeclarations` and not `VarScopedness`. */
    static func opensKeywordTerm(_ spelling: [UInt8], in bytes: [UInt8], at start: Int) -> Bool {
        let end = start + spelling.count
        guard end <= bytes.count, bytes[start..<end].elementsEqual(spelling) else {
            return false
        }
        return end == bytes.count || !isLowercase(bytes[end])
    }

    /* Where Go's replacement pattern `Word($|[A-Z0-9])` first matches, which is where the suggestion rewrites. */
    static func firstSegmentStart(_ word: [UInt8], in bytes: [UInt8]) -> Int? {
        var start = 0
        while start + word.count <= bytes.count {
            if bytes[start..<(start + word.count)].elementsEqual(word), endsWord(bytes, at: start + word.count) {
                return start
            }
            start += 1
        }
        return nil
    }

    private static func endsWord(_ bytes: [UInt8], at index: Int) -> Bool {
        index == bytes.count || isUppercase(bytes[index]) || isDigit(bytes[index])
    }

    static func isUppercase(_ byte: UInt8) -> Bool { byte >= UInt8(ascii: "A") && byte <= UInt8(ascii: "Z") }
    static func isLowercase(_ byte: UInt8) -> Bool { byte >= UInt8(ascii: "a") && byte <= UInt8(ascii: "z") }
    static func isDigit(_ byte: UInt8) -> Bool { byte >= UInt8(ascii: "0") && byte <= UInt8(ascii: "9") }
    static func isLetter(_ byte: UInt8) -> Bool { isUppercase(byte) || isLowercase(byte) }
}
