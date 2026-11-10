import Foundation

/*
 What a Swift file's name says in words, and the two judgments of those words the file-name rules make: the Swift port
 of cohere's `internal/file_name_words`, which the TypeScript and Go rules read (#2xt6v5r), held to the same verdicts
 by the text rules corpus (families `utils-file-name` and `abbreviated-file-name`).

 A name is read in three steps, as Go reads it:
   - What a tool reads off the name is set aside: the `.swift` extension.
   - A name nobody inside the package chose is never judged: a generated file's (`Name.generated.swift`, the
     house's marker), and one SwiftPM reads by its exact spelling, `Package.swift`, a version-specific
     `Package@swift-6.0.swift`, and `main.swift`.
   - What is left is cut into words: each run of ASCII letters, cut again before a capital that follows a lowercase
     letter and before the last capital of a run that a lowercase letter follows, the parts
     `ConsistencyRequireSeparatedFolderWords.parts(of:)` cuts a folder's name into. So `ViewUtil`, `View+Util` and
     `Outer.View_Util` all hold `Util`.

 A file is judged only where its folders inside the package hold none whose files are someone else's or a test's input
 (`foreignFolders`, Go's `foreignFolders` word for word).
 */
enum FileNameWords {
    /* What the house puts between a generated file's name and `.swift`. */
    static let generatedMarker = "generated"

    /* The stems SwiftPM reads by their exact spelling. */
    static let toolStems: Set<String> = ["Package", "main"]

    /* What opens the name of a version-specific SwiftPM manifest. */
    static let packageManifestPrefix = "Package@swift"

    /* Folders whose files are someone else's or a test's input, wherever they sit: Go's `foreignFolders`. */
    static let foreignFolders: Set<String> = ["node_modules", "vendor", "third_party", "testdata"]

    /* The words of a Swift file's name, or nil when the name is not judged: a generated file's, one SwiftPM reads by name, or one under a foreign folder. */
    static func words(of file: ParsedFile) -> [String]? {
        if ConsistencyNoUtilsFolder.foldersInsidePackage(of: file).contains(where: foreignFolders.contains) {
            return nil
        }
        return words(ofName: file.url.lastPathComponent)
    }

    /* The words of a Swift file's name, its folder left out, or nil when the name is not judged. */
    static func words(ofName fileName: String) -> [String]? {
        let stem = fileName.hasSuffix(".swift") ? String(fileName.dropLast(".swift".count)) : fileName
        if toolStems.contains(stem) || stem.hasPrefix(packageManifestPrefix) {
            return nil
        }
        if stem.split(separator: ".", omittingEmptySubsequences: false).contains(where: { $0 == generatedMarker }) {
            return nil
        }
        let bytes = Array(stem.utf8)
        return ConsistencyRequireSeparatedFolderWords.parts(of: bytes).map {
            String(decoding: bytes[$0], as: UTF8.self)
        }
    }

    /*
     The first word that is `util` or `utils`, as the name spells it: Go's `UtilsWord`, through the folder rule's own
     reading of the word. A word right after `No` is the name of a ban on it, as in `ConsistencyNoUtilsFolder.swift`,
     a file named for exactly what it holds.
     */
    static func utilsWord(in words: [String]) -> String? {
        words.indices
            .first { index in
                ConsistencyNoUtilsFolder.isBanned(words[index])
                    && (index == 0 || words[index - 1].lowercased() != "no")
            }
            .map { words[$0] }
    }

    /* The house spelling for a `util` or `utils` word, in its letters' casing: Go's `UtilitiesReplacement`. */
    static func utilitiesReplacement(for word: String) -> String {
        if word == word.uppercased() {
            return "UTILITIES"
        }
        return word.hasPrefix("U") ? "Utilities" : "utilities"
    }

    /*
     The words as one camelCase identifier, the first lowercased and each after it capitalized with the rest of it
     lowercased: Go's `Identifier`. A word of two or more capitals and no lowercase letter is kept as written, an
     initialism or a code, as `CH` in `FlagCH.swift`, which the vocabulary reads as no abbreviation of its.
     */
    static func identifier(of words: [String]) -> String {
        words.enumerated()
            .map { index, word in
                if word.count >= 2, word == word.uppercased(), word != word.lowercased() {
                    return word
                }
                let lowered = word.lowercased()
                return index == 0 ? lowered : lowered.prefix(1).uppercased() + lowered.dropFirst()
            }
            .joined()
    }

    /*
     Registered rules' names, each as the words a file name spelling it holds: its last slash-separated part,
     lowercased, a space for each hyphen. Go's `NewRuleNames`.
     */
    static func ruleNames(_ names: [String]) -> Set<String> {
        Set(
            names.map { name in
                String(name.split(separator: "/").last ?? Substring(name)).lowercased()
                    .replacingOccurrences(of: "-", with: " ")
            }
        )
    }

    /*
     The words with the longest run that spells a registered rule's name taken out, the earliest where two are as
     long, and the words as they were where none does: Go's `withoutRuleName`. A file named for a rule holds the
     rule's name, upstream's for a port, and only the words beside it are the file's own to judge. A rule named in one
     word is never set aside: the word is an ordinary one, and a file spelling it is named for what it holds.
     */
    static func withoutRuleName(_ words: [String], ruleNames: Set<String>) -> [String] {
        let lowered = words.map { $0.lowercased() }
        for length in stride(from: words.count, through: 2, by: -1) {
            for start in 0...(words.count - length)
            where ruleNames.contains(lowered[start..<(start + length)].joined(separator: " ")) {
                return Array(words[..<start] + words[(start + length)...])
            }
        }
        return words
    }

    /* The message for a file name the vocabulary found abbreviated: Go's `Advice`, the word's expansions quoted as a list, or its entry's advice where it gives no expansion. */
    static func message(
        fileName: String,
        abbreviation: String,
        in vocabulary: AbbreviationVocabulary,
    ) -> RuleMessages.Message? {
        guard let entry = vocabulary.entry(for: abbreviation) else { return nil }
        guard !entry.expansions.isEmpty else {
            return RuleMessages.ConsistencyNoAbbreviatedFileName.abbreviatedFileNameAdvice(
                fileName: fileName,
                word: abbreviation,
                advice: entry.advice,
            )
        }
        let quoted = AbbreviationVocabulary.quoted(entry.expansions)
        return RuleMessages.ConsistencyNoAbbreviatedFileName.abbreviatedFileName(
            fileName: fileName,
            word: abbreviation,
            expansions: entry.expansions.count > 1 ? quoted + ", whichever it means" : quoted,
        )
    }

    /* A zero-width finding at the very start of the file, where every engine reports a finding about the file's name. */
    static func finding(in file: ParsedFile, rule: String, message: RuleMessages.Message) -> FindingRecord {
        FindingRecord(
            source: .rule,
            file: file.url.path,
            line: 1,
            column: 1,
            endLine: 1,
            endColumn: 1,
            severity: .error,
            rule: rule,
            messageId: message.id,
            message: message.text,
        )
    }
}
