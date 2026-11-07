import Foundation

/*
 The words in a folder's name are separated, never run together: `FormatOptions` or `format_options`, not
 `Formatoptions` or `formatoptions`. A mind reading cold reads `FormatOptions` at a glance and has to stop and parse
 `Formatoptions`, and a folder's name is read on every path. The Swift form of
 `cohere-go/consistency-require-separated-folder-words`, held by the text rules corpus to Go's verdict (#4b337jk,
 family `separated-folder-words`; #2fnqe2h).

 Go's folders are snake_case, so Go judges a name one underscore-separated part at a time. Swift's are PascalCase
 (`AhraOsServices`, `CohereSwiftTests`, `GLTF2RealityKit`), where a capital is the separator an underscore is in Go,
 so judging the name as Go does, lowercased whole, would report every Swift folder of two words. So Swift splits a
 name into parts first, then judges each part exactly as Go judges one:
   - a part ends at every character that is not an ASCII letter (an underscore, a hyphen, a digit, a dot), and at a
     case boundary: before a capital that follows a lowercase letter, and before the last capital of a run of them
     that a lowercase letter follows, so `AISTBench` is `AIST` and `Bench`, and `iOSTerminal` is `i`, `OS` and
     `Terminal`;
   - each part, lowercased, passes when it is a word, of policy's word list (`WordList`, `policy/Words.txt`) or a
     brand `policy/Brands.json` lists, lowercased to its letters. A part that splits into two or more pieces that are
     each a word is the finding, with the fewest pieces Go's tie-break chooses. A part that splits no way is a name
     the lists cannot read (`vfs`, `cachedvfs`) rather than words run together, and passes.

 The replacement is the folder with each run-together part written as its pieces in that part's own style: a
 capitalized part with each piece capitalized (`Formatoptions` becomes `FormatOptions`), an all-capitals part with
 its pieces in capitals joined by `_`, and a lowercase part with its pieces joined by `_`, or by `-` where the folder
 already uses hyphens and no underscores. Everything else in the name stays as written.

 It reads only the path, as `consistency-no-utils-folder` does, and judges the same folders: those inside the
 package, from its root down (`ConsistencyNoUtilsFolder.foldersInsidePackage`). The root itself is the checkout's
 name and nobody's to rename from inside it (fw12), and a folder above it is no finding anybody inside the package
 can act on. One finding per file, for the outermost run-together folder, at line 1, column 1, zero width, since the
 file as a whole is what sits under the name; renaming the folder clears every file's.
 */
public struct ConsistencyRequireSeparatedFolderWords: FileRule {
    public static let ruleName = "cohere-swift/consistency-require-separated-folder-words"
    public let name = ruleName
    public let origin = RuleOrigin.house
    public let upstreamName: String? = nil

    let words: WordList
    /* `Brands.json`'s brands, lowercased and letters alone, so a brand a folder spells is a word as the list's words are: Go's `brandWords`. */
    let brandWords: Set<String>

    /*
     The fewest letters a piece of a split may have, unless it is one of `twoLetterWords`: Go's `minimumPieceLength`.
     The lists hold hundreds of two-letter entries, abbreviations and interjections among them, and with those as
     pieces `acheck` would split as `ac_heck`.
     */
    static let minimumPieceLength = 3

    /* The two-letter pieces a split may use despite the minimum, each a name a reader knows on sight: Go's closed `twoLetterWords`, word for word. */
    static let twoLetterWords: Set<String> = ["cc", "db", "go", "io", "js", "os", "ts", "ui"]

    /* The lists are handed in, loaded by the pipeline before anything is checked, never looked up here. */
    public init(words: WordList, brands: BrandVocabulary) {
        self.words = words
        brandWords = Set(
            brands.longestFirst.map { brand in
                String(decoding: brand.utf8.filter(Self.isLetter).map(Self.lowercased), as: UTF8.self)
            }
        )
    }

    public func findings(in file: ParsedFile) -> [FindingRecord] {
        for folder in ConsistencyNoUtilsFolder.foldersInsidePackage(of: file) {
            guard let replacement = separated(folder) else { continue }
            let message = RuleMessages.ConsistencyRequireSeparatedFolderWords.runTogetherFolderWords(
                folder: folder,
                replacement: replacement,
            )
            return [
                FindingRecord(
                    source: .rule,
                    file: file.url.path,
                    line: 1,
                    column: 1,
                    endLine: 1,
                    endColumn: 1,
                    severity: .error,
                    rule: name,
                    messageId: message.id,
                    message: message.text,
                )
            ]
        }
        return []
    }

    /* The folder with each run-together part written as its pieces, or nil when no part runs words together. */
    func separated(_ folder: String) -> String? {
        let bytes = Array(folder.utf8)
        let separator = bytes.contains(UInt8(ascii: "-")) && !bytes.contains(UInt8(ascii: "_")) ? "-" : "_"
        var replacement = ""
        var runTogether = false
        var written = 0
        for part in Self.parts(of: bytes) {
            let lowercase = String(decoding: bytes[part].map(Self.lowercased), as: UTF8.self)
            guard !isWord(lowercase), let pieces = fewestPieces(Array(lowercase.utf8)), pieces.count >= 2 else {
                continue
            }
            replacement += String(decoding: bytes[written..<part.lowerBound], as: UTF8.self)
            replacement += Self.styled(pieces, like: bytes[part], separator: separator)
            written = part.upperBound
            runTogether = true
        }
        guard runTogether else { return nil }
        return replacement + String(decoding: bytes[written...], as: UTF8.self)
    }

