import Foundation

/*
 A file's name abbreviates no word the house spells out, by the vocabulary `ConsistencyNoAbbreviatedIdentifier` reads,
 cohere's `policy/Abbreviations.json`: `Cfg.swift` and `MsgPrinter.swift` are each reported once (#2xt6v5r, the text
 rules family `abbreviated-file-name`). A file's name is read on every path, so it is held to the words an identifier
 is. The Swift form of `nexus/consistency-no-abbreviated-file-name` and `cohere-go/consistency-no-abbreviated-file-name`,
 which read cohere's `internal/file_name_words`.

 The name's words are `FileNameWords`', read as the one camelCase identifier they spell, so `MsgPrinter.swift` is
 judged as `msgPrinter` would be, and the message names the word and how to spell it out, never a rewritten name. A
 run of words spelling a rule this engine registers is the rule's name and is set aside, so only the words beside it
 are judged (`FileNameWords.withoutRuleName`). A
 generated file, `Package.swift` and `main.swift` are not judged, and nor is a file under a folder whose files are
 someone else's.

 It reads only the path, never the tree, so there is no syntax visitor. One finding per file, at line 1, column 1,
 zero width, as the folder rules report a misplaced file.
 */
public struct ConsistencyNoAbbreviatedFileName: FileRule {
    public static let ruleName = "cohere-swift/consistency-no-abbreviated-file-name"
    public let name = ruleName
    public let origin = RuleOrigin.house
    public let upstreamName: String? = nil

    let vocabulary: AbbreviationVocabulary

    /*
     This engine's registered rule names, read once from `RuleRegistry`, the registry the engine holds. A file named for
     one holds the rule's name, and only the words beside it are judged.
     */
    static let registeredRuleNames = FileNameWords.ruleNames(RuleRegistry.allNames)

    /* The vocabulary is handed in, loaded by the pipeline before anything is checked, as the identifier rule's is. */
    public init(vocabulary: AbbreviationVocabulary) {
        self.vocabulary = vocabulary
    }

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        guard let words = FileNameWords.words(of: file),
            case let ownWords = FileNameWords.withoutRuleName(words, ruleNames: Self.registeredRuleNames),
            !ownWords.isEmpty,
            let found = vocabulary.find(FileNameWords.identifier(of: ownWords)),
            let message = FileNameWords.message(
                fileName: file.url.lastPathComponent,
                abbreviation: found.abbreviation,
                in: vocabulary,
            )
        else {
            return []
        }
        return [FileNameWords.finding(in: file, rule: name, message: message)]
    }
}