    /*
     The ranges of a name's parts: its runs of ASCII letters, each cut before a capital that follows a lowercase
     letter, and before the last capital of a run of them that a lowercase letter follows.
     */
    static func parts(of bytes: [UInt8]) -> [Range<Int>] {
        var parts: [Range<Int>] = []
        var start: Int?
        for index in 0...bytes.count {
            let letter = index < bytes.count && isLetter(bytes[index])
            guard letter else {
                if let start {
                    parts.append(start..<index)
                }
                start = nil
                continue
            }
            guard let partStart = start else {
                start = index
                continue
            }
            let afterLowercase = isUppercase(bytes[index]) && isLowercase(bytes[index - 1])
            let endsCapitalRun =
                isUppercase(bytes[index]) && isUppercase(bytes[index - 1]) && index + 1 < bytes.count
                && isLowercase(bytes[index + 1])
            if afterLowercase || endsCapitalRun {
                parts.append(partStart..<index)
                start = index
            }
        }
        return parts
    }

    /*
     The pieces in the part's own style: capitals joined by `_` for an all-capitals part, each capitalized and
     run together for a capitalized one, and as they are, joined by the folder's separator, for a lowercase one.
     */
    static func styled(_ pieces: [String], like part: ArraySlice<UInt8>, separator: String) -> String {
        if part.allSatisfy(isUppercase) {
            return pieces.map { $0.uppercased() }.joined(separator: "_")
        }
        if let first = part.first, isUppercase(first) {
            return pieces.map { $0.prefix(1).uppercased() + $0.dropFirst() }.joined()
        }
        return pieces.joined(separator: separator)
    }

    /* Whether text is in the list or is a brand, at any length: Go's `isWord`. */
    func isWord(_ text: String) -> Bool {
        brandWords.contains(text) || words.contains(text)
    }

    /* Whether text may be one piece of a split: one of `twoLetterWords`, or a word of `minimumPieceLength` letters or more. Go's `isPiece`. */
    func isPiece(_ text: String) -> Bool {
        Self.twoLetterWords.contains(text) || text.utf8.count >= Self.minimumPieceLength && isWord(text)
    }

    /*
     The part split into the fewest pieces, or nil when no split exists: Go's `fewestPieces`, step for step.

     Two splits can tie: `runoutput` is `run_output` or `runout_put`, and `gatesample` `gate_sample` or `gates_ample`.
     The tie goes to the split whose first piece is shortest, then its second, and so on. A longer first piece is most
     often the earlier word swallowing the start of the next as an inflection or a particle (`gates`, `runout`), and
     the shorter one leaves the next word whole. The rule is a fixed order over the pieces, so a name splits the same
     way on every run, and the same way in both engines.

     `pieceCount[start]` is the fewest pieces `part[start...]` splits into, or 0 when it splits no way, and
     `pieceEnd[start]` is where the first of them ends. Filling them from the end, each start tries its pieces
     shortest first and keeps only a strictly better count, so the shortest first piece of a fewest-pieces split is
     the one kept.
     */
    func fewestPieces(_ part: [UInt8]) -> [String]? {
        var pieceCount = [Int](repeating: 0, count: part.count + 1)
        var pieceEnd = [Int](repeating: 0, count: part.count + 1)
        for start in stride(from: part.count - 1, through: 0, by: -1) {
            for end in (start + 1)...part.count {
                var rest = 0
                if end < part.count {
                    rest = pieceCount[end]
                    if rest == 0 {
                        continue
                    }
                }
                if pieceCount[start] == 0 || rest + 1 < pieceCount[start],
                    isPiece(String(decoding: part[start..<end], as: UTF8.self))
                {
                    pieceCount[start] = rest + 1
                    pieceEnd[start] = end
                }
            }
        }
        guard pieceCount[0] > 0 else { return nil }
        var pieces: [String] = []
        var start = 0
        while start < part.count {
            pieces.append(String(decoding: part[start..<pieceEnd[start]], as: UTF8.self))
            start = pieceEnd[start]
        }
        return pieces
    }

    static func isLetter(_ byte: UInt8) -> Bool {
        isLowercase(byte) || isUppercase(byte)
    }

    static func isLowercase(_ byte: UInt8) -> Bool {
        byte >= UInt8(ascii: "a") && byte <= UInt8(ascii: "z")
    }

    static func isUppercase(_ byte: UInt8) -> Bool {
        byte >= UInt8(ascii: "A") && byte <= UInt8(ascii: "Z")
    }

    /* An ASCII capital as its lowercase letter, and any other byte as it is. */
    static func lowercased(_ byte: UInt8) -> UInt8 {
        isUppercase(byte) ? byte + (UInt8(ascii: "a") - UInt8(ascii: "A")) : byte
    }
}
